package main

import (
	"testing"
	"time"
)

func testCache() *SourceCache {
	s := newSourceCache(time.Hour)
	s.dev = map[string]sourceHit{}
	s.mpK = map[string]sourceHit{}
	s.mpS = map[string]sourceHit{}
	s.at = time.Now()
	return s
}

func TestIndexModelparamsAuthTypeSplit(t *testing.T) {
	raw := []byte(`{"models":[
		{"model":"m-key","authType":"api_key","provider":"p","params":[{"path":"max_tokens","range":{"max":100}}]},
		{"model":"m-sub","authType":"subscription","params":[{"path":"max_tokens","range":{"max":200}}]},
		{"model":"m-none","params":[{"path":"max_tokens","range":{"max":300}}]}
	]}`)
	k, s := map[string]sourceHit{}, map[string]sourceHit{}
	indexModelparams(raw, k, s)
	if k["m-key"].MaxTokens != 100 || k["p/m-key"].MaxTokens != 100 {
		t.Fatalf("api_key index wrong: %+v", k)
	}
	if _, ok := s["m-key"]; ok {
		t.Fatal("api_key entry leaked into subscription index")
	}
	if s["m-sub"].MaxTokens != 200 {
		t.Fatal("subscription entry missing")
	}
	if k["m-none"].MaxTokens != 300 || s["m-none"].MaxTokens != 300 {
		t.Fatal("authType-less entry must be indexed into both")
	}
}

func TestSourceChainResolution(t *testing.T) {
	cfg := &Config{SourcePriority: []string{"models_dev"}}
	ch := ChannelConfig{
		SourcePriority: []string{"modelparams_api_key"},
		Models: map[string]ModelConfig{
			"m1": {SourcePriority: []string{"modelparams_subscription"}},
		},
	}
	if got := cfg.sourceChain(ch, "m1"); got[0] != "modelparams_subscription" {
		t.Fatalf("model-level should win: %v", got)
	}
	if got := cfg.sourceChain(ch, "m2"); got[0] != "modelparams_api_key" {
		t.Fatalf("channel-level should win: %v", got)
	}
	ch2 := ChannelConfig{}
	if got := cfg.sourceChain(ch2, "m3"); got[0] != "models_dev" {
		t.Fatalf("global should win: %v", got)
	}
	cfg2 := &Config{}
	got := cfg2.sourceChain(ch2, "m4")
	if got[0] != "modelparams_subscription" || len(got) != 3 {
		t.Fatalf("builtin default expected: %v", got)
	}
}

func TestModelOverridesLegacyMerge(t *testing.T) {
	ch := ChannelConfig{
		Overrides: map[string]map[string]any{"m": {"a": 1, "b": 2}},
		Models:    map[string]ModelConfig{"m": {Overrides: map[string]any{"b": 3}}},
	}
	ov := ch.modelOverrides("m")
	if ov["a"] != 1 || ov["b"] != 3 {
		t.Fatalf("legacy+models merge wrong: %v", ov)
	}
	legacyOnly := ChannelConfig{Overrides: map[string]map[string]any{"x": {"c": 4}}}
	if legacyOnly.modelOverrides("x")["c"] != 4 {
		t.Fatal("legacy flat overrides must keep working alone")
	}
}

func TestMergeChainPrecedence(t *testing.T) {
	s := testCache()
	s.mpS["m"] = sourceHit{MaxTokens: 200, ContextWindow: 2000}
	s.mpK["m"] = sourceHit{MaxTokens: 100}
	s.dev["m"] = sourceHit{DisplayName: "DevName", ContextWindow: 9999}

	cfg := &Config{} // default chain: sub -> key -> dev
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c", Type: "openai-compatibility"},
		Models:  []ParsedModel{{ID: "m", DisplayName: "Parsed"}},
	}}
	out := mergeManifest(nil, fetched, cfg, s)
	if len(out.Models) != 1 {
		t.Fatalf("want 1 model, got %d", len(out.Models))
	}
	m := out.Models[0]
	if m["slug"] != "c/m" {
		t.Fatalf("slug: %v", m["slug"])
	}
	if m["display_name"] != "Parsed" {
		t.Fatalf("parsed must beat sources: %v", m["display_name"])
	}
	if m["max_tokens"] != 200 {
		t.Fatalf("subscription should precede api_key: %v", m["max_tokens"])
	}
	if m["context_window"] != 2000 {
		t.Fatalf("subscription fills context_window first: %v", m["context_window"])
	}
}

func TestMergeChainModelLevelOverride(t *testing.T) {
	s := testCache()
	s.mpS["m"] = sourceHit{MaxTokens: 200}
	s.mpK["m"] = sourceHit{MaxTokens: 100}
	cfg := &Config{Channels: map[string]ChannelConfig{
		"c": {Models: map[string]ModelConfig{
			"m": {SourcePriority: []string{"modelparams_api_key"},
				Overrides: map[string]any{"max_tokens": 42}},
		}},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c"},
		Models:  []ParsedModel{{ID: "m"}},
	}}
	out := mergeManifest(nil, fetched, cfg, s)
	if out.Models[0]["max_tokens"] != 42 {
		t.Fatalf("overrides must win last: %v", out.Models[0]["max_tokens"])
	}
}

func TestStaticInheritStillWorks(t *testing.T) {
	s := testCache()
	cfg := &Config{
		StaticModels: []map[string]any{
			{"slug": "alias-m", "inherit": []any{"c/m"}, "overrides": map[string]any{"description": "d"}},
		},
	}
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c"},
		Models:  []ParsedModel{{ID: "m", ContextWindow: 1234}},
	}}
	out := mergeManifest(nil, fetched, cfg, s)
	var alias map[string]any
	for _, m := range out.Models {
		if m["slug"] == "alias-m" {
			alias = m
		}
	}
	if alias == nil || alias["context_window"] != 1234 || alias["description"] != "d" {
		t.Fatalf("static inherit broken: %+v", alias)
	}
}

func TestCPABareDroppedAtIngest(t *testing.T) {
	s := testCache()
	base := &Manifest{Models: []map[string]any{
		{"slug": "bare-model", "context_window": 1},
		{"slug": "prefixed/model", "context_window": 2},
	}}
	out := mergeManifest(base, nil, &Config{}, s)
	if len(out.Models) != 1 || out.Models[0]["slug"] != "prefixed/model" {
		t.Fatalf("bare CPA entries must be dropped at ingest: %+v", out.Models)
	}
}

func TestStaticInheritIDAt(t *testing.T) {
	// id@ 无池条目也能从源解析；链内首命中逐字段胜出
	s := testCache()
	s.mpS["qwen-flash"] = sourceHit{DisplayName: "Qwen Flash", ContextWindow: 1000000, MaxTokens: 65536}
	s.dev["qwen-flash"] = sourceHit{DisplayName: "Qwen Flash Dev", ContextWindow: 131072, MaxTokens: 8192}
	cfg := &Config{
		StaticModels: []map[string]any{{
			"slug":    "qwen-flash-alias",
			"inherit": []any{"id@qwen-flash"},
		}},
	}
	out := mergeManifest(&Manifest{}, nil, cfg, s)
	if len(out.Models) != 1 {
		t.Fatalf("expect 1 model, got %d", len(out.Models))
	}
	m := out.Models[0]
	// 默认链 subscription 优先：ctx 取 mpS 的 1000000，dev 的 131072 不得覆盖
	if m["context_window"] != 1000000 {
		t.Fatalf("chain order broken: %v", m["context_window"])
	}
	if m["display_name"] != "Qwen Flash" {
		t.Fatalf("display_name: %v", m["display_name"])
	}
}

func TestStaticInheritIDAtMiss(t *testing.T) {
	// 源缺失 → 跳层 + overrides 仍生效（fail-open）
	s := testCache()
	cfg := &Config{
		StaticModels: []map[string]any{{
			"slug":      "ghost-alias",
			"inherit":   []any{"id@no-such-model"},
			"overrides": map[string]any{"display_name": "Ghost"},
		}},
	}
	out := mergeManifest(&Manifest{}, nil, cfg, s)
	if len(out.Models) != 1 || out.Models[0]["display_name"] != "Ghost" {
		t.Fatalf("fail-open broken: %+v", out.Models)
	}
}

func TestStaticInheritIDAtStaticChain(t *testing.T) {
	// static 级 source_priority 控制 authType 索引选择
	s := testCache()
	s.mpS["m"] = sourceHit{MaxTokens: 200}
	s.mpK["m"] = sourceHit{MaxTokens: 100}
	cfg := &Config{
		StaticModels: []map[string]any{{
			"slug":            "m-alias",
			"source_priority": []any{"modelparams_api_key"},
			"inherit":         []any{"id@m"},
		}},
	}
	out := mergeManifest(&Manifest{}, nil, cfg, s)
	if out.Models[0]["max_tokens"] != 100 {
		t.Fatalf("static-level chain ignored: %v", out.Models[0]["max_tokens"])
	}
}

func TestStripCodexTemplateJunk(t *testing.T) {
	s := testCache()
	s.dev["glm-5.3"] = sourceHit{ContextWindow: 1000000, MaxTokens: 131072}
	base := &Manifest{Models: []map[string]any{
		// 模板克隆：非 codex 目录型号 + ctx==272000 → 剥
		{"slug": "zcode/glm-5.3", "context_window": 272000, "max_context_window": 272000,
			"max_tokens": 120000, "supported_reasoning_levels": []any{map[string]any{"effort": "medium"}},
			"default_reasoning_level": "medium", "shell_type": "shell"},
		// codex 目录型号豁免（末段命中）
		{"slug": "codex/gpt-5.5", "context_window": 272000, "max_tokens": 128000},
		// 真实渠道/registry 值（≠模板值）保留
		{"slug": "kimi-coding/kimi-k3", "context_window": 1048576, "max_tokens": 65536},
		{"slug": "axis/gpt-5.6-sol", "context_window": 1050000, "max_tokens": 128000},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "zc", Prefix: "zcode"},
		Models:  []ParsedModel{{ID: "glm-5.3"}},
	}}
	out := mergeManifest(base, fetched, &Config{SourcePriority: []string{"models_dev"}}, s)
	got := map[string]map[string]any{}
	for _, m := range out.Models {
		got[asString(m["slug"])] = m
	}
	// 剥后由源重建
	if g := got["zcode/glm-5.3"]; g["context_window"] != 1000000 || g["max_tokens"] != 131072 {
		t.Fatalf("glm-5.3 should be refilled from source: %+v", g)
	}
	// 契约字段保留
	if _, ok := got["zcode/glm-5.3"]["shell_type"]; !ok {
		t.Fatal("client-contract fields must survive the strip")
	}
	if c := got["codex/gpt-5.5"]; c["context_window"] != 272000 {
		t.Fatalf("codex catalog slug exempt: %+v", c)
	}
	if k := got["kimi-coding/kimi-k3"]; k["context_window"] != 1048576 {
		t.Fatalf("non-template value must survive: %+v", k)
	}
	if a := got["axis/gpt-5.6-sol"]; a["context_window"] != 1050000 {
		t.Fatalf("channel-declared value must survive: %+v", a)
	}
}

func TestIndexModelsDevProviderPreference(t *testing.T) {
	raw := []byte(`{
		"digitalocean": {"models": {"glm-5.3": {"id":"glm-5.3","limit":{"context":1048576,"output":1048576}}}},
		"zhipuai": {"models": {"glm-5.3": {"id":"glm-5.3","limit":{"context":1000000,"output":131072}}}}
	}`)
	for i := 0; i < 8; i++ {
		out := map[string]sourceHit{}
		indexModelsDev(raw, out)
		if h := out["glm-5.3"]; h.ContextWindow != 1000000 || h.MaxTokens != 131072 {
			t.Fatalf("official provider must win deterministically: %+v", h)
		}
	}
}

func TestStaticBareInheritPoolAndIDAt(t *testing.T) {
	s := testCache()
	s.dev["glm-5.3"] = sourceHit{ContextWindow: 1000000, MaxTokens: 131072}
	cfg := &Config{
		SourcePriority: []string{"models_dev"},
		StaticModels: []map[string]any{
			{"slug": "zcode/glm-5.3", "overrides": map[string]any{"default_reasoning_level": "max"}},
			{"slug": "glm-5.3", "inherit": []any{"id@zcode/glm-5.3", "zcode/glm-5.3"}},
		},
	}
	base := &Manifest{Models: []map[string]any{
		{"slug": "zcode/glm-5.3", "context_window": 272000, "max_tokens": 120000},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "zc", Prefix: "zcode"},
		Models:  []ParsedModel{{ID: "glm-5.3"}},
	}}
	out := mergeManifest(base, fetched, cfg, s)
	for _, m := range out.Models {
		if m["slug"] == "glm-5.3" {
			if m["context_window"] != 1000000 || m["default_reasoning_level"] != "max" {
				t.Fatalf("bare static must inherit enriched pool entry: %+v", m)
			}
			return
		}
	}
	t.Fatal("bare static entry missing from output")
}
