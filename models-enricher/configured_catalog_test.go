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
	pool, exists := cfg.CustomChannels["static-parents"]
	if len(cfg.CustomChannels) != 1 || !exists {
		t.Fatalf("static parent pool must be the only custom channel: %v", cfg.CustomChannels)
	}
	if !reflect.DeepEqual(pool.SourcePriority, []string{"models.dev/openai", "models.dev/xai", "models.dev/zai-coding-plan", "modelparams.dev/z-ai/subscription", "models.dev/zai"}) ||
		len(pool.Models) != 7 || len(pool.Overrides) != 0 || len(pool.providerPrefixes) != 0 {
		t.Fatalf("static parent pool must use only the declared subscription sources: %+v", pool)
	}
	for name, model := range pool.Models {
		if len(model.SourcePriority) != 0 || len(model.LookupIDs) != 0 || len(model.Overrides) != 0 || model.MetadataFrom != "" {
			t.Fatalf("static parent %s must stay a plain virtual entry", name)
		}
	}
	for name, want := range map[string][]string{
		"zcode":        {"models.dev/zai-coding-plan", "modelparams.dev/z-ai/subscription", "models.dev/zai"},
		"ollama-cloud": {"ollama_cloud", "models.dev/ollama-cloud"},
		"shuaiapi":     {"models.dev/anthropic"},
		"nim":          {"models.dev/nvidia"},
		"commandcode":  {"models.dev/openai", "models.dev/anthropic"},
		"gmicloud":     nil,
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
	muse := cfg.Channels["xl"].Models["muse-spark-1.3-contributor"]
	if !reflect.DeepEqual(muse.SourcePriority, []string{"models.dev/meta"}) ||
		len(muse.LookupIDs) != 0 ||
		muse.MetadataFrom != "" || len(muse.Overrides) != 0 {
		t.Fatal("XL Muse must retain its Meta chain without redundant same-name bindings or manual overrides")
	}
	nim := cfg.Channels["nim"]
	if len(nim.providerPrefixes) != 2 {
		t.Fatal("NIM must use only the two approved NVIDIA provider mappings")
	}
	for _, id := range []string{"deepseek-ai/deepseek-v4-flash-0731", "minimaxai/minimax-m3"} {
		model := nim.Models[id]
		if len(model.SourcePriority) != 0 || len(model.Overrides) != 0 || model.MetadataFrom != "" || !reflect.DeepEqual(model.LookupIDs, map[string]string{"models.dev/nvidia": id}) {
			t.Fatalf("NIM must retain only the full-ID lookup for %s", id)
		}
		if got := sourceQueries(nim, id); !reflect.DeepEqual(got, []sourceQuery{{"models.dev/nvidia", id, true}}) {
			t.Fatalf("NIM full-ID query changed: %#v", got)
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
	want := map[string]string{
		"gpt-5.6-terra": "static-parents/gpt-5.6-terra",
		"gpt-5.6-luna":  "static-parents/gpt-5.6-luna",
		"gpt-6-astra":   "static-parents/gpt-6-astra",
		"grok-4.6":      "static-parents/grok-4.6",
		"glm-5.2":       "static-parents/glm-5.2",
		"glm-5.3":       "static-parents/glm-5.3",
		"glm-5.3-flash": "static-parents/glm-5.3-flash",
	}
	if len(cfg.StaticModels) != len(want) {
		t.Fatalf("static model count: got %d, want %d", len(cfg.StaticModels), len(want))
	}
	base := &Manifest{}
	seen := map[string]bool{}
	for _, static := range cfg.StaticModels {
		slug := asString(static["slug"])
		parent, exists := want[slug]
		if !exists || seen[slug] || !reflect.DeepEqual(inheritList(static["inherit"]), []string{parent}) {
			t.Fatalf("unexpected static mapping: %s", slug)
		}
		overrides, _ := static["overrides"].(map[string]any)
		if slug == "grok-4.6" || strings.HasPrefix(slug, "glm-") {
			if len(static) != 2 {
				t.Fatalf("%s must stay a pure source inherit: %v", slug, static)
			}
		} else if len(static) != 3 || toInt(overrides["context_window"]) != 372000 || toInt(overrides["max_context_window"]) != 372000 {
			t.Fatalf("gpt static %s must carry the 372000 context overrides: %v", slug, static)
		}
		seen[slug] = true
		// 虚空父项不进base：只存在于custom pool，不依赖任何真实CPA成员。
		// 裸名假条目仍放入，证明CPA原始数据不会泄漏进静态结果。
		base.Models = append(base.Models, map[string]any{"slug": slug, "leaked_from_cpa_bare": true})
	}
	// 裸名全部被身份过滤；static只能从虚空池取数。
	identities := &catalogIdentities{qualified: map[string]bool{}}
	base, _ = identities.filter(base, cfg)
	out := mergeManifest(base, nil, cfg, emptySourceTables(), nil)
	if len(out.Models) != len(seen) {
		t.Fatalf("virtual parents leaked or static members lost: %v", out.Models)
	}
	bare := map[string]bool{}
	for _, model := range out.Models {
		slug := asString(model["slug"])
		if _, leak := model["leaked_from_cpa_bare"]; leak || (model["id"] != nil && model["id"] != slug) {
			t.Fatalf("static %s must use only its virtual pool, not CPA bare data", slug)
		}
		bare[slug] = true
	}
	if !reflect.DeepEqual(bare, seen) {
		t.Fatal("the explicit static models changed")
	}
	// 虚空池成员不进公开清单，但必须能用真实来源补齐静态元数据。
	tables := emptySourceTables()
	tables.setModelsDev(indexModelsDev([]byte(`{"openai":{"models":{
		"gpt-5.6-terra":{"name":"GPT-5.6 Terra","limit":{"context":1050000,"input":922000,"output":128000}},
		"gpt-5.6-luna":{"name":"GPT-5.6 Luna","limit":{"context":1050000,"input":922000,"output":128000}},
		"gpt-6-astra":{"name":"GPT-6 Astra","limit":{"context":1050000,"input":922000,"output":128000}}}
	},"xai":{"models":{"grok-4.6":{"name":"Grok 4.6","limit":{"context":500000,"output":500000}}}},
	"zai-coding-plan":{"models":{
		"glm-5.2":{"name":"GLM-5.2","limit":{"context":1000000,"output":131072}},
		"glm-5.3":{"name":"GLM-5.3","limit":{"context":1000000,"output":131072}},
		"glm-5.3-flash":{"name":"GLM-5.3-Flash","limit":{"context":1000000,"output":131072}}}}
	}`)), nil)
	out = mergeManifest(base, nil, cfg, tables, nil)
	if len(out.Models) != len(seen) {
		t.Fatalf("enriched pass changed membership: %v", out.Models)
	}
	for _, model := range out.Models {
		slug := asString(model["slug"])
		if slug == "grok-4.6" {
			if toInt(model["context_window"]) != 500000 || model["display_name"] != "Grok 4.6" {
				t.Fatalf("grok-4.6 must inherit xai source: %v", model)
			}
			continue
		}
		if strings.HasPrefix(slug, "glm-") {
			if toInt(model["context_window"]) != 1000000 || toInt(model["max_output_tokens"]) != 131072 || model["display_name"] == nil {
				t.Fatalf("glm static %s must inherit the zai-coding-plan source: %v", slug, model)
			}
			continue
		}
		if toInt(model["context_window"]) != 372000 || toInt(model["max_input_tokens"]) != 922000 || toInt(model["max_output_tokens"]) != 128000 || model["display_name"] == nil {
			t.Fatalf("static %s must override context to 372000 and inherit the rest: %v", slug, model)
		}
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
