package main

import (
	"reflect"
	"testing"
)

// 端到端：bare 接管 + 全局链命中 models.dev/zai 时，merge 输出动态元数据。
func TestBareTakeoverMergeEnrichesFromGlobalChain(t *testing.T) {
	cfg := &Config{
		BareModelsTakeover:    true,
		GlobalSourcePriority:  []string{"models.dev/zai"},
		Channels:              map[string]ChannelConfig{},
		CustomChannels:        map[string]ChannelConfig{},
	}
	// 裸名经 bare 接管 admitted（绕过 identity.filter 直接构造等价 base）。
	base := &Manifest{Models: []map[string]any{{"slug": "glm-5.2"}}, admitted: map[string]bool{"glm-5.2": true}, preserveNative: map[string]bool{}}

	tables := emptySourceTables()
	tables.devAuto = map[string]map[string]sourceHit{
		"zai": {"zai/glm-5.2": {"context_window": 1000000, "display_name": "GLM-5.2", "max_output_tokens": 131072}},
	}
	out := mergeManifest(base, nil, cfg, tables, nil)
	var got map[string]any
	for _, m := range out.Models {
		if asString(m["slug"]) == "glm-5.2" {
			got = m
		}
	}
	if got == nil {
		t.Fatal("bare model missing from merged catalog")
	}
	if toInt(got["context_window"]) != 1000000 || got["display_name"] != "GLM-5.2" {
		t.Fatalf("bare model not enriched from global chain: %v", got)
	}
}

// 无源命中时保留 CPA 原始字段（fail-open）。
func TestBareTakeoverFailOpenKeepsCPAFields(t *testing.T) {
	cfg := &Config{
		BareModelsTakeover:    true,
		GlobalSourcePriority:  []string{"models.dev/zai"},
		Channels:              map[string]ChannelConfig{},
		CustomChannels:        map[string]ChannelConfig{},
	}
	base := &Manifest{Models: []map[string]any{{"slug": "orphan-model", "context_window": 272000}}, admitted: map[string]bool{"orphan-model": true}, preserveNative: map[string]bool{}}
	out := mergeManifest(base, nil, cfg, emptySourceTables(), nil)
	var got map[string]any
	for _, m := range out.Models {
		if asString(m["slug"]) == "orphan-model" {
			got = m
		}
	}
	if got == nil || toInt(got["context_window"]) != 272000 {
		t.Fatalf("fail-open must keep CPA fields: %v", got)
	}
	_ = reflect.DeepEqual
}
