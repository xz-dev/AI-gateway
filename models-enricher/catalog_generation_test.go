package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStandardAndCodexCatalogsShareAdmittedIDs(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native:      []byte(`{"models":[{"slug":"oauth/m1","id":"oauth/m1"}]}`),
		oauthModels: []string{"m1"},
	}
	cpa := httptest.NewServer(fake.handler())
	t.Cleanup(cpa.Close)
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"axis/模型 + exact"}]}`))
	}))
	t.Cleanup(aisix.Close)
	cfg := testCfg()
	cfg.AISIXModelsURL = aisix.URL
	handler := newTestHandler(t, cfg, cpa)

	standardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(standardRecorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if standardRecorder.Code != http.StatusOK {
		t.Fatalf("standard catalog: HTTP %d: %s", standardRecorder.Code, standardRecorder.Body.String())
	}
	var standard standardModelsResponse
	if err := json.Unmarshal(standardRecorder.Body.Bytes(), &standard); err != nil {
		t.Fatal(err)
	}
	standardIDs := map[string]bool{}
	for _, model := range standard.Data {
		standardIDs[model.ID] = true
	}

	codexRecorder := httptest.NewRecorder()
	handler.ServeHTTP(codexRecorder, httptest.NewRequest(http.MethodGet, "/v1/models?client_version=client", nil))
	if codexRecorder.Code != http.StatusOK {
		t.Fatalf("Codex catalog: HTTP %d: %s", codexRecorder.Code, codexRecorder.Body.String())
	}
	var codex Manifest
	if err := decodeJSON(codexRecorder.Body.Bytes(), &codex); err != nil {
		t.Fatal(err)
	}
	codexIDs := map[string]bool{}
	for _, model := range codex.Models {
		codexIDs[asString(model["slug"])] = true
	}
	if len(standardIDs) != len(codexIDs) {
		t.Fatalf("catalog ID set sizes differ: standard=%v Codex=%v", standardIDs, codexIDs)
	}
	for id := range standardIDs {
		if !codexIDs[id] {
			t.Fatalf("standard ID %q absent from Codex catalog", id)
		}
	}
	if !standardIDs["axis/模型 + exact"] {
		t.Fatalf("standard projection changed exact ID bytes: %v", standardIDs)
	}
}

func TestCatalogCacheIsPinnedToRawGeneration(t *testing.T) {
	stubSources(t)
	var native atomic.Value
	native.Store(`{"models":[{"slug":"old"}]}`)
	base := (&fakeCPA{}).handler()
	buildStarted := make(chan struct{})
	releaseBuild := make(chan struct{})
	var blockOnce sync.Once
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(native.Load().(string)))
			return
		}
		if r.URL.Path == "/v0/management/openai-compatibility" {
			blockOnce.Do(func() {
				close(buildStarted)
				<-releaseBuild
			})
		}
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(cpa.Close)

	cfg := testCfg()
	cfg.BareModelsTakeover = true
	cfg.Channels = map[string]ChannelConfig{}
	pool := newHTTPPool(4, 2*time.Second)
	client := newCPAClient(cpa.URL, "management", "client", pool, testLog())
	owner := newRoutingSnapshotOwner(client, nil, time.Minute, time.Second, testLog())
	_ = owner.refresh(t.Context()) // AISIX is intentionally unavailable; CPA is authoritative.
	oldGeneration := owner.current().generation
	handler := handleModels(cfg, client, nil, pool, testLog(), owner)

	catalogCacheTTL.Store(int64(time.Minute))
	catalogCacheMaxBytes.Store(1 << 20)
	t.Cleanup(func() {
		catalogCacheTTL.Store(0)
		catalogCacheMaxBytes.Store(0)
	})

	oldResponse := make(chan []byte, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models?client_version=race", nil))
		oldResponse <- recorder.Body.Bytes()
	}()
	select {
	case <-buildStarted:
	case <-time.After(time.Second):
		t.Fatal("old-generation catalog build did not start")
	}

	native.Store(`{"models":[{"slug":"new"}]}`)
	_ = owner.refresh(t.Context())
	if owner.current().generation <= oldGeneration {
		t.Fatal("raw inventory change did not advance generation")
	}
	close(releaseBuild)

	var oldManifest Manifest
	if err := decodeJSON(<-oldResponse, &oldManifest); err != nil {
		t.Fatal(err)
	}
	if len(oldManifest.Models) != 1 || asString(oldManifest.Models[0]["slug"]) != "old" {
		t.Fatalf("in-flight reader did not finish its pinned generation: %+v", oldManifest.Models)
	}

	currentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(currentRecorder, httptest.NewRequest(http.MethodGet, "/v1/models?client_version=race", nil))
	if currentRecorder.Code != http.StatusOK {
		t.Fatalf("current catalog: HTTP %d: %s", currentRecorder.Code, currentRecorder.Body.String())
	}
	var current Manifest
	if err := decodeJSON(currentRecorder.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if len(current.Models) != 1 || asString(current.Models[0]["slug"]) != "new" {
		t.Fatalf("stale completed projection served as current: %+v", current.Models)
	}
}
