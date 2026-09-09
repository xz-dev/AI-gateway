package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestProviderPrefixBareChainComposition(t *testing.T) {
	tables := emptySourceTables()
	tables.setModelsDev(indexModelsDev([]byte(`{
		"first":{"models":{"model-x":{"id":"model-x","context_window":100,"max_tokens":0,"reasoning":false,"name":"","output_modalities":[],"nested":{"first":1},"missing":null}}},
		"second":{"models":{"model-x":{"id":"model-x","context_window":200,"limit":{"output":80},"output_modalities":["text"],"nested":{"first":2,"second":2},"missing":9}}},
		"third":{"models":{"model-x":{"context_window":999,"max_completion_tokens":999,"third_only":true}}}
	}`)), nil)
	ch := ChannelConfig{
		SourcePriority:   []string{"models.dev/FIRST", "models.dev/second", "models.dev/first", "ollama_cloud"},
		providerPrefixes: providerPrefixes{{"first", []string{"third"}}},
		Models:           map[string]ModelConfig{"Model-X": {}},
	}
	wantQueries := []sourceQuery{{"models.dev/first", "model-x", false}, {"models.dev/second", "model-x", false}, {"ollama_cloud", "Model-X", false}}
	if got := sourceQueries(ch, "Model-X"); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("bare scope/order/deduplication: %#v", got)
	}
	base := &Manifest{Models: []map[string]any{{"slug": "demo/Model-X", "id": "demo/Model-X", "context_window": 400}}}
	cfg := &Config{Channels: map[string]ChannelConfig{"demo": ch}, CustomChannels: map[string]ChannelConfig{"pool": ch}, StaticModels: []map[string]any{{"slug": "static-id", "inherit": []string{"pool/Model-X"}}}}
	before := fmt.Sprint(tables.devAuto)
	out := mergeManifest(base, []channelModels{{Channel: chanOf("demo", "demo")}}, cfg, tables, map[string]map[string]sourceHit{"demo": {"Model-X": {"native_field": true, "context_window": 500}}})
	if len(out.Models) != 2 || out.Models[0]["slug"] != "demo/Model-X" || out.Models[0]["id"] != "demo/Model-X" || out.Models[1]["slug"] != "static-id" || out.Models[1]["id"] != "static-id" {
		t.Fatalf("membership or identity changed: %#v", out.Models)
	}
	for _, row := range out.Models {
		for field, want := range map[string]any{"context_window": 100, "max_output_tokens": 80, "max_tokens": 0, "reasoning": false, "display_name": "", "missing": 9} {
			if fmt.Sprint(row[field]) != fmt.Sprint(want) {
				t.Errorf("%s %s: got %#v want %#v", row["slug"], field, row[field], want)
			}
		}
		if a, ok := row["output_modalities"].([]any); !ok || len(a) != 0 {
			t.Fatal("empty array lost")
		}
		if fmt.Sprint(row["nested"]) != "map[first:1 second:2]" {
			t.Fatal("recursive source composition changed")
		}
		for _, field := range []string{"third_only", "max_input_tokens", "max_completion_tokens"} {
			if _, exists := row[field]; exists {
				t.Fatalf("unselected source or inferred token field: %s", field)
			}
		}
	}
	if out.Models[0]["native_field"] != true || out.Models[1]["native_field"] != nil || before != fmt.Sprint(tables.devAuto) || base.Models[0]["context_window"] != 400 {
		t.Fatal("native layer or shared input mutated")
	}
	ch.Models["Model-X"] = ModelConfig{SourcePriority: []string{"models.dev/second"}, Overrides: map[string]any{"context_window": 777}}
	cfg.Channels["demo"] = ch
	row := mergeManifest(base, []channelModels{{Channel: chanOf("demo", "demo")}}, cfg, tables, nil).Models[0]
	if toInt(row["context_window"]) != 777 || row["max_tokens"] != nil || toInt(row["max_output_tokens"]) != 80 {
		t.Fatal("model whole-chain replacement or manual priority changed")
	}
}

func TestProviderPrefixBareModelparamsScope(t *testing.T) {
	tables := emptySourceTables()
	tables.mpK, tables.mpS = indexModelparams([]byte(`{"models":[
		{"provider":"first","model":"Model-X","authType":"subscription","context_window":42},
		{"provider":"first","model":"model-x","authType":"api_key","context_window":43},
		{"provider":"second","model":"model-x","authType":"subscription","context_window":99},
		{"model":"model-x","authType":"subscription","context_window":999},
		{"model":"providerless","authType":"subscription","context_window":888},
		{"provider":"first","model":"Case","authType":"subscription","context_window":50},
		{"provider":"first","model":"CASE","authType":"subscription","context_window":51},
		{"provider":"second","model":"case","authType":"subscription","context_window":52},
		{"provider":"first","model":"EXACT","authType":"subscription","context_window":60},
		{"provider":"first","model":"exact","authType":"subscription","context_window":61},
		{"provider":"first","model":"Team/Explicit:Tag","authType":"subscription","context_window":70}
	]}`), testLog())
	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"Model-X", "modelparams.dev/first/subscription", 42},
		{"MODEL-X", "modelparams.dev/first/api_key", 43},
		{"Model-X", "modelparams.dev/second/subscription", 99},
		{"Model-X", "modelparams.dev/absent/subscription", 7},
		{"Model-X", "modelparams.dev/second/api_key", 7},
		{"providerless", "modelparams.dev/first/subscription", 7},
		{"case", "modelparams.dev/first/subscription", 7},
		{"exact", "modelparams.dev/first/subscription", 61},
		{"alias", "modelparams.dev/first/subscription", 70},
		{"alias", "modelparams.dev/first/api_key", 7},
	} {
		t.Run(tc.name+"/"+tc.token, func(t *testing.T) {
			ch := ChannelConfig{SourcePriority: []string{tc.token}, Models: map[string]ModelConfig{"alias": {LookupIDs: map[string]string{"modelparams.dev/first/subscription": "Team/Explicit:Tag"}}}}
			cfg := &Config{Channels: map[string]ChannelConfig{"demo": ch}}
			slug := "demo/" + tc.name
			row := mergeProviderSamples(cfg, tables, []map[string]any{{"slug": slug, "context_window": 7}})[slug]
			if toInt(row["context_window"]) != tc.want {
				t.Fatalf("selected scope: %#v, want %d", row, tc.want)
			}
			if tables.channelFailed(ch, "demo", []ParsedModel{{ID: tc.name}}) {
				t.Fatal("ordinary lookup miss became HTTP failure")
			}
		})
	}
}

func TestProviderPrefixBareAnthropicHTTP(t *testing.T) {
	var apiCalls, flatCalls, otherCalls atomic.Int64
	sources := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			apiCalls.Add(1)
			w.Write([]byte(`{"anthropic":{"models":{"claude-opus-4-7":{"limit":{"output":80}},"claude-sonnet-5":{"limit":{"output":90}},"outside":{"limit":{"output":99}}}},"other":{"models":{"claude-opus-4-7":{"limit":{"output":999}},"CLAUDE-SONNET-5":{"limit":{"output":999}},"miss":{"limit":{"output":999}}}}}`))
		case "/flat.json":
			flatCalls.Add(1)
			w.Write([]byte(`{}`))
		default:
			otherCalls.Add(1)
			w.WriteHeader(500)
		}
	}))
	defer sources.Close()
	oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = sources.URL+"/api.json", sources.URL+"/flat.json", sources.URL+"/params.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })
	cfg, err := loadConfig(writeConfig(t, "cpa_base_url: http://cpa\nprovider_prefix_map: {anthropic: other}\nchannels:\n  demo: {fetch_models: false, source_priority: [models.dev/anthropic]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCPA{
		native:       []byte(`{"models":[{"slug":"demo/claude-opus-4-7","id":"demo/claude-opus-4-7"},{"slug":"demo/claude-sonnet-5"},{"slug":"demo/miss","context_window":7},{"slug":"demo/blocked"},{"slug":"oauth/claude-sonnet-5","max_output_tokens":13}]}`),
		oauthModels:  []string{"claude-sonnet-5"},
		channelsBody: []byte(`{"openai-compatibility":[{"name":"demo","prefix":"demo","base-url":"https://example.invalid/v1","api-key-entries":[{"auth-index":"test"}],"models":[{"name":"claude-opus-4-7"},{"name":"claude-sonnet-5"},{"name":"miss"},{"name":"outside"}]}]}`),
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	got := identityCatalog(t, newTestHandler(t, cfg, cpa))
	if len(got) != 4 || got["demo/blocked"] != nil || got["demo/outside"] != nil {
		t.Fatalf("source changed authority: %v", got)
	}
	for slug, want := range map[string]int{"demo/claude-opus-4-7": 80, "demo/claude-sonnet-5": 90, "oauth/claude-sonnet-5": 13} {
		if toInt(got[slug]["max_output_tokens"]) != want {
			t.Fatalf("%s: %#v", slug, got[slug])
		}
	}
	if got["demo/claude-opus-4-7"]["id"] != "demo/claude-opus-4-7" || got["demo/miss"]["max_output_tokens"] != nil || toInt(got["demo/miss"]["context_window"]) != 7 {
		t.Fatal("identity or selected-provider miss changed")
	}
	if apiCalls.Load() != 1 || flatCalls.Load() != 1 || otherCalls.Load() != 0 {
		t.Fatalf("HTTP budget: %d/%d/%d", apiCalls.Load(), flatCalls.Load(), otherCalls.Load())
	}
}
