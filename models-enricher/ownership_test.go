package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestManagedMembershipComesOnlyFromCPA(t *testing.T) {
	for _, kind := range []string{"openai-compatibility", "claude-api-key", "codex-api-key", "xai-api-key", "vertex-api-key"} {
		for _, upstream := range [][]ParsedModel{
			{{ID: "A", Metadata: map[string]any{"display_name": "upstream A"}}, {ID: "B"}},
			{{ID: "B"}}, nil,
		} {
			cfg := &Config{Channels: map[string]ChannelConfig{"p": {Overrides: map[string]map[string]any{"A": {"max_tokens": 1234}}}}}
			base := &Manifest{Models: []map[string]any{{"slug": "p/A", "display_name": "CPA A"}, {"slug": "oauth/native"}}}
			got := mergeManifest(base, []channelModels{{Channel: Channel{Type: kind, Prefix: "p"}, Models: upstream}}, cfg, &SourceTables{}, nil)
			if len(got.Models) != 2 {
				t.Fatalf("%s added upstream-only member: %#v", kind, got.Models)
			}
			if got.Models[0]["slug"] != "p/A" || got.Models[0]["max_tokens"] != 1234 {
				t.Fatalf("%s lost CPA member/override: %#v", kind, got.Models)
			}
		}
	}
}

func TestLegacyKindsRetainMembershipAndFilters(t *testing.T) {
	for _, kind := range []string{"gemini-api-key", "interactions-api-key"} {
		cfg := &Config{Channels: map[string]ChannelConfig{"p": {Exclude: []string{"^B$"}, exclude: []*regexp.Regexp{regexp.MustCompile("^B$")}, SourcePriority: []string{"models.dev/google"}}}}
		channel := Channel{Type: kind, Prefix: "p"}
		if err := validateRuntime(cfg, []Channel{channel}, &Manifest{}); err != nil {
			t.Fatal(err)
		}
		got := mergeManifest(&Manifest{}, []channelModels{{Channel: channel, Models: []ParsedModel{{ID: "A"}, {ID: "B"}}}}, cfg, &SourceTables{}, nil)
		if len(got.Models) != 1 || got.Models[0]["slug"] != "p/A" {
			t.Fatalf("%s legacy behavior changed: %#v", kind, got.Models)
		}
	}
}

func TestManagedFetchFailureReturnsPureCPAMember(t *testing.T) {
	stubSources(t)
	fake := (&fakeCPA{native: []byte(`{"models":[{"slug":"oc/deepseek-v4-flash:preview"}]}`), channelsBody: testChannels}).handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/api-call" {
			w.WriteHeader(503)
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	cfg := testCfg()
	ch := cfg.Channels["oc"]
	ch.Models = map[string]ModelConfig{"deepseek-v4-flash:preview": {LookupIDs: map[string]string{"models.dev/deepseek": "deepseek-v4-flash"}, Overrides: map[string]any{"max_tokens": 4321}}}
	cfg.Channels["oc"] = ch
	rec := httptest.NewRecorder()
	newTestHandler(t, cfg, server).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=test", nil))
	if rec.Code != 200 {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var manifest Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Models) != 1 {
		t.Fatalf("lost CPA member: %#v", manifest.Models)
	}
	assertMetadataJSON(t, manifest.Models[0], `{"slug":"oc/deepseek-v4-flash:preview"}`)
}

func TestDisabledInventoryDoesNotPublishButStaticAndOAuthRemain(t *testing.T) {
	cfg := &Config{CustomChannels: map[string]ChannelConfig{"hidden": {Models: map[string]ModelConfig{"A": {Overrides: map[string]any{"display_name": "static"}}}}}, StaticModels: []map[string]any{{"slug": "alias", "inherit": []string{"hidden/A"}}}}
	got := mergeManifest(&Manifest{Models: []map[string]any{{"slug": "oauth/native"}}}, []channelModels{{Channel: Channel{Type: "openai-compatibility", Prefix: "disabled"}, Models: []ParsedModel{{ID: "A"}}}}, cfg, &SourceTables{}, nil)
	if len(got.Models) != 2 || got.Models[0]["slug"] != "oauth/native" || got.Models[1]["slug"] != "alias" || got.Models[1]["display_name"] != "static" {
		t.Fatalf("catalog boundary changed: %#v", got.Models)
	}
}

func TestManagedLegacyFiltersAreRejected(t *testing.T) {
	cfg := &Config{Channels: map[string]ChannelConfig{"p": {Include: []string{"A"}, SourcePriority: []string{"models.dev/openai"}}}}
	if err := validateRuntime(cfg, []Channel{{Type: "openai-compatibility", Prefix: "p"}}, &Manifest{}); err == nil {
		t.Fatal("managed old filter silently accepted")
	}
}
