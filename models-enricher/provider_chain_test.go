package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
)

func providerFixture(t *testing.T) (*Config, *SourceTables) {
	t.Helper()
	// 独立测试配置保留精确查找/继承能力测试，不要求部署配置维持旧的批量映射。
	mini := ModelConfig{
		SourcePriority: []string{"models.dev/minimax", "modelparams.dev/minimax/api_key"},
		LookupIDs:      map[string]string{"models.dev/minimax": "MiniMax-M3", "modelparams.dev/minimax/api_key": "minimax-m3"},
	}
	moon := ModelConfig{
		SourcePriority: []string{"models.dev/moonshotai", "modelparams.dev/moonshot/api_key"},
		LookupIDs:      map[string]string{"models.dev/moonshotai": "kimi-k3", "modelparams.dev/moonshot/api_key": "kimi-k3"},
	}
	nimMini, nimMoon := mini, moon
	nimMini.SourcePriority = append([]string{"models.dev/nvidia"}, mini.SourcePriority...)
	nimMoon.SourcePriority = append([]string{"models.dev/nvidia"}, moon.SourcePriority...)
	nim := ChannelConfig{SourcePriority: []string{"models.dev/nvidia"}, Models: map[string]ModelConfig{
		"minimaxai/minimax-m3":               nimMini,
		"moonshotai/kimi-k3":                 nimMoon,
		"deepseek-ai/deepseek-v4-flash-0731": {SourcePriority: []string{"models.dev/nvidia", "modelparams.dev/nvidia/api_key"}, LookupIDs: map[string]string{"modelparams.dev/nvidia/api_key": "deepseek-v4-flash-0731"}},
	}}
	cfg := &Config{
		Channels: map[string]ChannelConfig{
			"commandcode": {SourcePriority: []string{"models.dev/anthropic", "modelparams.dev/anthropic/api_key"}, Models: map[string]ModelConfig{"moonshotai/Kimi-K3": moon, "MiniMaxAI/MiniMax-M3": mini}},
			"gmicloud":    {SourcePriority: mini.SourcePriority, Models: map[string]ModelConfig{"MiniMaxAI/MiniMax-M3": mini}},
			"nim":         nim,
		},
		CustomChannels: map[string]ChannelConfig{"nvidia-catalog": nim},
		StaticModels:   []map[string]any{{"slug": "minimaxai/minimax-m3", "inherit": []string{"nvidia-catalog/minimaxai/minimax-m3"}}},
	}
	raw, err := os.ReadFile("testdata/provider-chain-sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Dev  json.RawMessage `json:"models_dev_api"`
		Flat json.RawMessage `json:"models_dev_flat"`
		MP   json.RawMessage `json:"modelparams"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	tables := emptySourceTables()
	mergeSourceMaps(tables.dev, indexModelsDev(fixture.Dev))
	mergeSourceMaps(tables.dev, indexModelsDevFlat(fixture.Flat))
	tables.mpK, tables.mpS = indexModelparams(fixture.MP, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return cfg, tables
}

func mergeProviderSamples(cfg *Config, tables *SourceTables, rows []map[string]any) map[string]map[string]any {
	packs := []channelModels{}
	for prefix := range cfg.Channels {
		packs = append(packs, channelModels{Channel: Channel{Prefix: prefix, Type: "openai-compatibility"}})
	}
	result := mergeManifest(&Manifest{Models: rows}, packs, cfg, tables, nil)
	by := map[string]map[string]any{}
	for _, m := range result.Models {
		by[asString(m["slug"])] = m
	}
	return by
}

// 仅验证原绑定的源记录存在，不表示默认拆分后仍会采用该 token；型号身份另行核验。
func TestProviderFixtureLookupBindingsExist(t *testing.T) {
	cfg, tables := providerFixture(t)
	checked := 0
	for _, channels := range []map[string]ChannelConfig{cfg.Channels, cfg.CustomChannels} {
		for name, ch := range channels {
			for model, mc := range ch.Models {
				for token, id := range mc.LookupIDs {
					if !chainHas(sourceChain(ch, model), token) {
						t.Fatalf("%s/%s lookup token outside effective chain: %s", name, model, token)
					}
					if _, ok := tables.lookupOne(token, id); !ok {
						t.Fatalf("source lookup target missing from snapshot %s/%s: %s -> %s", name, model, token, id)
					}
					checked++
				}
			}
		}
	}
	if checked != 16 {
		t.Fatalf("fixture lookup coverage changed: %d", checked)
	}
	for _, native := range []string{"codex", "supergrok", "kimi-coding"} {
		if _, ok := cfg.Channels[native]; ok {
			t.Fatalf("OAuth/native channel acquired external chain: %s", native)
		}
	}
}

func TestProviderFixtureFinalTokenScope(t *testing.T) {
	cfg, tables := providerFixture(t)
	slugs := []string{"commandcode/claude-sonnet-5", "commandcode/claude-opus-5", "commandcode/moonshotai/Kimi-K3", "commandcode/MiniMaxAI/MiniMax-M3", "gmicloud/MiniMaxAI/MiniMax-M3", "nim/minimaxai/minimax-m3", "nim/moonshotai/kimi-k3", "nim/deepseek-ai/deepseek-v4-flash-0731", "commandcode/deepseek/deepseek-v4-flash-fast", "commandcode/MiniMaxAI/minimax-m3", "codex/native-example"}
	rows := []map[string]any{}
	for _, slug := range slugs {
		rows = append(rows, map[string]any{"slug": slug, "id": slug})
	}
	rows[len(rows)-1]["context_window"] = 321
	out := mergeProviderSamples(cfg, tables, rows)
	for _, slug := range slugs {
		if out[slug]["slug"] != slug || out[slug]["id"] != slug {
			t.Fatalf("public identity changed: %s", slug)
		}
	}
	for _, i := range []int{0, 1, 2, 6} {
		if !reflect.DeepEqual(out[slugs[i]]["output_modalities"], []any{"text"}) {
			t.Fatalf("matching final source did not enrich %s", slugs[i])
		}
	}
	if toInt(out[slugs[0]]["max_output_tokens"]) != 128000 || toInt(out[slugs[1]]["context_window"]) != 1000000 {
		t.Fatal("shared Anthropic chain did not fill both models")
	}
	if toInt(out[slugs[6]]["max_output_tokens"]) != 131072 || out[slugs[6]]["max_output_tokens"] != out[slugs[2]]["max_output_tokens"] {
		t.Fatal("default moonshotai query retained the old NVIDIA provider")
	}
	// 未配置前缀别名时，minimax/nvidia 上的旧显式 ID 不会复制到 minimaxai/deepseek-ai。
	for _, i := range []int{3, 4, 5, 7} {
		for _, field := range []string{"context_window", "max_output_tokens", "output_modalities"} {
			if out[slugs[i]][field] != nil {
				t.Fatalf("old provider binding leaked into final scope: %s %s", slugs[i], field)
			}
		}
	}
	for _, slug := range slugs[5:8] {
		if out[slug]["max_input_tokens"] != nil {
			t.Fatalf("source-absent input limit fabricated for %s", slug)
		}
	}
	for _, slug := range slugs[8:10] {
		if out[slug]["context_window"] != nil {
			t.Fatalf("unverified case/fast variant guessed for %s", slug)
		}
	}
	if out["codex/native-example"]["context_window"] != 321 {
		t.Fatal("native metadata changed")
	}
	for slug := range out {
		if strings.HasPrefix(slug, "nvidia-catalog/") {
			t.Fatal("hidden custom pool published")
		}
	}
	if out["minimaxai/minimax-m3"]["slug"] != "minimaxai/minimax-m3" || out["minimaxai/minimax-m3"]["max_output_tokens"] != nil {
		t.Fatal("static identity changed or inherited a source outside the final scope")
	}
}

func TestProviderFixturePreservesEmptyAndReplacementBoundaries(t *testing.T) {
	cfg, tables := providerFixture(t)
	hit := tables.dev["anthropic"]["claude-sonnet-5"]
	hit["max_input_tokens"] = json.Number("0")
	hit["supported_reasoning_levels"] = []any{}
	hit["fixture_false"] = false
	setHit(tables.mpK, "anthropic", "claude-sonnet-5", sourceHit{"max_input_tokens": 99, "supported_reasoning_levels": effortsToLevels([]any{"high"}), "fixture_false": true})
	// 该模型显式整链替换渠道链；父级 Anthropic 的同名记录不应参与。
	setHit(tables.dev, "anthropic", "moonshotai/Kimi-K3", sourceHit{"max_input_tokens": 666})
	rows := []map[string]any{{"slug": "commandcode/claude-sonnet-5"}, {"slug": "commandcode/moonshotai/Kimi-K3"}}
	out := mergeProviderSamples(cfg, tables, rows)
	m := out["commandcode/claude-sonnet-5"]
	if m["max_input_tokens"] != json.Number("0") || m["fixture_false"] != false {
		t.Fatal("explicit zero/false lost to a lower source")
	}
	if a, ok := m["supported_reasoning_levels"].([]any); !ok || len(a) != 0 {
		t.Fatal("explicit empty reasoning list treated as absent")
	}
	if out["commandcode/moonshotai/Kimi-K3"]["max_input_tokens"] != nil {
		t.Fatal("model-level chain incorrectly inherited the parent chain")
	}
	rows[0]["context_window"] = 987
	missing := mergeProviderSamples(cfg, emptySourceTables(), rows)
	if missing["commandcode/claude-sonnet-5"]["context_window"] != 987 || missing["commandcode/claude-sonnet-5"]["max_input_tokens"] != nil {
		t.Fatal("unavailable sources should preserve baseline without fabricating fields")
	}
}
