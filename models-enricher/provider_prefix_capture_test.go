package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func prefixCapture(t *testing.T) (*Manifest, *SourceTables, map[string]map[string]sourceHit) {
	t.Helper()
	dir := os.Getenv("CATALOG_SOURCE_COMPLETION_FIXTURE")
	if dir == "" {
		t.Skip("set CATALOG_SOURCE_COMPLETION_FIXTURE for saved-source acceptance; skipping is not acceptance")
	}
	read := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	var base Manifest
	if err := decodeJSON(read("catalog.json"), &base); err != nil {
		t.Fatal(err)
	}
	api, flat := indexModelsDev(read("api.json")), indexModelsDevFlat(read("flat.json"))
	tables := emptySourceTables()
	tables.setModelsDev(api, flat)
	tables.mpK, tables.mpS = indexModelparams(read("params.json"), testLog())
	return &base, tables, flat
}

func TestProviderPrefixCapturedAdapters(t *testing.T) {
	_, tables, flat := prefixCapture(t)
	id := "nemotron-3-ultra-550b-a55b"
	if toInt(flat["nvidia"][id]["max_output_tokens"]) != 128000 {
		t.Fatal("captured flat Nvidia value changed")
	}
	for _, q := range []sourceQuery{{"models.dev/nvidia", id, false}, {"models.dev/nvidia", "nvidia/" + id, true}} {
		h, ok := tables.lookupQuery(q)
		if !ok || toInt(h["max_output_tokens"]) != 65536 || h["id"] != "nvidia/"+id {
			t.Fatalf("Nvidia API priority/identity: %#v %v", h, ok)
		}
	}
	h, ok := tables.lookupQuery(sourceQuery{"models.dev/minimax", "minimax-m3", false})
	if !ok || h["id"] != "MiniMax-M3" || toInt(h["max_output_tokens"]) != 512000 {
		t.Fatalf("bare API ID lost: %#v %v", h, ok)
	}
	providers := []string{}
	for provider, models := range tables.mpK {
		if _, n := lookupSourceID(models, "kimi-k3"); n != 0 {
			providers = append(providers, provider)
		}
	}
	sort.Strings(providers)
	if len(providers) < 2 {
		t.Fatal("fixed kimi-k3 ambiguity fixture missing")
	}
	for _, provider := range providers {
		token := "modelparams.dev/" + provider + "/api_key"
		queries := sourceQueries(ChannelConfig{SourcePriority: []string{token}}, "kimi-k3")
		if len(queries) != 1 || queries[0].token != token {
			t.Fatalf("bare model lost configured scope: %#v", queries)
		}
		_, n := lookupSourceID(tables.mpK[provider], "kimi-k3")
		if _, ok := tables.lookupQuery(queries[0]); ok != (n == 1) {
			t.Fatalf("another provider affected %s", token)
		}
	}
	if tables.channelFailed(ChannelConfig{SourcePriority: []string{"modelparams.dev/moonshotai/api_key"}}, "demo", []ParsedModel{{ID: "kimi-k3"}}) {
		t.Fatal("lookup miss became HTTP failure")
	}
	t.Logf("Nvidia API=65536 flat=128000; MiniMax API=512000; kimi-k3 api_key providers=%v", providers)
}

// 查询与完整来源层写入私有重放证据；不导出配置地址或凭证。
func prefixQueryEvidence(tables *SourceTables, q sourceQuery) map[string]any {
	hit, ok := tables.lookupQuery(q)
	row := map[string]any{"token": q.token, "id": q.id, "explicit": q.explicit, "hit": ok}
	if ok {
		for _, field := range []string{"id", "provider", "model", "authType"} {
			if value, exists := hit[field]; exists {
				row["source_"+field] = value
			}
		}
		row["fields"] = prefixFields(hit)
		row["metadata"] = hit
		return row
	}
	candidates := []string{}
	namespaces, provider := tables.sourceNamespace(q.token)
	dev := strings.HasPrefix(q.token, "models.dev/") && !q.explicit
	if dev {
		namespaces = tables.devAuto
	}
	for p, models := range namespaces {
		if (q.explicit && p != provider) || (!q.explicit && provider != "" && !strings.EqualFold(p, provider)) {
			continue
		}
		id := q.id
		if dev {
			id = strings.ToLower(p) + "/" + id
		}
		for key := range models {
			if strings.EqualFold(key, id) {
				candidates = append(candidates, p+":"+key)
			}
		}
	}
	sort.Strings(candidates)
	row["candidates"] = candidates
	row["reason"] = "miss"
	if len(candidates) > 0 {
		row["reason"] = "ambiguous"
	}
	return row
}

func prefixFields(row map[string]any) map[string]any {
	out := map[string]any{}
	for _, field := range []string{"context_window", "max_input_tokens", "max_tokens", "max_completion_tokens", "max_output_tokens", "input_modalities", "output_modalities", "supported_reasoning_levels", "default_reasoning_level"} {
		if value, exists := row[field]; exists {
			out[field] = value
		}
	}
	return out
}

func TestProviderPrefixCapturedReplay(t *testing.T) {
	base, tables, _ := prefixCapture(t)
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	packs := []channelModels{}
	prefixes := []string{}
	for prefix := range cfg.Channels {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		packs = append(packs, channelModels{Channel: chanOf(prefix, prefix), FetchSkipped: !cfg.Channels[prefix].fetchModelsEnabled()})
	}
	out := mergeManifest(base, packs, cfg, tables, nil)
	before := map[string]map[string]any{}
	for _, row := range base.Models {
		before[asString(row["slug"])] = row
	}
	if len(base.Models) != 189 || len(out.Models) != 193 || len(before) != 189 {
		t.Fatalf("fixed membership changed: base=%d out=%d before=%d", len(base.Models), len(out.Models), len(before))
	}
	rows := map[string]any{}
	// 已批准新增：grok-4.6与GLM裸static（2026-09-09，虚拟池成员，不进fixture基线）。
	approvedAdditions := map[string]bool{"grok-4.6": true, "glm-5.2": true, "glm-5.3": true, "glm-5.3-flash": true}
	for _, row := range out.Models {
		slug := asString(row["slug"])
		old, exists := before[slug]
		if !exists {
			if approvedAdditions[slug] {
				continue
			}
			t.Fatalf("member added: %s", slug)
		}
		if id, exists := old["id"]; exists {
			if next, present := row["id"]; present && !reflect.DeepEqual(next, id) {
				t.Fatalf("public ID spelling changed: %s", slug)
			}
		}
		prefix, name, _ := strings.Cut(slug, "/")
		queries := []map[string]any{}
		for _, q := range sourceQueries(cfg.Channels[prefix], name) {
			if q.token == "ollama_cloud" {
				queries = append(queries, map[string]any{"token": q.token, "id": q.id, "reason": "native input not in bulk fixture"})
				continue
			}
			queries = append(queries, prefixQueryEvidence(tables, q))
		}
		layer := map[string]any{}
		for i := len(queries) - 1; i >= 0; i-- {
			if hit, ok := queries[i]["metadata"].(sourceHit); ok {
				overlayMetadata(layer, hit)
			}
		}
		_, beforeID := old["id"]
		_, afterID := row["id"]
		rows[slug] = map[string]any{"queries": queries, "fields": prefixFields(row), "source_layer": layer, "id_present_before": beforeID, "id_present_after": afterID}
	}
	report, err := json.MarshalIndent(map[string]any{"members": len(rows), "channels": prefixes, "rows": rows}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("PROVIDER_PREFIX_REPLAY_OUTPUT"); path != "" {
		if err := os.WriteFile(path, report, 0600); err != nil {
			t.Fatal(err)
		}
		// 完整目录仅保存在本地受限证据文件，脱敏报告仍只列指定字段。
		manifest, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".manifest.json", manifest, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(fmt.Sprintf("replayed %d members across %d configured channels; source-only bulk replay, native HTTP layers not captured", len(rows), len(prefixes)))
}
