package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHit 便捷地向命名空间表写入（自动建内层 map）。
func setHit(table map[string]map[string]sourceHit, prov, id string, h sourceHit) {
	if table[prov] == nil {
		table[prov] = map[string]sourceHit{}
	}
	table[prov][id] = h
}

// ---------- token / chain ----------

func TestValidSourceToken(t *testing.T) {
	valid := []string{
		"ollama_cloud",
		"models.dev/openai",
		"models.dev/deepseek",
		"modelparams.dev/openai/api_key",
		"modelparams.dev/openai/subscription",
	}
	for _, tok := range valid {
		if !validSourceToken(tok) {
			t.Fatalf("%q should be valid", tok)
		}
	}
	invalid := []string{
		"", "models.dev", "models.dev/", "models.dev/a/b",
		"modelparams.dev/openai", "modelparams.dev/openai/",
		"modelparams.dev//api_key", "modelparams.dev/a/b/api_key",
		"models_dev", "modelparams_api_key", // 旧扁平 token 不再合法
	}
	for _, tok := range invalid {
		if validSourceToken(tok) {
			t.Fatalf("%q should be invalid", tok)
		}
	}
}

func TestSourceChainTwoLevel(t *testing.T) {
	ch := ChannelConfig{
		SourcePriority: []string{"modelparams.dev/zhipuai/api_key"},
		Models: map[string]ModelConfig{
			"m1": {SourcePriority: []string{"modelparams.dev/zhipuai/subscription"}},
		},
	}
	if got := sourceChain(ch, "m1"); got[0] != "modelparams.dev/zhipuai/subscription" {
		t.Fatalf("model-level should win wholesale: %v", got)
	}
	if got := sourceChain(ch, "m2"); got[0] != "modelparams.dev/zhipuai/api_key" {
		t.Fatalf("channel-level fallback expected: %v", got)
	}
	if got := sourceChain(ChannelConfig{}, "m3"); len(got) != 0 {
		t.Fatalf("no global/builtin default allowed: %v", got)
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

// ---------- loadConfig 拒绝路径 ----------

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigRejectsIDAt(t *testing.T) {
	_, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
static_models:
  - slug: alias
    inherit: [id@foo]
`))
	if err == nil || !strings.Contains(err.Error(), "id@") {
		t.Fatalf("id@ must be rejected: %v", err)
	}
}

func TestLoadConfigRejectsStaticSourcePriority(t *testing.T) {
	_, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
static_models:
  - slug: alias
    source_priority: [models.dev/openai]
    inherit: [pool/m]
`))
	if err == nil || !strings.Contains(err.Error(), "source_priority") {
		t.Fatalf("static source_priority must be rejected: %v", err)
	}
}

func TestLoadConfigRejectsInvalidToken(t *testing.T) {
	_, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
channels:
  axis:
    source_priority: [models_dev]
`))
	if err == nil || !strings.Contains(err.Error(), "invalid source token") {
		t.Fatalf("old flat token must be rejected: %v", err)
	}
}

func TestLoadConfigRejectsCustomOllama(t *testing.T) {
	_, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
custom_channels:
  pool:
    source_priority: [ollama_cloud]
    models: {m: {}}
`))
	if err == nil || !strings.Contains(err.Error(), "ollama_cloud") {
		t.Fatalf("ollama_cloud in custom channel must be rejected: %v", err)
	}
}

func TestLoadConfigRejectsEmptyCustomChain(t *testing.T) {
	// 无来源的成员必须内联声明元数据；纯空成员拒绝。
	_, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
custom_channels:
  pool:
    models: {m: {}}
`))
	if err == nil || !strings.Contains(err.Error(), "inline overrides") {
		t.Fatalf("source-free member without inline overrides must be rejected: %v", err)
	}
	// 带内联声明的无来源成员合法（虚空创造）。
	cfg, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
custom_channels:
  pool:
    models: {m: {overrides: {display_name: M}}}
`))
	if err != nil || len(cfg.CustomChannels["pool"].Models) != 1 {
		t.Fatalf("void-created member must load: %v", err)
	}
}

func TestLoadConfigRejectsChannelCustomOverlap(t *testing.T) {
	_, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
channels:
  axis: {source_priority: [models.dev/openai]}
custom_channels:
  axis:
    source_priority: [models.dev/openai]
    models: {m: {}}
`))
	if err == nil {
		t.Fatal("channels/custom_channels key overlap must be rejected")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
channels:
  axis: {source_priority: [models.dev/openai]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPConcurrency != 8 {
		t.Fatalf("default http_concurrency must be 8: %d", cfg.HTTPConcurrency)
	}
	if cfg.ChannelTimeout <= 0 || cfg.OverallDeadline <= 0 {
		t.Fatal("default timeouts must be positive")
	}
}

// ---------- validateRuntime ----------

func chanOf(name, prefix string) Channel {
	return Channel{Type: "openai-compatibility", Name: name, Prefix: prefix, BaseURL: "http://x/v1", AuthIndex: "k"}
}

func TestValidateRuntimeDuplicatePrefix(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{
		"same": {SourcePriority: []string{"models.dev/openai"}},
	}}
	channels := []Channel{chanOf("A", "same"), chanOf("B", "same")}
	err := validateRuntime(cfg, channels, &Manifest{})
	ce, ok := err.(*configError)
	if !ok || ce.code != "catalog_configuration_conflict" {
		t.Fatalf("duplicate prefix must conflict: %v", err)
	}
}

func TestValidateRuntimeEmptyPrefix(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{}}
	channels := []Channel{chanOf("NoPrefix", "")}
	err := validateRuntime(cfg, channels, &Manifest{})
	ce, ok := err.(*configError)
	if !ok || ce.code != "catalog_configuration_conflict" {
		t.Fatalf("empty prefix must conflict (unusable channel): %v", err)
	}
}

func TestValidateRuntimeEmptyNameOK(t *testing.T) {
	// name 仅展示元数据：claude-api-key 等 kind 永远无 name，不得报错
	cfg := &Config{Channels: map[string]ChannelConfig{
		"gmicloud": {SourcePriority: []string{"models.dev/minimax"}},
	}}
	channels := []Channel{chanOf("", "gmicloud")}
	if err := validateRuntime(cfg, channels, &Manifest{}); err != nil {
		t.Fatalf("empty name must be accepted: %v", err)
	}
}

func TestValidateRuntimeUnconfiguredChannelUsesDefaults(t *testing.T) {
	channels := []Channel{chanOf("Axis", "axis")}
	for _, cfg := range []*Config{{}, {Channels: map[string]ChannelConfig{"axis": {}}}} {
		if err := validateRuntime(cfg, channels, &Manifest{}); err != nil {
			t.Fatalf("missing/empty channel config must use channel-only defaults: %v", err)
		}
	}
}

func TestValidateRuntimeOllamaNeedsNativeBase(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{
		"oc": {SourcePriority: []string{"ollama_cloud"}},
	}}
	err := validateRuntime(cfg, []Channel{chanOf("OC", "oc")}, &Manifest{})
	if ce, ok := err.(*configError); !ok || ce.code != "catalog_configuration_missing" {
		t.Fatalf("ollama without native base must be missing-config: %v", err)
	}
}

func TestValidateRuntimeCustomCollisions(t *testing.T) {
	cfg := &Config{
		Channels: map[string]ChannelConfig{
			"axis": {SourcePriority: []string{"models.dev/openai"}},
		},
		CustomChannels: map[string]ChannelConfig{
			"axis":  {SourcePriority: []string{"models.dev/openai"}}, // 与 prefix 冲突
			"groq":  {SourcePriority: []string{"models.dev/groq"}},   // 与 native prefix 冲突
			"clean": {SourcePriority: []string{"models.dev/openai"}},
		},
	}
	base := &Manifest{Models: []map[string]any{{"slug": "groq/m1"}}}
	err := validateRuntime(cfg, []Channel{chanOf("Axis", "axis")}, base)
	ce, ok := err.(*configError)
	if !ok || ce.code != "catalog_configuration_conflict" {
		t.Fatalf("custom collisions expected: %v", err)
	}
	msg := ce.Error()
	if !strings.Contains(msg, "axis") || !strings.Contains(msg, "groq") {
		t.Fatalf("both collisions must be reported: %s", msg)
	}
	if strings.Contains(msg, "clean") {
		t.Fatalf("non-conflicting custom channel must not be reported: %s", msg)
	}
}

// ---------- sources 索引与精确查找 ----------

func TestIndexModelsDevNamespaced(t *testing.T) {
	raw := []byte(`{
		"digitalocean": {"models": {"glm-5.3": {"id":"glm-5.3","limit":{"context":1048576,"output":1048576}}}},
		"zhipuai": {"models": {"glm-5.3": {"id":"glm-5.3","limit":{"context":1000000,"output":131072}}}}
	}`)
	out := indexModelsDev(raw)
	if h := out["zhipuai"]["glm-5.3"]; toInt(h["context_window"]) != 1000000 || toInt(h["max_output_tokens"]) != 131072 {
		t.Fatalf("zhipuai namespace: %+v", h)
	}
	if h := out["digitalocean"]["glm-5.3"]; toInt(h["context_window"]) != 1048576 {
		t.Fatalf("digitalocean namespace: %+v", h)
	}
}

func TestIndexModelsDevFlat(t *testing.T) {
	raw := []byte(`{"openai/gpt-5.6-sol": {"limit":{"context":1050000,"input":922000,"output":128000}}, "bare-key": {"limit":{"context":1}}}`)
	out := indexModelsDevFlat(raw)
	h := out["openai"]["gpt-5.6-sol"]
	if toInt(h["context_window"]) != 1050000 || toInt(h["max_input_tokens"]) != 922000 || toInt(h["max_output_tokens"]) != 128000 {
		t.Fatalf("flat provider/model key fields: %+v", h)
	}
	if _, ok := out["bare-key"]; ok {
		t.Fatal("bare key without provider must be skipped, not guessed")
	}
}

func TestMergeSourceMapsPreservesFirstSourceFields(t *testing.T) {
	dst := map[string]map[string]sourceHit{}
	mergeSourceMaps(dst, map[string]map[string]sourceHit{
		"openai": {"m": {"context_window": 1050000, "max_input_tokens": 922000, "max_tokens": 128000, "input_modalities": []string{"text", "image"}}},
	})
	mergeSourceMaps(dst, map[string]map[string]sourceHit{
		"openai": {"m": {"context_window": 272000, "max_input_tokens": 1, "max_tokens": 1, "supported_reasoning_levels": effortsToLevels([]any{"high"})}},
	})
	h := dst["openai"]["m"]
	if h["context_window"] != 1050000 || h["max_input_tokens"] != 922000 || h["max_tokens"] != 128000 {
		t.Fatalf("first source fields must win: %+v", h)
	}
	assertMetadataJSON(t, h["supported_reasoning_levels"], `[{"effort":"high"}]`)
}

func TestIndexModelparamsAuthTypeSplitAndOptionalProvider(t *testing.T) {
	log := testLog()
	raw := []byte(`{"models": [
		{"model":"m1","provider":"zai","authType":"subscription","params":[]},
		{"model":"m2","provider":"zai","authType":"api_key","params":[]},
		{"model":"m3","provider":"zai","params":[]},
		{"model":"orphan","params":[]}
	]}`)
	k, s := indexModelparams(raw, log)
	if _, ok := s["zai"]["m1"]; !ok {
		t.Fatal("m1 must be in subscription namespace")
	}
	if _, ok := k["zai"]["m2"]; !ok {
		t.Fatal("m2 must be in api_key namespace")
	}
	if _, ok := k["zai"]["m3"]; !ok {
		t.Fatal("m3 (no authType) must be indexed in both")
	}
	if _, ok := s["zai"]["m3"]; !ok {
		t.Fatal("m3 (no authType) must be indexed in both")
	}
	if _, ok := k["zai"]["m1"]; ok {
		t.Fatal("m1 must NOT leak into api_key namespace")
	}
	if _, ok := k[""]["orphan"]; !ok {
		t.Fatal("provider-less entry must remain available to bare-ID queries")
	}
	tables := &SourceTables{mpK: k, mpS: s}
	if _, ok := tables.lookupQuery(sourceQuery{token: "modelparams.dev//api_key", id: "orphan"}); !ok {
		t.Fatal("bare-ID query lost provider-less record")
	}
	if _, ok := tables.lookupQuery(sourceQuery{token: "modelparams.dev/zai/api_key", id: "orphan"}); ok {
		t.Fatal("provider-scoped query borrowed provider-less record")
	}
}

func TestLookupOneExactThenUniqueCaseFold(t *testing.T) {
	tables := emptySourceTables()
	tables.dev["openai"] = map[string]sourceHit{
		"gpt-5.6-sol": {"context_window": 1050000},
		"Model":       {"context_window": 1}, "model": {"context_window": 2},
	}
	tables.dev["openrouter"] = map[string]sourceHit{"openai/gpt-5.6-sol": {"context_window": 999}}
	tables.mpK["openai"] = map[string]sourceHit{"Params": {"context_window": 0}}
	tables.mpS["openai"] = map[string]sourceHit{"Params": {"context_window": 3}}
	for _, tc := range []struct {
		name, token, id string
		want            int
		found           bool
	}{
		{"exact", "models.dev/openai", "gpt-5.6-sol", 1050000, true},
		{"case-fold", "models.dev/openai", "GPT-5.6-SOL", 1050000, true},
		{"exact-upper-collision", "models.dev/openai", "Model", 1, true},
		{"exact-lower-collision", "models.dev/openai", "model", 2, true},
		{"ambiguous", "models.dev/openai", "MODEL", 0, false},
		{"missing", "models.dev/openai", "absent", 0, false},
		{"namespace", "models.dev/anthropic", "GPT-5.6-SOL", 0, false},
		{"provider-token", "models.dev/OPENAI", "GPT-5.6-SOL", 0, false},
		{"preserve-prefix", "models.dev/openai", "openai/GPT-5.6-SOL", 0, false},
		{"preserve-tag", "models.dev/openai", "GPT-5.6-SOL:free", 0, false},
		{"preserve-date", "models.dev/openai", "GPT-5.6-SOL-20260908", 0, false},
		{"preserve-variant", "models.dev/openai", "GPT-5.6-SOL-fast", 0, false},
		{"qualified-source-id", "models.dev/openrouter", "OPENAI/GPT-5.6-SOL", 999, true},
		{"api-key-zero", "modelparams.dev/openai/api_key", "PARAMS", 0, true},
		{"subscription", "modelparams.dev/openai/subscription", "params", 3, true},
		{"non-bulk-source", "ollama_cloud", "x", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hit, ok := tables.lookupOne(tc.token, tc.id)
			if ok != tc.found || ok && hit["context_window"] != tc.want {
				t.Fatalf("lookup %s %q: %+v ok=%v; want %d ok=%v", tc.token, tc.id, hit, ok, tc.want, tc.found)
			}
		})
	}
}

func TestOllamaContextLengthParse(t *testing.T) {
	body := []byte(`{"model_info": {"deepseek.context_length": 262144, "other.field": "x"}}`)
	hit, err := parseOllamaMetadata(body)
	if err != nil || toInt(hit["context_window"]) != 262144 {
		t.Fatalf("context_length suffix scan: %+v, %v", hit, err)
	}
	hit, err = parseOllamaMetadata([]byte(`{"model_info": {}}`))
	if err != nil || hit["context_window"] != nil {
		t.Fatalf("missing context_length must stay missing: %+v, %v", hit, err)
	}
	if _, err := parseOllamaMetadata([]byte(`not json`)); err == nil {
		t.Fatal("bad json must fail parsing")
	}
}

// ---------- merge ----------

func chanCfg(chain ...string) ChannelConfig { return ChannelConfig{SourcePriority: chain} }

func TestMergeProviderChainPrecedence(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.mpS, "zhipuai", "m", sourceHit{"max_tokens": 200, "context_window": 2000})
	setHit(tables.mpK, "zhipuai", "m", sourceHit{"max_tokens": 100})
	setHit(tables.dev, "zhipuai", "m", sourceHit{"display_name": "DevName", "context_window": 9999})
	cfg := &Config{Channels: map[string]ChannelConfig{
		"c": chanCfg("modelparams.dev/zhipuai/subscription", "modelparams.dev/zhipuai/api_key", "models.dev/zhipuai"),
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c", Type: "openai-compatibility"},
		Models:  []ParsedModel{{ID: "m", Metadata: map[string]any{"display_name": "Parsed", "context_window": 272000, "max_input_tokens": 200000}}},
	}}
	out := mergeManifest(&Manifest{Models: []map[string]any{{"slug": "c/m"}}}, fetched, cfg, tables, nil)
	m := out.Models[0]
	if m["slug"] != "c/m" || m["display_name"] != "DevName" {
		t.Fatalf("explicit source metadata must overlay the channel: %+v", m)
	}
	if m["max_tokens"] != 200 || m["context_window"] != 2000 {
		t.Fatalf("subscription must precede api_key and override channel capability: %+v", m)
	}
	if m["max_input_tokens"] != 200000 {
		t.Fatalf("source miss must retain the channel max_input_tokens: %+v", m)
	}
}

func TestMergeSourceAuthoritativeAndMetadataReference(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "xai", "grok-4.6", sourceHit{
		"context_window": 500000, "max_input_tokens": 400000, "max_tokens": 500000,
		"input_modalities": []string{"text", "image"}, "supported_reasoning_levels": effortsToLevels([]any{"low", "high"}),
	})
	cfg := &Config{Channels: map[string]ChannelConfig{
		"xl": {
			SourcePriority: []string{"models.dev/xai"},
			Models: map[string]ModelConfig{
				"grok-4.6": {MetadataFrom: "supergrok/grok-4.6"},
			},
		},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "XL", Prefix: "xl"},
		Models:  []ParsedModel{{ID: "grok-4.6", Metadata: map[string]any{"context_window": 272000, "max_input_tokens": 272000, "max_tokens": 65536}}},
	}, {
		Channel: Channel{Name: "Super", Prefix: "supergrok"},
		Models:  []ParsedModel{{ID: "grok-4.6", Metadata: map[string]any{"context_window": 500000, "max_input_tokens": 400000, "max_tokens": 65536, "display_name": "Grok 4.6"}}},
	}}
	out := mergeManifest(nil, fetched, cfg, tables, nil)
	by := map[string]map[string]any{}
	for _, m := range out.Models {
		by[asString(m["slug"])] = m
	}
	xl := by["xl/grok-4.6"]
	if xl["context_window"] != 500000 || xl["max_input_tokens"] != 400000 || xl["max_tokens"] != 65536 {
		t.Fatalf("source/reference metadata precedence broken: %+v", xl)
	}
	if xl["slug"] != "xl/grok-4.6" {
		t.Fatalf("metadata reference must not change destination slug: %+v", xl)
	}
}

func TestMergeMissingMetadataReferencePreservesCapabilities(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{
		"xl": {
			SourcePriority: []string{"models.dev/xai"},
			Models: map[string]ModelConfig{
				"grok-4.6": {MetadataFrom: "supergrok/grok-4.6"},
			},
		},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "XL", Prefix: "xl"},
		Models:  []ParsedModel{{ID: "grok-4.6", Metadata: map[string]any{"context_window": 272000, "max_input_tokens": 272000, "max_tokens": 65536}}},
	}}
	out := mergeManifest(nil, fetched, cfg, emptySourceTables(), nil)
	m := out.Models[0]
	for key, want := range map[string]int{"context_window": 272000, "max_input_tokens": 272000, "max_tokens": 65536} {
		if m[key] != want {
			t.Fatalf("missing metadata reference must retain %s: %+v", key, m)
		}
	}
}

func TestMergeModelLevelChainReplacesChannel(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.mpS, "zhipuai", "m", sourceHit{"max_tokens": 200})
	setHit(tables.mpK, "zhipuai", "m", sourceHit{"max_tokens": 100})
	cfg := &Config{Channels: map[string]ChannelConfig{
		"c": {SourcePriority: []string{"modelparams.dev/zhipuai/subscription"},
			Models: map[string]ModelConfig{
				"m": {SourcePriority: []string{"modelparams.dev/zhipuai/api_key"},
					Overrides: map[string]any{"max_tokens": 42}},
			}},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c"},
		Models:  []ParsedModel{{ID: "m"}},
	}}
	out := mergeManifest(nil, fetched, cfg, tables, nil)
	if out.Models[0]["max_tokens"] != 42 {
		t.Fatalf("overrides must win last: %+v", out.Models[0])
	}
}

func TestMergeLookupIDsPerSource(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "deepseek", "deepseek-v4-flash", sourceHit{"context_window": 128000})
	setHit(tables.mpK, "deepseek", "DEEPSEEK-V4-FLASH:PREVIEW", sourceHit{"max_tokens": 8192})
	cfg := &Config{Channels: map[string]ChannelConfig{
		"oc": {SourcePriority: []string{"models.dev/deepseek", "modelparams.dev/deepseek/api_key"},
			Models: map[string]ModelConfig{
				"deepseek-v4-flash:preview": {LookupIDs: map[string]string{
					"models.dev/deepseek": "DEEPSEEK-V4-FLASH",
				}},
			}},
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "OC", Prefix: "oc"},
		Models:  []ParsedModel{{ID: "deepseek-v4-flash:preview"}},
	}}
	out := mergeManifest(nil, fetched, cfg, tables, nil)
	m := out.Models[0]
	// canonical 与显式lookup_ids都忽略大小写；只有显式映射才能改变tag。
	if m["context_window"] != 128000 || m["max_tokens"] != 8192 {
		t.Fatalf("per-source lookup_ids broken: %+v", m)
	}
	// tag 必须原样保留在 slug
	if m["slug"] != "oc/deepseek-v4-flash:preview" {
		t.Fatalf("colon tag must survive: %v", m["slug"])
	}
}

func TestMergeOllamaHit(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "deepseek", "m", sourceHit{"context_window": 999999, "max_tokens": 8192})
	ollama := map[string]map[string]sourceHit{
		"oc": {"m": {"context_window": 262144}},
	}
	cfg := &Config{Channels: map[string]ChannelConfig{
		"oc": chanCfg("ollama_cloud", "models.dev/deepseek"),
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "OC", Prefix: "oc"},
		Models:  []ParsedModel{{ID: "m"}},
	}}
	out := mergeManifest(nil, fetched, cfg, tables, ollama)
	m := out.Models[0]
	if m["context_window"] != 262144 {
		t.Fatalf("ollama first in chain must win context: %+v", m)
	}
	if m["max_tokens"] != 8192 {
		t.Fatalf("dev fills gaps after ollama: %+v", m)
	}
}

func TestMergeCustomPoolHiddenAndStaticInherit(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "openai", "gpt-5.6-sol", sourceHit{"context_window": 1050000, "max_tokens": 128000})
	cfg := &Config{
		Channels: map[string]ChannelConfig{
			"c": chanCfg("models.dev/openai"),
		},
		CustomChannels: map[string]ChannelConfig{
			"openai-catalog": {
				SourcePriority: []string{"models.dev/openai"},
				Models: map[string]ModelConfig{
					"gpt-5.6-sol": {Overrides: map[string]any{"display_name": "GPT-5.6 Sol"}},
					"hidden-m":    {},
				},
			},
		},
		StaticModels: []map[string]any{{
			"slug":    "gpt-5.6-sol",
			"inherit": []any{"openai-catalog/gpt-5.6-sol", "c/m"},
		}},
	}
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c"},
		Models:  []ParsedModel{{ID: "m", Metadata: map[string]any{"context_window": 777}}},
	}}
	out := mergeManifest(nil, fetched, cfg, tables, nil)
	slugs := map[string]map[string]any{}
	for _, m := range out.Models {
		slugs[asString(m["slug"])] = m
	}
	if _, leaked := slugs["openai-catalog/gpt-5.6-sol"]; leaked {
		t.Fatal("custom pool entry must not appear in output")
	}
	if _, leaked := slugs["openai-catalog/hidden-m"]; leaked {
		t.Fatal("custom pool model without static consumer must not appear")
	}
	alias := slugs["gpt-5.6-sol"]
	if alias == nil {
		t.Fatal("static alias missing")
	}
	// custom pool 在公开池之前解析 → ctx 取 custom 的 1050000
	if alias["context_window"] != 1050000 || alias["display_name"] != "GPT-5.6 Sol" {
		t.Fatalf("static must inherit custom pool: %+v", alias)
	}
	if alias["max_tokens"] != 128000 {
		t.Fatalf("custom enrichment fields must flow: %+v", alias)
	}
}

func TestMergeStaticInheritPublicPoolAndMiss(t *testing.T) {
	cfg := &Config{
		StaticModels: []map[string]any{
			{"slug": "alias-m", "inherit": []any{"c/m"}, "overrides": map[string]any{"description": "d"}},
			{"slug": "ghost", "inherit": []any{"nowhere/x"}, "overrides": map[string]any{"display_name": "Ghost"}},
		},
	}
	fetched := []channelModels{{
		Channel: Channel{Name: "c", Prefix: "c"},
		Models:  []ParsedModel{{ID: "m", Metadata: map[string]any{"context_window": 1234}}},
	}}
	out := mergeManifest(nil, fetched, cfg, emptySourceTables(), nil)
	by := map[string]map[string]any{}
	for _, m := range out.Models {
		by[asString(m["slug"])] = m
	}
	if by["alias-m"]["context_window"] != 1234 || by["alias-m"]["description"] != "d" {
		t.Fatalf("public pool inherit broken: %+v", by["alias-m"])
	}
	if by["ghost"]["display_name"] != "Ghost" {
		t.Fatalf("miss must fail-open with overrides: %+v", by["ghost"])
	}
}

func TestMergeDoesNotInferIdentityFromSlashes(t *testing.T) {
	base := &Manifest{Models: []map[string]any{
		{"slug": "bare-model", "context_window": 1},
		{"slug": "prefixed/model", "context_window": 2},
	}}
	out := mergeManifest(base, nil, &Config{}, emptySourceTables(), nil)
	if len(out.Models) != 2 || out.Models[0]["slug"] != "bare-model" || out.Models[1]["slug"] != "prefixed/model" {
		t.Fatalf("merge must not guess identity; admission belongs to Management-backed filtering: %+v", out.Models)
	}
}

func TestStripExactPrefixOnly(t *testing.T) {
	if got := stripExactPrefix("oc/model", "oc"); got != "model" {
		t.Fatalf("exact prefix strip: %q", got)
	}
	// 无末段猜测：前缀不匹配时原样返回
	if got := stripExactPrefix("some-other/model", "oc"); got != "some-other/model" {
		t.Fatalf("no last-segment guessing allowed: %q", got)
	}
}

func TestNativeMetadataPreservedWithExplicitSourceOverride(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "zhipuai", "glm-5.3", sourceHit{"context_window": 1000000, "max_tokens": 131072})
	base := &Manifest{Models: []map[string]any{
		{"slug": "zcode/glm-5.3", "context_window": 272000,
			"max_tokens": 120000, "supported_reasoning_levels": []any{map[string]any{"effort": "medium"}},
			"default_reasoning_level": "medium", "shell_type": "shell"},
		{"slug": "codex/gpt-6-astra", "context_window": 272000, "max_tokens": 128000},
		{"slug": "codex/gpt-5.5", "context_window": 272000, "max_tokens": 128000},
		{"slug": "axis/gpt-5.6-sol", "context_window": 1050000, "max_tokens": 128000},
	}}
	cfg := &Config{Channels: map[string]ChannelConfig{
		"zcode": chanCfg("models.dev/zhipuai"),
	}}
	fetched := []channelModels{{
		Channel: Channel{Name: "zc", Prefix: "zcode"},
		Models:  []ParsedModel{{ID: "glm-5.3"}},
	}}
	out := mergeManifest(base, fetched, cfg, tables, nil)
	got := map[string]map[string]any{}
	for _, m := range out.Models {
		got[asString(m["slug"])] = m
	}
	if g := got["zcode/glm-5.3"]; g["context_window"] != 1000000 || g["max_tokens"] != 131072 {
		t.Fatalf("glm-5.3 should be refilled from source: %+v", g)
	}
	if _, ok := got["zcode/glm-5.3"]["shell_type"]; !ok {
		t.Fatal("client-contract fields must survive source overlay")
	}
	if c := got["codex/gpt-5.5"]; c["context_window"] != 272000 {
		t.Fatalf("native declared limits must survive: %+v", c)
	}
	if c := got["codex/gpt-6-astra"]; c["context_window"] != 272000 || c["max_tokens"] != 128000 {
		t.Fatalf("gpt-6 declared limits must survive: %+v", c)
	}
	if a := got["axis/gpt-5.6-sol"]; a["context_window"] != 1050000 {
		t.Fatalf("channel-declared value must survive: %+v", a)
	}
}

func TestAdapterForUnknownType(t *testing.T) {
	if _, err := adapterFor("some-new-type"); err == nil {
		t.Fatal("unknown channel type must error, not silently default to OpenAI")
	}
	if _, err := adapterFor("openai-compatibility"); err != nil {
		t.Fatalf("known type: %v", err)
	}
}

// TestMergeStaticReplacesNativeBareInPlace：static 与 native 裸条目同 slug 时
// 只能输出一行且取 static 补全值（回归：曾输出两行，Pi 读到无 limits 的第一行）。
func TestMergeStaticReplacesNativeBareInPlace(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "minimax", "MiniMax-M3", sourceHit{"context_window": 1048576, "max_tokens": 512000})
	base := &Manifest{Models: []map[string]any{
		{"slug": "MiniMaxAI/MiniMax-M3"}, // native 裸条目，无 capability
	}}
	cfg := &Config{
		CustomChannels: map[string]ChannelConfig{
			"minimax-catalog": {
				SourcePriority: []string{"models.dev/minimax"},
				Models:         map[string]ModelConfig{"MiniMax-M3": {}},
			},
		},
		StaticModels: []map[string]any{
			{"slug": "MiniMaxAI/MiniMax-M3", "inherit": []any{"minimax-catalog/MiniMax-M3"}},
		},
	}
	out := mergeManifest(base, nil, cfg, tables, nil)
	count := 0
	for _, m := range out.Models {
		if asString(m["slug"]) == "MiniMaxAI/MiniMax-M3" {
			count++
			if m["context_window"] != 1048576 {
				t.Fatalf("static-completed limits missing: %+v", m)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one row for slug, got %d: %+v", count, out.Models)
	}
}
