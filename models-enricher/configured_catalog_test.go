package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestConfiguredSourcesAndStaticModels(t *testing.T) {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CustomChannels) != 0 {
		t.Fatalf("no custom channel expected after bare-model takeover: %v", cfg.CustomChannels)
	}
	// gmicloud 显式空链退出全局兜底。
	if got := sourceChain(cfg.Channels["gmicloud"], "x"); len(got) != 0 {
		t.Fatalf("gmicloud explicit empty chain must disable global fallback: %v", got)
	}
	// 未显式配置链的渠道在源级判断处回退全局链（sourceChain 仅在渠道存在时生效）。
	for name, want := range map[string][]string{
		"zcode":        {"models.dev/zai-coding-plan", "modelparams.dev/z-ai/subscription", "models.dev/zai"},
		"ollama-cloud": {"ollama_cloud", "models.dev/ollama-cloud"},
		"shuaiapi":     {"models.dev/anthropic"},
		"nim":          {"models.dev/nvidia"},
		"commandcode":  {"models.dev/openai", "models.dev/anthropic"},
		"gmicloud":     {},
	} {
		ch, exists := cfg.Channels[name]
		if !exists || !ch.fetchModelsEnabled() || !reflect.DeepEqual(ch.SourcePriority, want) {
			t.Fatalf("%s: explicit channel must fetch its own inventory with the requested sources", name)
		}
	}
	congee, exists := cfg.Channels["congee"]
	if !exists || congee.fetchModelsEnabled() || !reflect.DeepEqual(congee.SourcePriority, []string{"models.dev/openai"}) {
		t.Fatal("Congee must use only the explicit OpenAI source, without fetching its own inventory")
	}
	if !chainHas(cfg.Channels["xl"].SourcePriority, "models.dev/tencent") {
		t.Fatal("XL must include its explicit Tencent source")
	}
	wantModels := map[string]int{"xl": 1, "nim": 2}
	for name, ch := range cfg.Channels {
		if len(ch.Models) != wantModels[name] || len(ch.Overrides) != 0 || name != "commandcode" && name != "nim" && len(ch.providerPrefixes) != 0 {
			t.Fatalf("%s has unapproved model configuration, provider mapping or manual overrides", name)
		}
	}
	// 全局链与裸模型接管开关。
	if !cfg.BareModelsTakeover {
		t.Fatal("bare_models_takeover must be enabled")
	}
	wantGlobal := []string{"models.dev/zai", "models.dev/xai", "models.dev/moonshotai", "models.dev/openai", "models.dev/anthropic"}
	if !reflect.DeepEqual(cfg.GlobalSourcePriority, wantGlobal) {
		t.Fatalf("global chain changed: %v", cfg.GlobalSourcePriority)
	}
	// statics 只保留与动态源有真实差异的声明。
	wantInherit := map[string][]string{
		"gpt-5.6-terra": nil,
		"gpt-5.6-luna":  nil,
		"gpt-6-astra":   nil,
		"glm-5.2":       nil,
		"kimi-k3-256k":  {"kimi-k3"},
	}
	if len(cfg.StaticModels) != len(wantInherit) {
		t.Fatalf("static model count: got %d, want %d", len(cfg.StaticModels), len(wantInherit))
	}
	for _, static := range cfg.StaticModels {
		slug := asString(static["slug"])
		if _, exists := wantInherit[slug]; !exists {
			t.Fatalf("unexpected static model: %s", slug)
		}
		if got := inheritList(static["inherit"]); !reflect.DeepEqual(got, wantInherit[slug]) {
			t.Fatalf("unexpected static inherit for %s: %v", slug, got)
		}
		overrides, _ := static["overrides"].(map[string]any)
		switch slug {
		case "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6-astra", "kimi-k3-256k":
			if toInt(overrides["context_window"]) != 372000 && slug != "kimi-k3-256k" {
				t.Fatalf("gpt static %s must carry the 372000 context overrides: %v", slug, static)
			}
			if slug == "kimi-k3-256k" && toInt(overrides["context_window"]) != 256000 {
				t.Fatalf("kimi-k3-256k static must carry the 256000 context overrides: %v", static)
			}
		case "glm-5.2":
			if overrides["default_reasoning_level"] != "max" {
				t.Fatalf("glm-5.2 static must retain default_reasoning_level max: %v", static)
			}
		}
	}
	commandcode := cfg.Channels["commandcode"]
	if len(commandcode.providerPrefixes) != 4 {
		t.Fatal("Commandcode must use only the four verified provider aliases")
	}
	for from, to := range map[string]string{"MiniMaxAI": "minimax", "Qwen": "alibaba", "z-ai": "zai", "zai-org": "zai"} {
		if got := sourceQueries(commandcode, from+"/Model"); !reflect.DeepEqual(got, []sourceQuery{{"models.dev/" + to, "model", false}}) {
			t.Fatalf("unexpected provider query for %s: %#v", from, got)
		}
	}
	if cfg.Channels["ollama-cloud"].OllamaNativeBase != "https://ollama.com" {
		t.Fatal("Ollama source must retain its explicit native endpoint")
	}
	// 物化验证：静态声明在空来源下克隆 bySlug 并叠加 override。
	base := &Manifest{}
	base.Models = append(base.Models,
		map[string]any{"slug": "gpt-5.6-terra", "display_name": "cpa-bare"},
		map[string]any{"slug": "gpt-5.6-luna", "display_name": "cpa-bare"},
		map[string]any{"slug": "gpt-6-astra", "display_name": "cpa-bare"},
		map[string]any{"slug": "glm-5.2", "display_name": "cpa-bare"},
		map[string]any{"slug": "kimi-k3-256k", "display_name": "cpa-bare"},
		map[string]any{"slug": "kimi-k3", "display_name": "cpa-bare", "context_window": 1048576, "max_output_tokens": 131072},
	)
	identities := &catalogIdentities{qualified: map[string]bool{}}
	base, _ = identities.filter(base, cfg)
	out := mergeManifest(base, nil, cfg, emptySourceTables(), nil)
	bySlug := map[string]map[string]any{}
	for _, model := range out.Models {
		bySlug[asString(model["slug"])] = model
	}
	for slug, model := range bySlug {
		switch slug {
		case "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6-astra":
			if toInt(model["context_window"]) != 372000 {
				t.Fatalf("gpt static %s must override context to 372000: %v", slug, model)
			}
		case "glm-5.2":
			if model["default_reasoning_level"] != "max" {
				t.Fatalf("glm-5.2 must retain default_reasoning_level max: %v", model)
			}
		case "kimi-k3-256k":
			if toInt(model["context_window"]) != 256000 || toInt(model["max_output_tokens"]) != 131072 {
				t.Fatalf("kimi-k3-256k must inherit kimi-k3 and clamp context: %v", model)
			}
		}
	}
	if _, leaked := bySlug["nonexistent-static"]; leaked {
		t.Fatal("statics must not invent members")
	}

}

// 复用已录制的公开目录和来源缓存；不重新抓取目录、执行推理或证明渠道能力。
func TestCapturedSourceCompletion(t *testing.T) {
	dir := os.Getenv("CATALOG_SOURCE_COMPLETION_FIXTURE")
	if dir == "" {
		t.Skip("set CATALOG_SOURCE_COMPLETION_FIXTURE to the fixed catalog/source capture")
	}
	read := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	beforeConfig, err := loadConfig(filepath.Join(dir, "..", "config.before.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	afterConfig, err := loadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var base Manifest
	if err := decodeJSON(read("catalog.json"), &base); err != nil {
		t.Fatal(err)
	}
	tables := emptySourceTables()
	tables.setModelsDev(indexModelsDev(read("api.json")), indexModelsDevFlat(read("flat.json")))
	tables.mpK, tables.mpS = indexModelparams(read("params.json"), testLog())
	replay := func(cfg *Config) map[string]map[string]any {
		var packs []channelModels
		for _, prefix := range []string{"zcode", "xl", "congee", "commandcode"} {
			ch, exists := cfg.Channels[prefix]
			// 自身渠道元数据已包含在固定目录中；这里只重放真实的来源解析与合并。
			packs = append(packs, channelModels{Channel: Channel{Prefix: prefix, Type: "openai-compatibility"}, FetchSkipped: !exists || !ch.fetchModelsEnabled()})
		}
		by := map[string]map[string]any{}
		for _, row := range mergeManifest(&base, packs, cfg, tables, nil).Models {
			by[asString(row["slug"])] = row
		}
		return by
	}
	before, after := replay(beforeConfig), replay(afterConfig)
	if len(base.Models) != 189 || len(before) != 193 || len(after) != 193 {
		t.Fatalf("fixed snapshot membership changed: base=%d before=%d after=%d", len(base.Models), len(before), len(after))
	}
	wantOutput := map[string]string{
		"xl/muse-spark-1.3-contributor":          "131072",
		"commandcode/deepseek/deepseek-v4-flash": "384000", "commandcode/deepseek/deepseek-v4-flash-vision-exp": "384000", "commandcode/deepseek/deepseek-v4-pro": "384000",
		"commandcode/google/gemini-3.1-flash-lite": "65536", "commandcode/google/gemini-3.5-flash": "65536", "commandcode/google/gemini-3.5-flash-lite": "65536",
		"commandcode/google/gemini-3.6-flash": "65536", "commandcode/google/gemini-3.7-flash": "65536", "commandcode/google/gemini-3.8-flash": "65536",
		"commandcode/meta/muse-spark-1.1": "131072", "commandcode/meta/muse-spark-1.2": "131072", "commandcode/meta/muse-spark-1.2-contributor": "131072",
		"commandcode/meta/muse-spark-1.3": "131072", "commandcode/meta/muse-spark-1.3-contributor": "131072",
		"commandcode/MiniMaxAI/MiniMax-M2.5": "131072", "commandcode/MiniMaxAI/MiniMax-M2.7": "131072", "commandcode/MiniMaxAI/MiniMax-M3": "512000",
		"commandcode/moonshotai/Kimi-K2.6": "262144", "commandcode/moonshotai/Kimi-K2.7-Code": "262144", "commandcode/moonshotai/Kimi-K2.7-Code-Highspeed": "262144", "commandcode/moonshotai/Kimi-K3": "131072",
		"commandcode/nvidia/nemotron-3-ultra-550b-a55b": "65536",
		"commandcode/Qwen/Qwen3.6-Max-Preview":          "65536", "commandcode/Qwen/Qwen3.6-Plus": "65536", "commandcode/Qwen/Qwen3.7-Max": "65536", "commandcode/Qwen/Qwen3.7-Plus": "65536",
		"commandcode/Qwen/Qwen3.8-Flash": "131072", "commandcode/Qwen/Qwen3.8-Max": "131072",
		// 用户批准前缀映射覆盖这三个额外的既有型号；值来自同一固定 Alibaba 记录。
		"commandcode/Qwen/Qwen3.7-Flash": "65536", "commandcode/Qwen/Qwen3.8-27B": "32768", "commandcode/Qwen/Qwen3.8-Max-0902": "131072",
		"commandcode/sakana/fugu-ultra":      "1000000",
		"commandcode/stepfun/Step-3.5-Flash": "256000", "commandcode/stepfun/Step-3.7-Flash": "256000",
		"commandcode/xai/grok-4.5": "500000", "commandcode/xai/grok-4.6": "500000",
		"commandcode/xiaomi/mimo-v2.5": "131072", "commandcode/xiaomi/mimo-v2.5-pro": "131072",
		"commandcode/z-ai/glm-5.3-flash": "131072", "commandcode/zai-org/GLM-5": "131072", "commandcode/zai-org/GLM-5.1": "131072",
		"commandcode/zai-org/GLM-5.2": "131072", "commandcode/zai-org/GLM-5.3": "131072",
	}
	fields := []string{"context_window", "input_modalities", "output_modalities", "max_input_tokens", "max_tokens", "max_completion_tokens", "max_output_tokens", "supported_reasoning_levels", "default_reasoning_level"}
	changes := map[string]map[string]any{}
	var newOutput []string
	for slug, old := range before {
		row, exists := after[slug]
		if !exists {
			t.Fatalf("member removed: %s", slug)
		}
		prefix, id, _ := strings.Cut(slug, "/")
		if _, targeted := wantOutput[slug]; !targeted {
			if !reflect.DeepEqual(old, row) {
				t.Fatalf("out-of-scope metadata changed: %s", slug)
			}
			continue
		}
		for _, field := range []string{"max_tokens", "max_completion_tokens"} {
			a, aOK := old[field]
			b, bOK := row[field]
			if aOK != bOK || !reflect.DeepEqual(a, b) {
				t.Fatalf("independent token field changed: %s %s", slug, field)
			}
		}
		for _, field := range fields {
			for _, query := range sourceQueries(beforeConfig.Channels[prefix], id) {
				if hit, ok := tables.lookupQuery(query); ok && hit[field] != nil {
					if !reflect.DeepEqual(row[field], hit[field]) {
						t.Fatalf("earlier source lost priority: %s %s %s", slug, field, query.token)
					}
					break
				}
			}
			if !reflect.DeepEqual(old[field], row[field]) {
				if changes[slug] == nil {
					changes[slug] = map[string]any{}
				}
				changes[slug][field] = map[string]any{"before": old[field], "after": row[field]}
			}
		}
		if _, existed := old["max_output_tokens"]; !existed && row["max_output_tokens"] != nil {
			newOutput = append(newOutput, slug)
		}
	}
	for slug, output := range wantOutput {
		row := after[slug]
		prefix, id, _ := strings.Cut(slug, "/")
		maker, modelID, _ := strings.Cut(id, "/")
		provider := strings.ToLower(maker)
		if alias := map[string]string{"MiniMaxAI": "minimax", "Qwen": "alibaba", "z-ai": "zai", "zai-org": "zai"}[maker]; alias != "" {
			provider = alias
		}
		if prefix == "xl" {
			provider, modelID = "meta", id
		}
		token := "models.dev/" + provider
		queries := sourceQueries(afterConfig.Channels[prefix], id)
		if len(queries) != 1 || queries[0].token != token || queries[0].explicit {
			t.Fatalf("incorrect manufacturer query: %s %#v", slug, queries)
		}
		// Nvidia 的 API 原始 ID 已包含 provider；其 API 限额仍须优先于 flat。
		if maker == "nvidia" {
			modelID = id
		}
		declared, declaredOK := tables.lookupOne(token, modelID)
		hit, hitOK := tables.lookupQuery(queries[0])
		if !declaredOK || !hitOK || !reflect.DeepEqual(hit["id"], declared["id"]) || !reflect.DeepEqual(prefixFields(hit), prefixFields(declared)) {
			t.Fatalf("declared source identity/fields not retained: %s", slug)
		}
		for field, value := range prefixFields(hit) {
			if !reflect.DeepEqual(row[field], value) {
				t.Fatalf("source field not applied: %s %s", slug, field)
			}
		}
		if row["max_output_tokens"] != json.Number(output) || !reflect.DeepEqual(row["output_modalities"], []any{"text"}) {
			t.Fatalf("declared output fields not applied: %s", slug)
		}
		if strings.HasPrefix(slug, "commandcode/stepfun/") && row["max_input_tokens"] != json.Number("256000") {
			t.Fatalf("declared input limit not applied: %s", slug)
		}
	}
	// 逐项验证全部目标的最终值；旧配置在新查询规则下也会自动补全，不能要求全部是本轮新增。
	for _, slug := range newOutput {
		if _, expected := wantOutput[slug]; !expected {
			t.Fatalf("unexpected new output field: %s", slug)
		}
	}
	sort.Strings(newOutput)
	counts := func(rows map[string]map[string]any) map[string]int {
		out := map[string]int{}
		for _, row := range rows {
			for _, field := range []string{"output_modalities", "max_input_tokens", "max_output_tokens", "max_completion_tokens"} {
				if _, exists := row[field]; exists {
					out[field]++
				}
			}
		}
		return out
	}
	report, err := json.MarshalIndent(map[string]any{"members": len(after), "before": counts(before), "after": counts(after), "new_output": newOutput, "changed_fields": changes, "earlier_priority_preserved": true, "other_channels_unchanged": true}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "..", "merge-result.json"), report, 0600); err != nil {
		t.Fatal(err)
	}
}
