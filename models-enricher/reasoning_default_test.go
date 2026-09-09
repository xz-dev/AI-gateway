package main

import (
	"reflect"
	"testing"
)

func TestReasoningDefaultOnlyOmitProvenConflict(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		omit      bool
	}{
		{"conflict", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"}],"unknown":null}`, true},
		{"empty support list", `{"default_reasoning_level":"medium","supported_reasoning_levels":[]}`, true},
		{"supported default", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"},{"effort":"medium"}]}`, false},
		{"missing list", `{"default_reasoning_level":"medium"}`, false},
		{"null list", `{"default_reasoning_level":"medium","supported_reasoning_levels":null}`, false},
		{"unknown list shape", `{"default_reasoning_level":"medium","supported_reasoning_levels":false}`, false},
		{"unknown list item", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"},{}]}`, false},
		{"empty effort", `{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":""}]}`, false},
		{"missing default", `{"supported_reasoning_levels":[{"effort":"high"}]}`, false},
		{"null default", `{"default_reasoning_level":null,"supported_reasoning_levels":[{"effort":"high"}]}`, false},
		{"false default", `{"default_reasoning_level":false,"supported_reasoning_levels":[{"effort":"high"}]}`, false},
		{"zero default", `{"default_reasoning_level":0,"supported_reasoning_levels":[{"effort":"high"}]}`, false},
		{"empty default", `{"default_reasoning_level":"","supported_reasoning_levels":[{"effort":"high"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var row map[string]any
			if err := decodeJSON([]byte(tc.raw), &row); err != nil {
				t.Fatal(err)
			}
			row["slug"] = "demo/m"
			want := cloneJSONValue(row).(map[string]any)
			if tc.omit {
				delete(want, "default_reasoning_level")
			}
			omitConflictingReasoningDefault(row, nil)
			if !reflect.DeepEqual(row, want) {
				t.Fatalf("应只移除已证实冲突的默认字段: got %v, want %v", row, want)
			}
		})
	}
}

func TestReasoningDefaultAfterReferencesAndManualOverrides(t *testing.T) {
	levels := func(effort string) []any { return []any{map[string]any{"effort": effort}} }
	base := &Manifest{Models: []map[string]any{
		{"slug": "demo/reference", "supported_reasoning_levels": levels("medium")},
		{"slug": "demo/target", "default_reasoning_level": "medium", "supported_reasoning_levels": levels("high")},
		{"slug": "demo/manual-default", "default_reasoning_level": "medium", "supported_reasoning_levels": levels("high")},
		{"slug": "demo/manual-list", "default_reasoning_level": "medium", "supported_reasoning_levels": levels("high")},
	}}
	cfg := &Config{Channels: map[string]ChannelConfig{"demo": {Models: map[string]ModelConfig{
		"target":         {MetadataFrom: "demo/reference"},
		"manual-default": {Overrides: map[string]any{"default_reasoning_level": "medium"}},
		"manual-list":    {Overrides: map[string]any{"supported_reasoning_levels": levels("medium")}},
	}}}}
	got := mergeManifest(base, []channelModels{{Channel: chanOf("demo", "demo")}}, cfg, emptySourceTables(), nil)
	for _, row := range got.Models {
		if row["slug"] != "demo/reference" && row["default_reasoning_level"] != "medium" {
			t.Errorf("应在引用和人工覆盖生效后判断，不得提前丢掉可用默认值: %v", row)
		}
	}
}

func TestReasoningDefaultCustomAndStaticInheritance(t *testing.T) {
	tables := emptySourceTables()
	tables.setModelsDev(indexModelsDev([]byte(`{"vendor":{"models":{"m":{"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"high"}]}}}}`)), nil)
	base := &Manifest{Models: []map[string]any{{"slug": "native/parent", "default_reasoning_level": "medium", "supported_reasoning_levels": []any{map[string]any{"effort": "high"}}}}}
	cfg := &Config{
		CustomChannels: map[string]ChannelConfig{"pool": {SourcePriority: []string{"models.dev/vendor"}, Models: map[string]ModelConfig{"m": {}}}},
		StaticModels: []map[string]any{
			{"slug": "from-pool", "inherit": []string{"pool/m"}},
			{"slug": "from-native", "inherit": []string{"native/parent"}},
			{"slug": "manual", "inherit": []string{"native/parent"}, "overrides": map[string]any{"default_reasoning_level": "medium"}},
			{"slug": "manual-list", "inherit": []string{"native/parent"}, "overrides": map[string]any{"supported_reasoning_levels": []any{map[string]any{"effort": "medium"}}}},
		},
	}
	got := mergeManifest(base, nil, cfg, tables, nil)
	if len(got.Models) != 5 {
		t.Fatalf("不得增加隐藏池成员或改变静态成员: %v", got.Models)
	}
	for _, row := range got.Models {
		slug := asString(row["slug"])
		value, present := row["default_reasoning_level"]
		if slug == "from-pool" || slug == "from-native" {
			if present {
				t.Errorf("继承结果仍包含冲突默认值: %v", row)
			}
		} else if value != "medium" {
			t.Errorf("不得改写原生对象或人工覆盖: %v", row)
		}
	}
}

func TestReasoningDefaultPreservesBaselineBoundaries(t *testing.T) {
	for _, name := range []string{"failed", "skipped", "oauth"} {
		t.Run(name, func(t *testing.T) {
			base := &Manifest{Models: []map[string]any{{"slug": "demo/m", "default_reasoning_level": "medium", "supported_reasoning_levels": []any{map[string]any{"effort": "high"}}, "unknown": nil}}}
			cfg := &Config{Channels: map[string]ChannelConfig{"demo": {}}}
			packs := []channelModels{{Channel: chanOf("demo", "demo"), Failed: name == "failed", FetchSkipped: name == "skipped"}}
			if name == "oauth" {
				base.preserveNative = map[string]bool{"demo/m": true}
			}
			got := mergeManifest(base, packs, cfg, emptySourceTables(), nil)
			if !reflect.DeepEqual(got.Models, base.Models) {
				t.Fatalf("默认值清理穿过了%s原样边界: %v", name, got.Models)
			}
		})
	}
}
