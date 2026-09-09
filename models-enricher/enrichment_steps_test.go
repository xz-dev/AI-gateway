package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnabledEnrichmentSteps(t *testing.T) {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	congee := cfg.Channels["congee"]
	off := false
	for _, tc := range []struct {
		name                       string
		channel                    *ChannelConfig
		failInventory, failSource  bool
		wantInventory, wantSources int32
		wantContext                string
		pure                       bool
	}{
		{name: "unconfigured retains CPA baseline", wantContext: "100", pure: true},
		{name: "empty configuration uses channel data", channel: &ChannelConfig{}, wantInventory: 1, wantContext: "1000"},
		{name: "fetch disabled without sources is baseline", channel: &ChannelConfig{FetchModels: &off}, wantContext: "100", pure: true},
		{name: "fetch disabled with explicit source", channel: &ChannelConfig{FetchModels: &off, SourcePriority: []string{"modelparams.dev/p/subscription"}}, wantSources: 1, wantContext: "2048"},
		{name: "configured Congee skips inventory", channel: &congee, wantSources: 2, wantContext: "2048"},
		{name: "configured Congee source failure preserves baseline", channel: &congee, failSource: true, wantSources: 2, wantContext: "100", pure: true},
		{name: "only actual model chain is enabled", channel: &ChannelConfig{FetchModels: &off, SourcePriority: []string{"models.dev/p"}, Models: map[string]ModelConfig{"m": {SourcePriority: []string{"modelparams.dev/p/subscription"}}, "absent": {SourcePriority: []string{"models.dev/p"}}}}, wantSources: 1, wantContext: "2048"},
		{name: "empty model chain disables inherited source", channel: &ChannelConfig{FetchModels: &off, SourcePriority: []string{"models.dev/p"}, Models: map[string]ModelConfig{"m": {SourcePriority: []string{}}}}, wantContext: "100", pure: true},
		{name: "enabled channel step fails", channel: &ChannelConfig{SourcePriority: []string{"modelparams.dev/p/subscription"}}, failInventory: true, wantInventory: 1, wantContext: "100", pure: true},
		{name: "enabled source step fails", channel: &ChannelConfig{FetchModels: &off, SourcePriority: []string{"modelparams.dev/p/subscription"}}, failSource: true, wantSources: 1, wantContext: "100", pure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var inventory, sources, unwanted atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				responses := map[string]string{
					"/params.json": `{"models":[{"provider":"p","model":"m","authType":"subscription","context_window":2048}]}`,
					"/api.json":    `{"openai":{"models":{"m":{"limit":{"context":2048}}}}}`,
					"/flat.json":   `{}`,
				}
				body, exists := responses[r.URL.Path]
				if !exists {
					unwanted.Add(1)
					w.WriteHeader(503)
					return
				}
				sources.Add(1)
				if tc.failSource {
					w.WriteHeader(503)
					return
				}
				w.Write([]byte(body))
			}))
			defer source.Close()
			oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
			modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = source.URL+"/api.json", source.URL+"/flat.json", source.URL+"/params.json"
			t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })
			const native = `{"slug":"p/m","id":"p/m","context_window":100,"null":null,"false":false,"zero":0,"empty":"","large":9007199254740993}`
			fake := (&fakeCPA{native: []byte(`{"models":[` + native + `]}`), channelsBody: []byte(`{"openai-compatibility":[{"prefix":"p","base-url":"https://p","api-key-entries":[{}],"models":[{"name":"m"}]}]}`)}).handler()
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v0/management/api-call" {
					inventory.Add(1)
					if tc.failInventory {
						w.Write([]byte(`{"status_code":503}`))
						return
					}
					w.Write([]byte(`{"status_code":200,"body":{"data":[{"id":"m","context_window":1000}]}}`))
					return
				}
				fake.ServeHTTP(w, r)
			}))
			defer cpa.Close()
			cfg := &Config{OverallDeadline: 5 * time.Second, ChannelTimeout: time.Second, Channels: map[string]ChannelConfig{}}
			if tc.channel != nil {
				cfg.Channels["p"] = *tc.channel
			}
			pool := cachedTestPool(t)
			handler := handleModels(cfg, newCPAClient(cpa.URL, "m", "c", pool, testLog()), pool, testLog())
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest("GET", "/v1/models?client_version=steps", nil))
			if w.Code != 200 {
				t.Fatalf("catalog status %d: %s", w.Code, w.Body.String())
			}
			var got Manifest
			if err := decodeJSON(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Models) != 1 || got.Models[0]["context_window"] != json.Number(tc.wantContext) {
				t.Fatalf("wrong step result: %s", w.Body.String())
			}
			if tc.pure {
				assertMetadataJSON(t, got.Models[0], native)
			}
			if inventory.Load() != tc.wantInventory || sources.Load() != tc.wantSources || unwanted.Load() != 0 {
				t.Fatalf("wrong enabled requests: inventory=%d sources=%d unwanted=%d", inventory.Load(), sources.Load(), unwanted.Load())
			}
		})
	}
}

func TestFetchModelsConfigDefaultAndFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("cpa_base_url: http://cpa\nchannels:\n  default: {}\n  off:\n    fetch_models: false\n  on:\n    fetch_models: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Channels["default"].fetchModelsEnabled() || !cfg.Channels["missing"].fetchModelsEnabled() || cfg.Channels["off"].fetchModelsEnabled() || !cfg.Channels["on"].fetchModelsEnabled() {
		t.Fatal("fetch_models defaults/explicit values changed")
	}
}

func TestConfiguredSubscriptionChannels(t *testing.T) {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	axis, xl := cfg.Channels["axis"], cfg.Channels["xl"]
	if axis.fetchModelsEnabled() || !xl.fetchModelsEnabled() {
		t.Fatal("Axis must skip inventory; XL must retain its own inventory")
	}
	if strings.Join(axis.SourcePriority, ",") != "models.dev/openai" {
		t.Fatal("Axis must use only the OpenAI catalog source; subscription parameters proved redundant (zero-diff replay)")
	}
	if len(axis.Models) != 0 || len(xl.Models) != 1 || strings.Join(xl.Models["muse-spark-1.3-contributor"].SourcePriority, ",") != "models.dev/meta" {
		t.Fatal("only the explicit XL Muse manufacturer binding may replace the channel chain")
	}
	for name, ch := range map[string]ChannelConfig{"axis": axis, "xl": xl} {
		if len(ch.Overrides) != 0 || len(ch.SourcePriority) == 0 {
			t.Fatalf("%s must retain its channel sources without manual overrides", name)
		}
		for _, token := range ch.SourcePriority {
			if !strings.HasPrefix(token, "models.dev/") && (!strings.HasPrefix(token, "modelparams.dev/") || !strings.HasSuffix(token, "/subscription")) {
				t.Fatalf("%s needs a catalog source or subscription parameters: %s", name, token)
			}
		}
	}
	if !chainHas(xl.SourcePriority, "modelparams.dev/xai/subscription") {
		t.Fatal("XL needs its direct xAI subscription source for the grok default reasoning level")
	}
}
