package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOneOllamaModelFailureRollsBackWholeChannelAndRetainsSuccess(t *testing.T) {
	var good, bad, unrelated atomic.Int32
	fake := (&fakeCPA{native: []byte(`{"models":[{"slug":"p/a","context_window":100,"null":null},{"slug":"p/b","context_window":200},{"slug":"q/c","context_window":300}]}`), channelsBody: []byte(`{"openai-compatibility":[{"prefix":"p","base-url":"https://p","api-key-entries":[{}],"models":[{"name":"a"},{"name":"b"}]},{"prefix":"q","base-url":"https://q","api-key-entries":[{}],"models":[{"name":"c"}]}]}`)}).handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/api-call" {
			fake.ServeHTTP(w, r)
			return
		}
		var payload struct {
			URL  string `json:"url"`
			Data string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if !strings.HasSuffix(payload.URL, "/api/show") {
			t.Error("disabled channel inventory was fetched")
			w.WriteHeader(503)
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(payload.Data), &request); err != nil {
			t.Error(err)
			return
		}
		switch request.Model {
		case "a":
			good.Add(1)
		case "b":
			bad.Add(1)
			w.Write([]byte(`{"status_code":503}`))
			return
		case "c":
			unrelated.Add(1)
		}
		w.Write([]byte(`{"status_code":200,"body":{"model_info":{"family.context_length":999}}}`))
	}))
	defer server.Close()
	off := false
	cfg := &Config{OverallDeadline: 5 * time.Second, ChannelTimeout: time.Second, Channels: map[string]ChannelConfig{
		"p": {FetchModels: &off, SourcePriority: []string{"ollama_cloud"}, OllamaNativeBase: "https://p"},
		"q": {FetchModels: &off, SourcePriority: []string{"ollama_cloud"}, OllamaNativeBase: "https://q"},
	}}
	pool := cachedTestPool(t)
	handler := handleModels(cfg, newCPAClient(server.URL, "m", "c", pool, testLog()), pool, testLog())
	for range 2 {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/v1/models?client_version=ollama-fallback", nil))
		if w.Code != 200 {
			t.Fatalf("whole catalog failed: %d", w.Code)
		}
		var got Manifest
		if err := decodeJSON(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Models) != 3 {
			t.Fatal("lost CPA membership")
		}
		assertMetadataJSON(t, got.Models[0], `{"slug":"p/a","context_window":100,"null":null}`)
		assertMetadataJSON(t, got.Models[1], `{"slug":"p/b","context_window":200}`)
		if got.Models[2]["context_window"] != json.Number("999") {
			t.Fatal("unrelated provider did not enrich")
		}
	}
	if good.Load() != 1 || unrelated.Load() != 1 || bad.Load() != 2 {
		t.Fatalf("success caches lost or failure cached: a=%d b=%d c=%d", good.Load(), bad.Load(), unrelated.Load())
	}
}
