package main

import (
	"reflect"
	"testing"
)

func TestMarkImageOutputFillsOnlyMissingGPTImage(t *testing.T) {
	base := &Manifest{Models: []map[string]any{
		{"slug": "codex/gpt-image-2"},
		{"slug": "xl/gpt-image-2.5-flare", "output_modalities": []any{"image", "text"}},
		{"slug": "codex/gpt-6-luna"},
	}, preserveNative: map[string]bool{"codex/gpt-image-2": true, "codex/gpt-6-luna": true}}
	got := map[string]any{}
	for _, m := range mergeManifest(base, nil, &Config{}, emptySourceTables(), nil).Models {
		got[asString(m["slug"])] = m["output_modalities"]
	}
	if !reflect.DeepEqual(got["codex/gpt-image-2"], []any{"image"}) {
		t.Fatalf("missing gpt-image output not filled: %#v", got["codex/gpt-image-2"])
	}
	if !reflect.DeepEqual(got["xl/gpt-image-2.5-flare"], []any{"image", "text"}) {
		t.Fatalf("declared output overwritten: %#v", got["xl/gpt-image-2.5-flare"])
	}
	if got["codex/gpt-6-luna"] != nil {
		t.Fatalf("non-image model tagged: %#v", got["codex/gpt-6-luna"])
	}
}
