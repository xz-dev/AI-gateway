package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func assertMetadataJSON(t *testing.T, got any, expected string) {
	t.Helper()
	var want any
	if err := decodeJSON([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	actualJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	if string(actualJSON) != string(wantJSON) {
		t.Fatalf("got  %s\nwant %s", actualJSON, wantJSON)
	}
}

func TestAdapterRecordsReachManifest(t *testing.T) {
	cases := []struct {
		name, body, want string
		parse            func([]byte) ([]ParsedModel, error)
	}{
		{"openai", `{"secret_envelope":"excluded","data":[{"id":"vendor/m:tag","name":"Declared","type":"image","context_length":0,"max_input_tokens":0,"max_output_tokens":0,"max_tokens":7,"architecture":{"input_modalities":["text"],"output_modalities":["image"]},"vendor":{"sequence":9007199254740993,"values":[null,false]},"supported_parameters":["reasoning"]}]}`,
			`{"slug":"c/vendor/m:tag","id":"c/vendor/m:tag","name":"Declared","display_name":"Declared","type":"image","context_length":0,"context_window":0,"max_input_tokens":0,"max_output_tokens":0,"max_tokens":7,"architecture":{"input_modalities":["text"],"output_modalities":["image"]},"input_modalities":["text"],"output_modalities":["image"],"vendor":{"sequence":9007199254740993,"values":[null,false]},"supported_parameters":["reasoning"]}`, parseOpenAI},
		{"claude", `{"secret_envelope":"excluded","data":[{"id":"claude","display_name":"","input_modalities":[],"output_modalities":["text"],"future":{"values":[false,0]}}]}`,
			`{"slug":"c/claude","id":"c/claude","display_name":"","input_modalities":[],"output_modalities":["text"],"future":{"values":[false,0]}}`, parseClaude},
		{"gemini", `{"secret_envelope":"excluded","models":[{"name":"models/g","displayName":"Gemini","inputTokenLimit":0,"outputTokenLimit":42,"supportedGenerationMethods":["generateContent","embedContent"],"output_modalities":["text"]}]}`,
			`{"slug":"c/g","name":"models/g","displayName":"Gemini","display_name":"Gemini","inputTokenLimit":0,"max_input_tokens":0,"outputTokenLimit":42,"max_output_tokens":42,"supportedGenerationMethods":["generateContent","embedContent"],"output_modalities":["text"]}`, parseGemini},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			models, err := tc.parse([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			manifest := mergeManifest(nil, []channelModels{{Channel: Channel{Prefix: "c"}, Models: models}}, &Config{}, emptySourceTables(), nil)
			if len(manifest.Models) != 1 {
				t.Fatalf("models: %+v", manifest.Models)
			}
			assertMetadataJSON(t, manifest.Models[0], tc.want)
		})
	}
}

func TestSourceRecordsReachManifest(t *testing.T) {
	cases := []struct {
		name, source, body, want string
		load                     func(*SourceTables, []byte)
	}{
		{"models.dev", "models.dev/p", `{"p":{"secret_provider_config":"excluded","models":{"dbid":{"id":"dbid","name":"Sourced","limit":{"context":0,"input":0,"output":0},"modalities":{"input":[],"output":["audio"]},"reasoning":true,"vendor_source":{"public":true},"sequence":9007199254740993}}}}`,
			`{"slug":"c/public","id":"c/public","name":"Sourced","display_name":"Sourced","limit":{"context":0,"input":0,"output":0},"context_window":0,"max_input_tokens":0,"max_output_tokens":0,"modalities":{"input":[],"output":["audio"]},"input_modalities":[],"output_modalities":["audio"],"reasoning":true,"vendor_source":{"public":true},"sequence":9007199254740993}`,
			func(tables *SourceTables, body []byte) { tables.dev = indexModelsDev(body) }},
		{"modelparams default is not maximum", "modelparams.dev/p/api_key", `{"secret_envelope":"excluded","models":[{"model":"dbid","provider":"p","params":[{"path":"max_tokens","default":64},{"path":"reasoning_effort","values":["low","high"],"default":"low"}],"vendor":{"sequence":9007199254740993}}]}`,
			`{"slug":"c/public","model":"dbid","provider":"p","params":[{"path":"max_tokens","default":64},{"path":"reasoning_effort","values":["low","high"],"default":"low"}],"vendor":{"sequence":9007199254740993},"supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],"default_reasoning_level":"low"}`,
			func(tables *SourceTables, body []byte) { tables.mpK, tables.mpS = indexModelparams(body, testLog()) }},
		{"modelparams explicit zero maximum", "modelparams.dev/p/api_key", `{"models":[{"model":"dbid","provider":"p","params":[{"path":"max_tokens","default":64,"range":{"max":0}}]}]}`,
			`{"slug":"c/public","model":"dbid","provider":"p","params":[{"path":"max_tokens","default":64,"range":{"max":0}}],"max_tokens":0}`,
			func(tables *SourceTables, body []byte) { tables.mpK, tables.mpS = indexModelparams(body, testLog()) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tables := emptySourceTables()
			tc.load(tables, []byte(tc.body))
			cfg := &Config{Channels: map[string]ChannelConfig{"c": {
				SourcePriority: []string{tc.source},
				Models:         map[string]ModelConfig{"public": {LookupIDs: map[string]string{tc.source: "dbid"}}},
			}}}
			fetched := []channelModels{{Channel: Channel{Prefix: "c"}, Models: []ParsedModel{{ID: "public"}}}}
			manifest := mergeManifest(nil, fetched, cfg, tables, nil)
			if len(manifest.Models) != 1 {
				t.Fatalf("models: %+v", manifest.Models)
			}
			assertMetadataJSON(t, manifest.Models[0], tc.want)
		})
	}
}

func TestStructuredOutputFields(t *testing.T) {
	for _, source := range []string{"top_provider", "gemini", "models.dev"} {
		for _, destination := range []string{"missing", "null", "zero"} {
			t.Run(source+"/"+destination, func(t *testing.T) {
				row := map[string]any{"id": "m"}
				want := 32768
				if destination == "null" {
					row["max_output_tokens"] = nil
				} else if destination == "zero" {
					row["max_output_tokens"] = 0
					want = 0
				}
				var got map[string]any
				if source == "models.dev" {
					row["limit"] = map[string]any{"output": 32768}
					got = hitFromModelsDev(row)
				} else {
					key, parse := "data", parseOpenAI
					if source == "gemini" {
						key, parse = "models", parseGemini
						row["outputTokenLimit"] = 32768
					} else {
						row["top_provider"] = map[string]any{"max_completion_tokens": 32768}
					}
					body, _ := json.Marshal(map[string]any{key: []any{row}})
					models, err := parse(body)
					if err != nil || len(models) != 1 {
						t.Fatalf("parse: %v, models: %+v", err, models)
					}
					got = models[0].Metadata
				}
				assertMetadataJSON(t, got["max_output_tokens"], fmt.Sprint(want))
				for _, sibling := range []string{"max_tokens", "max_completion_tokens"} {
					if _, exists := got[sibling]; exists {
						t.Fatalf("structured source invented %s: %+v", sibling, got)
					}
				}
			})
		}
	}
}

func TestGeminiLegacyOutputField(t *testing.T) {
	models, err := parseGemini([]byte(`{"models":[{"name":"models/g","max_tokens":128000}]}`))
	if err != nil || len(models) != 1 {
		t.Fatalf("parse: %v, models: %+v", err, models)
	}
	assertMetadataJSON(t, models[0].Metadata, `{"name":"models/g","max_tokens":128000}`)
}

func TestModelparamsOutputFields(t *testing.T) {
	cases := []struct{ name, fields, want string }{
		{"distinct", `"params":[{"path":"max_tokens","range":{"max":4096}},{"path":"max_completion_tokens","range":{"max":16384}},{"path":"max_output_tokens","range":{"max":8192}}]`, `{"max_tokens":4096,"max_completion_tokens":16384,"max_output_tokens":8192}`},
		{"reversed", `"params":[{"path":"max_output_tokens","range":{"max":8192}},{"path":"max_completion_tokens","range":{"max":16384}},{"path":"max_tokens","range":{"max":4096}}]`, `{"max_tokens":4096,"max_completion_tokens":16384,"max_output_tokens":8192}`},
		{"no-maximum", `"params":[{"path":"max_tokens","default":64,"range":{"min":1}},{"path":"max_completion_tokens","range":{"max":null}},{"path":"max_output_tokens"}]`, `{}`},
		{"zero", `"params":[{"path":"max_completion_tokens","range":{"max":0}}]`, `{"max_completion_tokens":0}`},
		{"declared-destination", `"max_completion_tokens":0,"params":[{"path":"max_completion_tokens","range":{"max":32768}}]`, `{"max_completion_tokens":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := `{"provider":"p","model":"m","authType":"subscription",` + tc.fields + `}`
			tables := emptySourceTables()
			tables.mpK, tables.mpS = indexModelparams([]byte(`{"models":[`+record+`]}`), testLog())
			if _, ok := tables.lookupOne("modelparams.dev/p/api_key", "m"); ok {
				t.Fatal("subscription record leaked into API-key source")
			}
			cfg := &Config{Channels: map[string]ChannelConfig{"c": {SourcePriority: []string{"modelparams.dev/p/subscription"}}}}
			manifest := mergeManifest(nil, []channelModels{{Channel: Channel{Prefix: "c"}, Models: []ParsedModel{{ID: "m"}}}}, cfg, tables, nil)
			if len(manifest.Models) != 1 || manifest.Models[0]["slug"] != "c/m" {
				t.Fatalf("source changed membership: %+v", manifest.Models)
			}
			got := manifest.Models[0]
			tokens := map[string]any{}
			for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
				if value, exists := got[key]; exists {
					tokens[key] = value
				}
			}
			assertMetadataJSON(t, tokens, tc.want)
			var original map[string]any
			if err := decodeJSON([]byte(record), &original); err != nil {
				t.Fatal(err)
			}
			params, _ := json.Marshal(original["params"])
			assertMetadataJSON(t, got["params"], string(params))
		})
	}
}

func TestNativeNumbersAndDeclaredLimitsSurvive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.URL.Query().Get("client_version") != "1" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		fmt.Fprint(w, `{"models":[{"slug":"oauth/unlisted","context_window":272000,"max_tokens":128000,"vendor":{"sequence":9007199254740993}}]}`)
	}))
	defer server.Close()
	cpa := newCPAClient(server.URL, "fixture-management", "fixture-client", newHTTPPool(1, 0), testLog())
	base, err := cpa.NativeManifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tables := emptySourceTables()
	setHit(tables.dev, "oauth", "unlisted", sourceHit{"context_window": 1})
	manifest := mergeManifest(base, nil, &Config{}, tables, nil)
	assertMetadataJSON(t, manifest.Models, `[{"slug":"oauth/unlisted","context_window":272000,"max_tokens":128000,"vendor":{"sequence":9007199254740993}}]`)
}

func TestYAMLMetadataLayers(t *testing.T) {
	cfg, err := loadConfig(writeConfig(t, `
cpa_base_url: http://unused.invalid
static_models:
  - slug: alias
    inherit: [c/base]
    overrides:
      max_tokens: 0
      display_name: ""
      input_modalities: []
      vendor:
        zero: 0
        enabled: false
        absent: null
        sequence: 9007199254740993
        keep: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	base := &Manifest{Models: []map[string]any{{"slug": "c/base", "max_output_tokens": 100, "vendor": map[string]any{"keep": map[string]any{"child": true}}}}}
	manifest := mergeManifest(base, nil, cfg, emptySourceTables(), nil)
	assertMetadataJSON(t, manifest.Models[1], `{"slug":"alias","max_tokens":0,"max_output_tokens":100,"display_name":"","input_modalities":[],"vendor":{"zero":0,"enabled":false,"sequence":9007199254740993,"keep":{"child":true}}}`)
}

func TestSharedSourceMetadataIsIsolated(t *testing.T) {
	tables := emptySourceTables()
	setHit(tables.dev, "p", "db", sourceHit{"id": "db", "max_tokens": 200, "vendor": map[string]any{
		"enabled": true, "array": []any{map[string]any{"value": "source", "nullable": nil}},
	}})
	cfg := &Config{Channels: map[string]ChannelConfig{"c": {
		SourcePriority: []string{"models.dev/p"},
		Models: map[string]ModelConfig{
			"a": {LookupIDs: map[string]string{"models.dev/p": "db"}, Overrides: map[string]any{"max_tokens": 0, "vendor": map[string]any{"enabled": false}}},
			"b": {LookupIDs: map[string]string{"models.dev/p": "db"}},
		},
	}}}
	before, _ := json.Marshal([]any{tables.dev, cfg})
	t.Cleanup(func() {
		after, _ := json.Marshal([]any{tables.dev, cfg})
		if string(before) != string(after) {
			t.Error("shared source or config mutated")
		}
	})
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			fetched := []channelModels{{Channel: Channel{Prefix: "c"}, Models: []ParsedModel{{ID: "a"}, {ID: "b"}}}}
			manifest := mergeManifest(nil, fetched, cfg, tables, nil)
			a, b := manifest.Models[0], manifest.Models[1]
			if _, exists := a["max_output_tokens"]; exists {
				t.Fatal("override invented a sibling output field")
			}
			if a["max_tokens"] != 0 || b["max_tokens"] != 200 {
				t.Fatalf("overrides leaked: %+v", manifest.Models)
			}
			a["vendor"].(map[string]any)["array"].([]any)[0].(map[string]any)["value"] = "changed"
			assertMetadataJSON(t, b["vendor"], `{"enabled":true,"array":[{"value":"source","nullable":null}]}`)
			if a["id"] != "c/a" || b["id"] != "c/b" {
				t.Fatalf("lookup identity leaked: %+v", manifest.Models)
			}
		})
	}
}

func TestOllamaPublicRecordAndSharedLookup(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v0/management/api-call" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		fmt.Fprint(w, `{"status_code":200,"header":{"private-header":["excluded"]},"body":{"model_info":{"family.context_length":0,"vendor.sequence":9007199254740993},"details":{"family":"vision"},"capabilities":["completion","vision"],"input_modalities":[],"output_modalities":["text"]}}`)
	}))
	defer server.Close()
	cfg := &Config{Channels: map[string]ChannelConfig{"c": {
		SourcePriority: []string{"ollama_cloud"}, OllamaNativeBase: "http://ollama.invalid",
		Models: map[string]ModelConfig{
			"one": {LookupIDs: map[string]string{"ollama_cloud": "remote"}},
			"two": {LookupIDs: map[string]string{"ollama_cloud": "remote"}},
		},
	}}}
	channel := Channel{Prefix: "c", Type: "openai-compatibility", AuthIndex: "fixture-auth"}
	models := []ParsedModel{{ID: "one"}, {ID: "two"}}
	cpa := newCPAClient(server.URL, "fixture-management", "fixture-client", newHTTPPool(2, 0), testLog())
	hits, err := fetchOllamaForChannel(t.Context(), cpa, cfg, channel, models, testLog())
	if err != nil {
		t.Fatal(err)
	}
	base := &Manifest{Models: []map[string]any{{"slug": "c/one"}, {"slug": "c/two"}}}
	manifest := mergeManifest(base, []channelModels{{Channel: channel, Models: models}}, cfg, emptySourceTables(), map[string]map[string]sourceHit{"c": hits})
	for i, name := range []string{"one", "two"} {
		assertMetadataJSON(t, manifest.Models[i], fmt.Sprintf(`{"slug":"c/%s","context_window":0,"model_info":{"family.context_length":0,"vendor.sequence":9007199254740993},"details":{"family":"vision"},"capabilities":["completion","vision"],"input_modalities":[],"output_modalities":["text"]}`, name))
	}
	if calls.Load() != 1 {
		t.Fatalf("shared lookup must issue one request, got %d", calls.Load())
	}
}

func TestSourceNullKeepsNativeAndChannelMetadata(t *testing.T) {
	base := &Manifest{Models: []map[string]any{{"slug": "c/m", "context_window": 272000, "max_input_tokens": 12345, "max_output_tokens": 100, "vendor": map[string]any{"base": true}}}}
	tables := emptySourceTables()
	setHit(tables.dev, "p", "m", sourceHit{"context_window": nil, "max_tokens": nil, "max_output_tokens": nil, "vendor": nil, "display_name": nil, "input_modalities": nil, "supported_reasoning_levels": nil})
	cfg := &Config{Channels: map[string]ChannelConfig{"c": {SourcePriority: []string{"models.dev/p"}, Overrides: map[string]map[string]any{"m": {"id": nil, "display_name": nil}}}}}
	fetched := []channelModels{{Channel: Channel{Prefix: "c"}, Models: []ParsedModel{{ID: "m", Metadata: map[string]any{"id": "remote", "max_tokens": 0, "vendor": map[string]any{"channel": true}}}}}}
	manifest := mergeManifest(base, fetched, cfg, tables, nil)
	assertMetadataJSON(t, manifest.Models, `[{"slug":"c/m","id":"c/m","context_window":272000,"max_input_tokens":12345,"max_output_tokens":100,"max_tokens":0,"vendor":{"base":true,"channel":true}}]`)
}

func TestReferencesReadFixedSnapshot(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{"c": {
		SourcePriority: []string{"models.dev/missing"},
		Models: map[string]ModelConfig{
			"A": {MetadataFrom: "c/B", Overrides: map[string]any{"max_tokens": 40, "id": "wrong", "slug": "wrong", "vendor": map[string]any{"target": false}}},
			"B": {MetadataFrom: "c/C", Overrides: map[string]any{"vendor": map[string]any{"overrideB": true}}},
		},
	}}}
	for _, reverse := range []bool{false, true} {
		models := []ParsedModel{
			{ID: "A", Metadata: map[string]any{"id": "raw-A", "max_output_tokens": 10, "vendor": map[string]any{"a": true}}},
			{ID: "B", Metadata: map[string]any{"id": "raw-B", "max_tokens": 20, "input_modalities": []any{"text", "image"}, "output_modalities": []any{"text"}, "vendor": map[string]any{"b": true}}},
			{ID: "C", Metadata: map[string]any{"id": "raw-C", "max_output_tokens": 30, "input_modalities": []any{}, "vendor": map[string]any{"c": true}}},
		}
		if reverse {
			models[0], models[2] = models[2], models[0]
		}
		before, _ := json.Marshal(models)
		manifest := mergeManifest(nil, []channelModels{{Channel: Channel{Prefix: "c"}, Models: models}}, cfg, emptySourceTables(), nil)
		by := map[string]map[string]any{}
		for _, model := range manifest.Models {
			by[asString(model["slug"])] = model
		}
		assertMetadataJSON(t, by["c/A"], `{"slug":"c/A","id":"c/A","max_tokens":40,"max_output_tokens":10,"input_modalities":["text","image"],"output_modalities":["text"],"vendor":{"a":true,"b":true,"overrideB":true,"target":false}}`)
		assertMetadataJSON(t, by["c/B"], `{"slug":"c/B","id":"c/B","max_tokens":20,"max_output_tokens":30,"input_modalities":[],"output_modalities":["text"],"vendor":{"b":true,"c":true,"overrideB":true}}`)
		after, _ := json.Marshal(models)
		if string(before) != string(after) {
			t.Fatal("reference composition mutated input")
		}
	}
}

func TestStaticGenericInheritance(t *testing.T) {
	base := &Manifest{Models: []map[string]any{
		{"slug": "public/new", "max_tokens": 100, "vendor": map[string]any{"existing": true}},
		{"slug": "c/channel", "vendor": map[string]any{"lower": true}, "input_modalities": []any{"text", "image"}, "output_modalities": []any{"text"}},
	}}
	tables := emptySourceTables()
	setHit(tables.dev, "p", "m", sourceHit{"id": "database-id", "vendor": map[string]any{"pool": true}, "input_modalities": []any{}, "output_modalities": []any{"image"}, "max_output_tokens": 50})
	cfg := &Config{
		CustomChannels: map[string]ChannelConfig{"pool": {SourcePriority: []string{"models.dev/p"}, Models: map[string]ModelConfig{"m": {}}}},
		StaticModels: []map[string]any{
			{"slug": "public/new", "inherit": []any{"pool/m", "missing/x", "c/channel"}, "overrides": map[string]any{"max_tokens": 0, "id": "wrong", "slug": "wrong", "vendor": map[string]any{"final": true}}},
			{"slug": "alias/next", "inherit": "public/new"},
		},
	}
	manifest := mergeManifest(base, nil, cfg, tables, nil)
	if len(manifest.Models) != 3 {
		t.Fatalf("hidden pool or duplicate static leaked: %+v", manifest.Models)
	}
	for _, model := range manifest.Models {
		slug := asString(model["slug"])
		if slug == "c/channel" {
			continue
		}
		if slug != "public/new" && slug != "alias/next" {
			t.Fatalf("unexpected public identity: %s", slug)
		}
		assertMetadataJSON(t, model, fmt.Sprintf(`{"slug":%q,"id":%q,"max_tokens":0,"max_output_tokens":50,"input_modalities":[],"output_modalities":["image"],"vendor":{"existing":true,"lower":true,"pool":true,"final":true}}`, slug, slug))
	}
}

func TestOverrideFormsShareLayerSemantics(t *testing.T) {
	for _, flat := range []bool{false, true} {
		tables := emptySourceTables()
		setHit(tables.dev, "p", "m", sourceHit{"context_window": 100, "max_tokens": 100, "vendor": map[string]any{"keep": true}, "input_modalities": []any{"text"}})
		overrides := map[string]any{"context_window": nil, "max_tokens": 0, "vendor": map[string]any{"keep": nil, "enabled": false, "empty": ""}, "input_modalities": []any{}}
		channel := ChannelConfig{SourcePriority: []string{"models.dev/p"}}
		if flat {
			channel.Overrides = map[string]map[string]any{"m": overrides}
		} else {
			channel.Models = map[string]ModelConfig{"m": {Overrides: overrides}}
		}
		cfg := &Config{Channels: map[string]ChannelConfig{"c": channel}}
		manifest := mergeManifest(nil, []channelModels{{Channel: Channel{Prefix: "c"}, Models: []ParsedModel{{ID: "m"}}}}, cfg, tables, nil)
		assertMetadataJSON(t, manifest.Models, `[{"slug":"c/m","context_window":100,"max_tokens":0,"vendor":{"keep":true,"enabled":false,"empty":""},"input_modalities":[]}]`)
	}
	combined := ChannelConfig{
		Overrides: map[string]map[string]any{"m": {"max_tokens": 7, "display_name": "Legacy", "vendor": map[string]any{"a": 1, "b": 2}}},
		Models:    map[string]ModelConfig{"m": {Overrides: map[string]any{"max_output_tokens": nil, "display_name": nil, "vendor": map[string]any{"a": nil, "b": 0}}}},
	}
	assertMetadataJSON(t, combined.modelOverrides("m"), `{"max_tokens":7,"display_name":"Legacy","vendor":{"a":1,"b":0}}`)
}

func TestSourceFetchOrderDoesNotChangeMetadata(t *testing.T) {
	for _, apiFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(apiFirst), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.URL.Path == "/api.json") != apiFirst {
					time.Sleep(20 * time.Millisecond)
				}
				switch r.URL.Path {
				case "/api.json":
					fmt.Fprint(w, `{"p":{"models":{"m":{"id":"m","limit":{"context":111,"output":100},"vendor":{"winner":"api","api":true}}}}}`)
				case "/flat.json":
					fmt.Fprint(w, `{"p/m":{"limit":{"context":222,"output":500},"vendor":{"winner":"flat","flat":true}}}`)
				default:
					fmt.Fprint(w, `{"models":[{"model":"m","provider":"p","max_tokens":200,"vendor":{"winner":"mp"}}]}`)
				}
			}))
			defer server.Close()
			oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
			modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = server.URL+"/api.json", server.URL+"/flat.json", server.URL+"/params.json"
			defer func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM }()
			tables := fetchSources(t.Context(), newHTTPPool(3, time.Second), map[string]bool{"models.dev": true, "modelparams.dev": true}, testLog())
			cfg := &Config{Channels: map[string]ChannelConfig{"c": {SourcePriority: []string{"modelparams.dev/p/api_key", "models.dev/p"}}}}
			manifest := mergeManifest(nil, []channelModels{{Channel: Channel{Prefix: "c"}, Models: []ParsedModel{{ID: "m"}}}}, cfg, tables, nil)
			model := manifest.Models[0]
			if toInt(model["context_window"]) != 111 || toInt(model["max_tokens"]) != 200 || toInt(model["max_output_tokens"]) != 100 {
				t.Fatalf("fetch order affected source priority: %+v", model)
			}
			assertMetadataJSON(t, model["vendor"], `{"winner":"mp","api":true,"flat":true}`)
		})
	}
}

func TestHTTPPoolHoldsSlotUntilBodyClose(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	pool := newHTTPPool(1, time.Second)
	first, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
	response, err := pool.Do(first)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	second, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	unexpected, err := pool.Do(second)
	if unexpected != nil {
		unexpected.Body.Close()
	}
	if err == nil || calls.Load() != 1 {
		t.Errorf("body still active but pool admitted another request: calls=%d, err=%v", calls.Load(), err)
	}
	response.Body.Close()
	response.Body.Close() // 关闭重试不能重复释放 slot。
	third, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, nil)
	last, err := pool.Do(third)
	if err != nil {
		t.Fatal(err)
	}
	last.Body.Close()
}

func TestPublicMetadataResponseBounds(t *testing.T) {
	stubSources(t)
	blob := strings.Repeat("x", (1<<20)+3) + "end"
	raw, _ := json.Marshal(map[string]any{"models": []any{map[string]any{"slug": "oauth/large", "vendor": map[string]any{"blob": blob}}}})
	fake := &fakeCPA{native: raw, oauthModels: []string{"large"}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	handler := newTestHandler(t, testCfg(), server)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/v1/models?client_version=large", nil))
	if response.Code != 200 {
		t.Fatalf("valid large metadata failed: %d", response.Code)
	}
	var manifest Manifest
	if err := decodeJSON(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Models) != 1 {
		t.Fatalf("expected the management-matched model, got %d", len(manifest.Models))
	}
	if manifest.Models[0]["vendor"].(map[string]any)["blob"] != blob {
		t.Fatal("large unknown public field truncated")
	}

	// 有效 JSON 后的空白仍是响应字节，不能静默截掉并返回成功。
	fake.native = append([]byte(`{"models":[]}`), []byte(strings.Repeat(" ", 32<<20))...)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/v1/models?client_version=over-limit", nil))
	if response.Code != http.StatusBadGateway {
		t.Errorf("native response over 32 MiB accepted: %d", response.Code)
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "{}", strings.Repeat(" ", 16<<20))
	}))
	defer source.Close()
	if _, err := sourceGet(t.Context(), newHTTPPool(1, time.Second), source.URL); err == nil {
		t.Error("bulk source response over 16 MiB accepted")
	}
}

// 对照 Generic JSON metadata layer composition，通过最终 manifest 验证叠层契约。
func TestLayeredMetadataFixtures(t *testing.T) {
	file, err := os.Open("testdata/metadata-layers.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var cases []struct {
		Name   string
		Layers []map[string]any
		Want   map[string]any
	}
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			before, _ := json.Marshal(tc.Layers)
			baseEntry := map[string]any{"slug": "fixture/0"}
			for k, v := range tc.Layers[0] {
				baseEntry[k] = v
			}
			cfg := &Config{}
			for i, layer := range tc.Layers[1:] {
				cfg.StaticModels = append(cfg.StaticModels, map[string]any{
					"slug":      fmt.Sprintf("fixture/%d", i+1),
					"inherit":   fmt.Sprintf("fixture/%d", i),
					"overrides": layer,
				})
			}
			manifest := mergeManifest(&Manifest{Models: []map[string]any{baseEntry}}, nil, cfg, emptySourceTables(), nil)
			last := fmt.Sprintf("fixture/%d", len(tc.Layers)-1)
			var got map[string]any
			for _, m := range manifest.Models {
				if m["slug"] == last {
					got = m
				}
			}
			want := map[string]any{"slug": last}
			for k, v := range tc.Want {
				want[k] = v
			}
			actual, _ := json.Marshal(got)
			expected, _ := json.Marshal(want)
			if string(actual) != string(expected) {
				t.Fatalf("manifest metadata\ngot  %s\nwant %s", actual, expected)
			}
			// 输出中的嵌套对象也不能反向修改原始输入。
			for _, value := range got {
				if object, ok := value.(map[string]any); ok {
					object["output_only"] = true
				}
				if array, ok := value.([]any); ok && len(array) > 0 {
					array[0] = "output_only"
				}
			}
			after, _ := json.Marshal(tc.Layers)
			if string(before) != string(after) {
				t.Fatal("composition mutated the source layers")
			}
		})
	}
}
