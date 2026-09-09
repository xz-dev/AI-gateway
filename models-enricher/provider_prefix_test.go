package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProviderPrefixConfigRejectsInvalid(t *testing.T) {
	for _, value := range []string{"null", "[]", `"provider"`, "{vendor: []}", "{vendor: null}", "{vendor: 12}", "{vendor: false}", `{vendor: ""}`, "{vendor: [target, null]}", `{vendor: "target/extra"}`, `{"two words": target}`, "{vendor: {target: value}}", "{vendor: [[target]]}", `{"": target}`, "{vendor/name: target}", `{vendor: "two words"}`} {
		t.Run(value, func(t *testing.T) {
			for _, scope := range []string{"provider_prefix_map: ", "channels:\n  example:\n    provider_prefix_map: ", "custom_channels:\n  pool:\n    source_priority: [models.dev/example]\n    provider_prefix_map: "} {
				_, err := loadConfig(writeConfig(t, "cpa_base_url: http://cpa\n"+scope+value+"\n"))
				if err == nil || !strings.Contains(err.Error(), "provider_prefix_map") {
					t.Fatalf("invalid mapping accepted or missing location: %q: %v", scope+value, err)
				}
			}
		})
	}
}

func TestProviderPrefixConfig(t *testing.T) {
	for _, input := range []string{"{Vendor: FIRST}", "{VENDOR: [first]}", "{vendor: [FIRST, first]}"} {
		cfg, err := loadConfig(writeConfig(t, "cpa_base_url: http://cpa\nprovider_prefix_map: "+input))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.providerPrefixes, providerPrefixes{{"vendor", []string{"first"}}}) {
			t.Fatalf("equivalent forms differ: %#v", cfg.providerPrefixes)
		}
	}
	for _, input := range []string{"", "provider_prefix_map: {}\n"} {
		cfg, err := loadConfig(writeConfig(t, "cpa_base_url: http://cpa\n"+input))
		if err != nil || len(cfg.providerPrefixes) != 0 {
			t.Fatalf("empty mapping: %v %v", cfg, err)
		}
	}
	cfg, err := loadConfig(writeConfig(t, `
cpa_base_url: http://cpa
provider_prefix_map:
  Vendor: first
  Other: original
  VENDOR: [second, SECOND]
channels:
  inherited: {provider_prefix_map: {}}
  replaced:
    provider_prefix_map: {vendor: third, Extra: fourth}
custom_channels:
  pool:
    source_priority: [models.dev/unused]
    provider_prefix_map: {alias: second}
`))
	if err != nil {
		t.Fatal(err)
	}
	global := providerPrefixes{{"vendor", []string{"second"}}, {"other", []string{"original"}}}
	if !reflect.DeepEqual(cfg.providerPrefixes, global) || !reflect.DeepEqual(cfg.Channels["inherited"].providerPrefixes, global) {
		t.Fatal("duplicate key or empty-channel inheritance changed global mapping")
	}
	want := providerPrefixes{{"vendor", []string{"third"}}, {"other", []string{"original"}}, {"extra", []string{"fourth"}}}
	if !reflect.DeepEqual(cfg.Channels["replaced"].providerPrefixes, want) {
		t.Fatalf("channel merge: %#v", cfg.Channels["replaced"].providerPrefixes)
	}
	if got := cfg.CustomChannels["pool"].providerPrefixes; len(got) != 3 || got[2].name != "alias" || got[0].targets[0] != "second" {
		t.Fatalf("custom pool and distinct alias: %#v", got)
	}
	for _, body := range []string{
		"channels: {example: {models: {m: {provider_prefix_map: {vendor: target}}}}}",
		"static_models: [{slug: alias, provider_prefix_map: {vendor: target}}]",
	} {
		if _, err := loadConfig(writeConfig(t, "cpa_base_url: http://cpa\n"+body)); err == nil {
			t.Fatal("mapping accepted outside global/channel scopes")
		}
	}
}

func TestProviderPrefixOrderedQueries(t *testing.T) {
	ch := ChannelConfig{
		SourcePriority:   []string{"models.dev/original", "modelparams.dev/original/subscription", "models.dev/duplicate", "ollama_cloud"},
		providerPrefixes: providerPrefixes{{"vendor", []string{"first", "second"}}, {"first", []string{"never"}}},
		Models: map[string]ModelConfig{"Vendor/M:Tag": {LookupIDs: map[string]string{
			"models.dev/first": "Opaque/ID:Tag", "models.dev/original": "must-not-leak", "modelparams.dev/first/api_key": "wrong-auth",
		}}},
	}
	want := []sourceQuery{
		{"models.dev/first", "Opaque/ID:Tag", true}, {"models.dev/second", "m:tag", false},
		{"modelparams.dev/first/subscription", "m:tag", false}, {"modelparams.dev/second/subscription", "m:tag", false},
		{"ollama_cloud", "Vendor/M:Tag", false},
	}
	if got := sourceQueries(ch, "Vendor/M:Tag"); !reflect.DeepEqual(got, want) {
		t.Fatalf("scope/order/explicit ID: %#v", got)
	}
	want = []sourceQuery{{"models.dev/original", "bare", false}, {"modelparams.dev/original/subscription", "bare", false}, {"models.dev/duplicate", "bare", false}, {"ollama_cloud", "Bare", false}}
	if got := sourceQueries(ch, "Bare"); !reflect.DeepEqual(got, want) {
		t.Fatalf("bare query lost its configured provider or source step: %#v", got)
	}
	ch.Models["Vendor/M:Tag"] = ModelConfig{SourcePriority: []string{}}
	if got := sourceQueries(ch, "Vendor/M:Tag"); len(got) != 0 {
		t.Fatalf("empty model chain enabled source: %#v", got)
	}
}

func TestProviderPrefixSourceComposition(t *testing.T) {
	tables := emptySourceTables()
	tables.setModelsDev(indexModelsDev([]byte(`{
		"first":{"models":{"Opaque/ID:Tag":{"id":"Opaque/ID:Tag","context_window":100,"max_tokens":0,"reasoning":false,"name":"","output_modalities":[],"nested":{"first":1},"missing":null}}},
		"second":{"models":{"m:tag":{"id":"m:tag","context_window":200,"limit":{"input":80},"output_modalities":["text"],"nested":{"first":2,"second":2},"missing":9}}}
	}`)), nil)
	setHit(tables.mpS, "first", "m:tag", sourceHit{"context_window": 300, "max_completion_tokens": 30, "nested": map[string]any{"third": 3}})
	setHit(tables.mpK, "first", "m:tag", sourceHit{"max_output_tokens": 999})
	ch := ChannelConfig{SourcePriority: []string{"models.dev/original", "modelparams.dev/original/subscription", "ollama_cloud"},
		providerPrefixes: providerPrefixes{{"vendor", []string{"first", "second"}}},
		Models:           map[string]ModelConfig{"Vendor/M:Tag": {LookupIDs: map[string]string{"models.dev/first": "Opaque/ID:Tag"}, Overrides: map[string]any{"manual": true}}},
	}
	base := &Manifest{Models: []map[string]any{{"slug": "demo/Vendor/M:Tag", "id": "demo/Vendor/M:Tag", "context_window": 400, "max_output_tokens": 4}}}
	cfg := &Config{Channels: map[string]ChannelConfig{"demo": ch}, CustomChannels: map[string]ChannelConfig{"pool": ch}, StaticModels: []map[string]any{{"slug": "static-id", "inherit": []string{"pool/Vendor/M:Tag"}}}}
	before := fmt.Sprint(tables.devAuto)
	out := mergeManifest(base, []channelModels{{Channel: chanOf("demo", "demo")}}, cfg, tables, map[string]map[string]sourceHit{"demo": {"Vendor/M:Tag": {"native_field": true, "context_window": 500}}})
	if len(out.Models) != 2 {
		t.Fatalf("membership/custom pool: %#v", out.Models)
	}
	row := out.Models[0]
	for field, want := range map[string]any{"context_window": 100, "max_input_tokens": 80, "max_tokens": 0, "max_completion_tokens": 30, "max_output_tokens": 4, "reasoning": false, "display_name": "", "manual": true, "missing": 9, "native_field": true} {
		if fmt.Sprint(row[field]) != fmt.Sprint(want) {
			t.Errorf("%s: got %#v want %#v", field, row[field], want)
		}
	}
	if a, ok := row["output_modalities"].([]any); !ok || len(a) != 0 {
		t.Fatal("empty array lost")
	}
	if fmt.Sprint(row["nested"]) != "map[first:1 second:2 third:3]" {
		t.Fatalf("recursive composition: %v", row["nested"])
	}
	if out.Models[1]["slug"] != "static-id" || out.Models[1]["id"] != "static-id" || out.Models[1]["max_output_tokens"] != nil {
		t.Fatal("static identity or token independence changed")
	}
	if before != fmt.Sprint(tables.devAuto) {
		t.Fatal("shared records mutated")
	}
	ch.Models["Vendor/M:Tag"] = ModelConfig{Overrides: map[string]any{"context_window": 777}, LookupIDs: map[string]string{"models.dev/first": "Opaque/ID:Tag"}}
	cfg.Channels["demo"] = ch
	if row = mergeManifest(base, []channelModels{{Channel: chanOf("demo", "demo")}}, cfg, tables, nil).Models[0]; toInt(row["context_window"]) != 777 {
		t.Fatal("manual override did not win")
	}
}

func TestProviderPrefixModelsDevIdentity(t *testing.T) {
	tables := emptySourceTables()
	tables.setModelsDev(indexModelsDev([]byte(`{"nvidia":{"models":{
		"nvidia/M":{"id":"nvidia/M","limit":{"output":65}},
		"nvidia/other/M":{"id":"nvidia/other/M","limit":{"output":66}},
		"Case":{"name":"same"},"CASE":{"name":"same"}
	}}}`)), indexModelsDevFlat([]byte(`{"nvidia/m":{"id":"nvidia/m","limit":{"output":128,"input":80}},"nvidia/nvidia/M":{"limit":{"output":99}}}`)))
	for _, tc := range []struct {
		id   string
		want int
		hit  bool
	}{{"m", 65, true}, {"other/m", 66, true}, {"nvidia/m", 99, true}, {"case", 0, false}} {
		h, ok := tables.lookupQuery(sourceQuery{token: "models.dev/nvidia", id: tc.id})
		if ok != tc.hit || ok && toInt(h["max_output_tokens"]) != tc.want {
			t.Fatalf("%s: %#v %v", tc.id, h, ok)
		}
	}
	if h, ok := tables.lookupQuery(sourceQuery{token: "models.dev/nvidia", id: "m"}); !ok || toInt(h["max_input_tokens"]) != 80 || toInt(h["max_output_tokens"]) != 65 {
		t.Fatal("API/flat priority or uniqueness failed")
	}
	setHit(tables.devAuto, "other", "other/m", sourceHit{"max_output_tokens": 200})
	if h, ok := tables.lookupQuery(sourceQuery{token: "models.dev/nvidia", id: "m"}); !ok || toInt(h["max_output_tokens"]) != 65 {
		t.Fatal("other provider changed the selected scope")
	}
}

func TestProviderPrefixHTTPBoundaries(t *testing.T) {
	var apiCalls, flatCalls, otherCalls atomic.Int64
	sources := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			apiCalls.Add(1)
			w.Write([]byte(`{"first":{"models":{"m":{"limit":{"context":100}},"blocked":{"limit":{"context":999}},"outside":{"limit":{"context":999}}}},"second":{"models":{"m":{"limit":{"input":80,"context":200}}}}}`))
		case "/flat.json":
			flatCalls.Add(1)
			w.Write([]byte(`{}`))
		default:
			otherCalls.Add(1)
			t.Error("mapping activated an unconfigured source")
			w.WriteHeader(500)
		}
	}))
	defer sources.Close()
	oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = sources.URL+"/api.json", sources.URL+"/flat.json", sources.URL+"/params.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })
	cfg, err := loadConfig(writeConfig(t, `cpa_base_url: http://cpa
provider_prefix_map: {VENDOR: [first, second]}
channels:
  demo: {source_priority: [models.dev/unused]}
`))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCPA{
		native:       []byte(`{"models":[{"slug":"demo/Vendor/M","id":"demo/Vendor/M"},{"slug":"demo/Vendor/Blocked"},{"slug":"oauth/Vendor/M","context_window":13}]}`),
		oauthModels:  []string{"Vendor/M"},
		channelsBody: []byte(`{"openai-compatibility":[{"name":"demo","prefix":"demo","base-url":"https://example.invalid/v1","api-key-entries":[{"auth-index":"test"}],"models":[{"name":"Vendor/M"},{"name":"Vendor/Outside"}]}]}`),
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	got := identityCatalog(t, newTestHandler(t, cfg, cpa))
	if len(got) != 2 || got["demo/Vendor/Blocked"] != nil || got["demo/Vendor/Outside"] != nil {
		t.Fatalf("source changed authority: %v", got)
	}
	if got["demo/Vendor/M"]["id"] != "demo/Vendor/M" || toInt(got["demo/Vendor/M"]["context_window"]) != 100 || toInt(got["demo/Vendor/M"]["max_input_tokens"]) != 80 {
		t.Fatalf("mapped public catalog: %v", got)
	}
	if toInt(got["oauth/Vendor/M"]["context_window"]) != 13 {
		t.Fatal("OAuth entered source synthesis")
	}
	if apiCalls.Load() != 1 || flatCalls.Load() != 1 || otherCalls.Load() != 0 {
		t.Fatalf("provider expansion changed HTTP budget: %d/%d/%d", apiCalls.Load(), flatCalls.Load(), otherCalls.Load())
	}
}

func TestProviderPrefixDefaultLookup(t *testing.T) {
	tables := emptySourceTables()
	tables.mpK, tables.mpS = indexModelparams([]byte(`{"models":[
		{"provider":"NVIDIA","model":"Model","authType":"api_key","context_window":42},
		{"provider":"vendor","model":"Team/Model-V2-20260901:Preview","authType":"api_key","context_window":43},
		{"provider":"first","model":"kimi-k3","authType":"api_key","context_window":99},
		{"provider":"second","model":"KIMI-K3","authType":"api_key","context_window":100},
		{"model":"providerless","authType":"api_key","context_window":44},
		{"provider":"only-sub","model":"providerless","authType":"subscription","context_window":88}
	]}`), slog.Default())
	for _, tc := range []struct {
		name string
		want int
	}{
		{"NVIDIA/Model", 42}, {"Model", 7}, {"Vendor/Team/Model-V2-20260901:Preview", 43},
		{"kimi-k3", 7}, {"providerless", 7}, {"other/providerless", 7}, {"/Model", 7}, {"NVIDIA/", 7},
		{"Vendor/Team/Model-V2-20260901", 7}, {"Vendor/Model-V2-20260901:Preview", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Channels: map[string]ChannelConfig{"demo": {SourcePriority: []string{"modelparams.dev/unused/api_key"}}}}
			slug := "demo/" + tc.name
			base := &Manifest{Models: []map[string]any{{"slug": slug, "context_window": 7}}}
			got := mergeManifest(base, []channelModels{{Channel: chanOf("demo", "demo")}}, cfg, tables, nil)
			if len(got.Models) != 1 || got.Models[0]["slug"] != slug || fmt.Sprint(got.Models[0]["context_window"]) != fmt.Sprint(tc.want) {
				t.Fatalf("lookup changed identity or chose wrong source: %#v", got.Models)
			}
		})
	}
}
