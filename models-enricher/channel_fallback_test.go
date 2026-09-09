package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedSourceFailureRollsBackOnlyDependentChannels(t *testing.T) {
	var successfulCalls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			w.WriteHeader(503)
		case "/flat.json":
			w.Write([]byte(`{}`))
		default:
			successfulCalls.Add(1)
			w.Write([]byte(`{"models":[{"provider":"p","model":"m","authType":"subscription","context_window":2048}]}`))
		}
	}))
	defer source.Close()
	oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = source.URL+"/api.json", source.URL+"/flat.json", source.URL+"/params.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })
	fake := &fakeCPA{
		native:       []byte(`{"models":[{"slug":"a/m","id":"a/m","context_window":100,"max_tokens":null,"max_completion_tokens":0,"output_modalities":null,"nested":{"null":null,"n":9007199254740993}},{"slug":"b/m","context_window":200},{"slug":"c/m","context_window":300}]}`),
		channelsBody: []byte(`{"openai-compatibility":[{"prefix":"a","base-url":"https://a","api-key-entries":[{}],"models":[{"name":"m"}]},{"prefix":"b","base-url":"https://b","api-key-entries":[{}],"models":[{"name":"m"}]},{"prefix":"c","base-url":"https://c","api-key-entries":[{}],"models":[{"name":"m"}]}]}`),
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	cfg := &Config{OverallDeadline: 5 * time.Second, ChannelTimeout: time.Second, Channels: map[string]ChannelConfig{
		"a": {SourcePriority: []string{"models.dev/p", "modelparams.dev/p/subscription"}, Models: map[string]ModelConfig{"m": {Overrides: map[string]any{"display_name": "must not survive"}}}},
		"b": {SourcePriority: []string{"models.dev/p"}},
		"c": {SourcePriority: []string{"modelparams.dev/p/subscription"}},
	}, StaticModels: []map[string]any{{"slug": "a/m", "overrides": map[string]any{"context_window": 9999}}}}
	pool := cachedTestPool(t)
	handler := handleModels(cfg, newCPAClient(cpa.URL, "m", "c", pool, testLog()), pool, testLog())
	var baseline Manifest
	if err := decodeJSON(fake.native, &baseline); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		r := httptest.NewRequest("GET", "/v1/models?client_version=fallback", nil)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != 200 {
			t.Fatalf("one source failed whole catalog: %d %s", w.Code, w.Body.String())
		}
		var got Manifest
		if err := decodeJSON(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		models := map[string]map[string]any{}
		for _, m := range got.Models {
			models[m["slug"].(string)] = m
		}
		for _, m := range baseline.Models[:2] {
			if !reflect.DeepEqual(models[m["slug"].(string)], m) {
				t.Fatalf("failed channel is not pure CPA baseline: %s", m["slug"])
			}
		}
		if models["c/m"]["context_window"] != json.Number("2048") {
			t.Fatal("unrelated channel enrichment lost")
		}
	}
	if successfulCalls.Load() != 1 {
		t.Fatal("successful source cache was discarded")
	}
}

// 可用旧缓存把暂时失败转为成功读取，不能误触发整渠道降级。
func TestUsableStaleSourceStillEnrichesChannel(t *testing.T) {
	var status atomic.Int32
	status.Store(200)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		if status.Load() == 200 {
			w.Write([]byte(`{"models":[{"provider":"p","model":"m","authType":"subscription","context_window":2048}]}`))
		}
	}))
	defer source.Close()
	old := modelparamsURL
	modelparamsURL = source.URL
	t.Cleanup(func() { modelparamsURL = old })
	fake := &fakeCPA{native: []byte(`{"models":[{"slug":"p/m","context_window":100}]}`), channelsBody: []byte(`{"openai-compatibility":[{"prefix":"p","base-url":"https://p","api-key-entries":[{}],"models":[{"name":"m"}]}]}`)}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	off := false
	cfg := &Config{OverallDeadline: 5 * time.Second, ChannelTimeout: time.Second, Channels: map[string]ChannelConfig{"p": {FetchModels: &off, SourcePriority: []string{"modelparams.dev/p/subscription"}}}}
	pool := cachedTestPool(t)
	now := time.Now()
	pool.cache.clock = func() time.Time { return now }
	handler := handleModels(cfg, newCPAClient(cpa.URL, "m", "c", pool, testLog()), pool, testLog())
	for range 2 {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/v1/models?client_version=stale-source", nil))
		var got Manifest
		if w.Code != 200 || decodeJSON(w.Body.Bytes(), &got) != nil || len(got.Models) != 1 || got.Models[0]["context_window"] != json.Number("2048") {
			t.Fatalf("usable stale source incorrectly downgraded: %d %s", w.Code, w.Body.String())
		}
		status.Store(503)
		now = now.Add(365 * 24 * time.Hour)
	}
}
