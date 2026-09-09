package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 表格列与清单字段的一致性：诊断页列顺序是表格消费契约。
// 清单输出必须让每一列既有值来源：未知显示未知，不允许捏造或隐藏。
func TestCatalogTableColumnsMatchManifest(t *testing.T) {
	if _, err := os.Stat(filepath.Join("testdata", "provider-chain-sources.json")); err != nil {
		t.Skip("source snapshot unavailable")
	}
	cfg, tables := providerFixture(t)
	rows := []map[string]any{
		{"slug": "commandcode/claude-sonnet-5"},
		{"slug": "nim/minimaxai/minimax-m3"},
		{"slug": "unknown/model"},
	}
	out := mergeProviderSamples(cfg, tables, rows)
	columns := []string{"slug", "display_name", "input_modalities", "output_modalities",
		"context_window", "max_input_tokens", "max_output_tokens",
		"supported_reasoning_levels", "default_reasoning_level"}
	for slug, m := range out {
		for _, col := range columns {
			if col == "slug" {
				continue
			}
			v, ok := m[col]
			if ok && v == nil {
				t.Fatalf("%s column %s: explicit null leaked as present", slug, col)
			}
		}
	}
	// 命中链的模型至少展示 output_modalities；无链模型字段保持未知而非假值。
	if out["commandcode/claude-sonnet-5"]["output_modalities"] == nil {
		t.Fatal("enriched model lost output_modalities")
	}
	if out["unknown/model"]["max_output_tokens"] != nil {
		t.Fatal("unknown model fabricated metadata")
	}
	_ = os.Stat
}
