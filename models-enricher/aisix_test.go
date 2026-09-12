package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testAISIXHandler(cfg *Config, cpaServer *httptest.Server, pool *httpPool) http.Handler {
	cpa := newCPAClient(cpaServer.URL, "m", "c", pool, testLog())
	aisix := newAISIXClient(cfg.AISIXModelsURL, cfg.AISIXToken, cfg.AISIXTimeout, pool)
	return handleModels(cfg, cpa, aisix, pool, testLog())
}

// Task 2.2: Overlap, duplicates, and CPA precedence
func TestAISIXCatalogOverlapOutsideIn(t *testing.T) {
	stubSources(t)
	// CPA returns model-a and vendor/model-b
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"model-a","context_window":128000},{"slug":"vendor/model-b","context_window":64000}]}`),
		channelsBody: []byte(`{"openai-compatibility": [
			{"name": "Vendor", "prefix": "vendor", "base-url": "https://vendor.invalid/v1", "api-key-entries": [{"auth-index": "k1"}], "models": [{"name": "model-b"}]}
		]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	// AISIX returns model-a, vendor/model-b, and coding-auto twice
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "model-a"},
				{"id": "vendor/model-b"},
				{"id": "coding-auto"},
				{"id": "coding-auto"}
			]
		}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels: map[string]ChannelConfig{
			"vendor": {
				SourcePriority: []string{"models.dev/openai"},
			},
		},
		CustomChannels:       map[string]ChannelConfig{},
		GlobalSourcePriority: []string{"models.dev/openai"},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models?client_version=v-overlap", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	countCodingAuto := 0
	bySlug := map[string]map[string]any{}
	for _, m := range manifest.Models {
		slug := asString(m["slug"])
		if slug == "coding-auto" {
			countCodingAuto++
		}
		bySlug[slug] = m
	}

	if countCodingAuto != 1 {
		t.Fatalf("expected exactly 1 coding-auto, got %d", countCodingAuto)
	}
	if bySlug["model-a"] == nil {
		t.Fatalf("expected model-a from CPA to be preserved")
	}
	if toInt(bySlug["model-a"]["context_window"]) != 128000 {
		t.Fatalf("expected model-a CPA metadata preserved, got %v", bySlug["model-a"]["context_window"])
	}
	if bySlug["vendor/model-b"] == nil {
		t.Fatalf("expected vendor/model-b from CPA to be preserved")
	}
	if toInt(bySlug["vendor/model-b"]["context_window"]) != 64000 {
		t.Fatalf("expected vendor/model-b CPA metadata preserved, got %v", bySlug["vendor/model-b"]["context_window"])
	}
}

// Task 2.1: Disabled mode makes no request
func TestAISIXDisabledModeMakesNoRequest(t *testing.T) {
	stubSources(t)
	var aisixCalls atomic.Int64
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aisixCalls.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list","data":[{"id":"should-not-be-called"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-only"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    "", // disabled
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models?client_version=v-disabled", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if aisixCalls.Load() != 0 {
		t.Fatalf("expected 0 AISIX calls in disabled mode, got %d", aisixCalls.Load())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	if len(manifest.Models) != 1 || asString(manifest.Models[0]["slug"]) != "cpa-only" {
		t.Fatalf("unexpected manifest models: %v", manifest.Models)
	}
}

// Task 2.1: Valid empty list contributes no entries and overwrites older nonempty cache
func TestAISIXValidEmptyListOverwritesCache(t *testing.T) {
	stubSources(t)
	var aisixBody atomic.Value
	aisixBody.Store(`{"object":"list","data":[{"id":"initial-extra"}]}`)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(aisixBody.Load().(string)))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-base"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	cache, err := newReadCache("", testLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cache.dir) })
	pool.cache = cache

	handler := testAISIXHandler(cfg, cpaServer, pool)

	// Step 1: initial run caches initial-extra
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest("GET", "/v1/models?client_version=v-empty-1", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("step 1 failed: %d", rec1.Code)
	}
	var m1 Manifest
	json.Unmarshal(rec1.Body.Bytes(), &m1)
	foundInitial := false
	for _, m := range m1.Models {
		if asString(m["slug"]) == "initial-extra" {
			foundInitial = true
		}
	}
	if !foundInitial {
		t.Fatalf("step 1 missing initial-extra")
	}

	// Step 2: AISIX now returns valid empty list
	aisixBody.Store(`{"object":"list","data":[]}`)
	// Advance clock past TTL to force fresh fetch
	cache.clock = func() time.Time { return time.Now().Add(10 * time.Minute) }

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest("GET", "/v1/models?client_version=v-empty-2", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("step 2 failed: %d", rec2.Code)
	}
	var m2 Manifest
	json.Unmarshal(rec2.Body.Bytes(), &m2)
	for _, m := range m2.Models {
		if asString(m["slug"]) == "initial-extra" {
			t.Fatalf("valid empty list must overwrite older nonempty success; found initial-extra")
		}
	}
}

// Task 2.1: Malformed body does not use stale cache and invalidates existing cache
func TestAISIXMalformedBodyInvalidatesCache(t *testing.T) {
	stubSources(t)
	var aisixBody atomic.Value
	aisixBody.Store(`{"object":"list","data":[{"id":"initial-cached"}]}`)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(aisixBody.Load().(string)))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-base"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	cache, err := newReadCache("", testLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cache.dir) })
	pool.cache = cache

	handler := testAISIXHandler(cfg, cpaServer, pool)

	// Step 1: initial run caches initial-cached
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest("GET", "/v1/models?client_version=v-mal-1", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("step 1 failed: %d", rec1.Code)
	}

	// Step 2: AISIX returns malformed body (missing data field or null data)
	aisixBody.Store(`{"object":"list","data":null}`)
	cache.clock = func() time.Time { return time.Now().Add(10 * time.Minute) }

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest("GET", "/v1/models?client_version=v-mal-2", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("step 2 must succeed with CPA baseline, got %d", rec2.Code)
	}
	var m2 Manifest
	json.Unmarshal(rec2.Body.Bytes(), &m2)
	for _, m := range m2.Models {
		if asString(m["slug"]) == "initial-cached" {
			t.Fatalf("malformed body must not conceal behind stale cache; initial-cached resurrected")
		}
	}
}

// Task 2.1: Authentication failure (401) invalidates cache and does not resurrect stale data
func TestAISIXAuthFailureInvalidatesCache(t *testing.T) {
	stubSources(t)
	var aisixStatus atomic.Int64
	aisixStatus.Store(http.StatusOK)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := int(aisixStatus.Load())
		if status != http.StatusOK {
			w.WriteHeader(status)
			w.Write([]byte(`{"error":{"message":"Unauthorized"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"initial-auth-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-base"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		AISIXToken:        "test-token",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	cache, err := newReadCache("", testLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cache.dir) })
	pool.cache = cache

	handler := testAISIXHandler(cfg, cpaServer, pool)

	// Step 1: cache success
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest("GET", "/v1/models?client_version=v-auth-1", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("step 1 failed: %d", rec1.Code)
	}

	// Step 2: 401 Unauthorized
	aisixStatus.Store(http.StatusUnauthorized)
	cache.clock = func() time.Time { return time.Now().Add(10 * time.Minute) }

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest("GET", "/v1/models?client_version=v-auth-2", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("step 2 must succeed with CPA baseline, got %d", rec2.Code)
	}
	var m2 Manifest
	json.Unmarshal(rec2.Body.Bytes(), &m2)
	for _, m := range m2.Models {
		if asString(m["slug"]) == "initial-auth-model" {
			t.Fatalf("auth failure must not resurrect stale data; found initial-auth-model")
		}
	}
}

// Task 2.1: Timeout / 502 permits stale cache fallback
func TestAISIXTimeoutPermitsStaleCache(t *testing.T) {
	stubSources(t)
	var aisixStatus atomic.Int64
	aisixStatus.Store(http.StatusOK)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := int(aisixStatus.Load())
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"stale-permitted-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-base"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	cache, err := newReadCache("", testLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cache.dir) })
	pool.cache = cache

	handler := testAISIXHandler(cfg, cpaServer, pool)

	// Step 1: cache success
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest("GET", "/v1/models?client_version=v-stale-1", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("step 1 failed: %d", rec1.Code)
	}

	// Step 2: upstream 502 Bad Gateway
	aisixStatus.Store(http.StatusBadGateway)
	cache.clock = func() time.Time { return time.Now().Add(10 * time.Minute) }

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest("GET", "/v1/models?client_version=v-stale-2", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("step 2 must succeed with stale cache, got %d", rec2.Code)
	}
	var m2 Manifest
	json.Unmarshal(rec2.Body.Bytes(), &m2)
	foundStale := false
	for _, m := range m2.Models {
		if asString(m["slug"]) == "stale-permitted-model" {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("expected stale-permitted-model to be returned from stale cache on 502")
	}
}

// Task 2.1: Timeout without cache omits supplement without blocking CPA success
func TestAISIXTimeoutWithoutCacheOmitsSupplement(t *testing.T) {
	stubSources(t)
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond) // exceeds AISIXTimeout
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"slow-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-base"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		AISIXTimeout:      50 * time.Millisecond, // tight bounded timeout
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	start := time.Now()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-timeout", nil))
	duration := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if duration > 1*time.Second {
		t.Fatalf("timeout took too long: %v, must not exhaust overall deadline", duration)
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	for _, m := range manifest.Models {
		if asString(m["slug"]) == "slow-model" {
			t.Fatalf("slow-model should not have been included on timeout")
		}
	}
}

// Task 2.1: Authenticated request header is sent
func TestAISIXAuthenticatedHeader(t *testing.T) {
	stubSources(t)
	var authHeader atomic.Value
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"auth-check"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-base"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		AISIXToken:        "secret-newapi-token",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-auth-header", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	gotHeader, _ := authHeader.Load().(string)
	if gotHeader != "Bearer secret-newapi-token" {
		t.Fatalf("expected Bearer secret-newapi-token, got %q", gotHeader)
	}
}

// Task 2.2: CPA filtering does not resurrect a duplicate
func TestAISIXCPAFilteringDoesNotResurrectDuplicate(t *testing.T) {
	stubSources(t)
	// CPA has filtered-cpa-model in native manifest, but no channel registers it and BareModelsTakeover=false
	// so identities.filter drops it from CPA catalog.
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"filtered-cpa-model"},{"slug":"admitted-model"}]}`),
		channelsBody: []byte(`{"openai-compatibility": [
			{"name": "Adm", "prefix": "adm", "base-url": "https://adm.invalid/v1", "api-key-entries": [{"auth-index": "k1"}], "models": [{"name": "model"}]}
		]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	// AISIX returns filtered-cpa-model and new-model
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"filtered-cpa-model"},{"id":"new-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: false, // filtered-cpa-model will be filtered by CPA local rules
		Channels: map[string]ChannelConfig{
			"adm": {SourcePriority: []string{}},
		},
		CustomChannels: map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-filter-dup", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	bySlug := map[string]bool{}
	for _, m := range manifest.Models {
		bySlug[asString(m["slug"])] = true
	}

	if bySlug["filtered-cpa-model"] {
		t.Fatalf("filtered-cpa-model must NOT be reintroduced through AISIX supplement")
	}
	if !bySlug["new-model"] {
		t.Fatalf("expected new-model to be admitted")
	}
}

// Task 2.2: Qualified names are distinct from bare names
func TestAISIXQualifiedNamesAreDistinct(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"model-a"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"vendor/model-a"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-qualified", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	bySlug := map[string]map[string]any{}
	for _, m := range manifest.Models {
		bySlug[asString(m["slug"])] = m
	}

	if bySlug["model-a"] == nil {
		t.Fatalf("model-a should exist")
	}
	if bySlug["vendor/model-a"] == nil {
		t.Fatalf("vendor/model-a should exist as distinct supplemental ID")
	}
	// Verify ID is not shortened or rewritten
	if asString(bySlug["vendor/model-a"]["id"]) != "vendor/model-a" {
		t.Fatalf("expected id to be vendor/model-a, got %v", bySlug["vendor/model-a"]["id"])
	}
}

// Task 2.2: Case-sensitive full IDs
func TestAISIXCaseSensitiveFullIDs(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"model-a"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"MODEL-A"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-case", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	bySlug := map[string]bool{}
	for _, m := range manifest.Models {
		bySlug[asString(m["slug"])] = true
	}

	if !bySlug["model-a"] || !bySlug["MODEL-A"] {
		t.Fatalf("both model-a and MODEL-A should exist due to case sensitivity: %v", bySlug)
	}
}

// Task 2.2: CPA baseline failure preserved
func TestAISIXCPABaselineFailurePreserved(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		nativeStatus: http.StatusBadGateway,
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"new-api-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-cpa-fail", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway on CPA baseline failure, got %d", rec.Code)
	}
}

// Task 2.3: Metadata lookup succeeds for supplemental model & opaque alias retains basic record
func TestAISIXMetadataLookupAndOpaqueAlias(t *testing.T) {
	stubSources(t)
	// mock sources with deepseek-v4-flash
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-model"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-v4-flash"},{"id":"coding-auto"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:           cpaServer.URL,
		AISIXModelsURL:      aisixServer.URL + "/v1/models",
		HTTPConcurrency:      8,
		ChannelTimeout:       2 * time.Second,
		OverallDeadline:      3 * time.Second,
		BareModelsTakeover:   true,
		Channels:             map[string]ChannelConfig{},
		CustomChannels:       map[string]ChannelConfig{},
		GlobalSourcePriority: []string{"models.dev/deepseek"},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-meta", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	bySlug := map[string]map[string]any{}
	for _, m := range manifest.Models {
		bySlug[asString(m["slug"])] = m
	}

	// deepseek-v4-flash matches models.dev/deepseek from stubSources -> context_window 128000
	ds := bySlug["deepseek-v4-flash"]
	if ds == nil {
		t.Fatalf("expected deepseek-v4-flash in manifest")
	}
	if toInt(ds["context_window"]) != 128000 {
		t.Fatalf("expected enriched context_window 128000, got %v", ds["context_window"])
	}

	// coding-auto has no source match -> basic record with no synthesized context_window
	ca := bySlug["coding-auto"]
	if ca == nil {
		t.Fatalf("expected coding-auto in manifest")
	}
	if ca["context_window"] != nil {
		t.Fatalf("expected coding-auto to have no context_window, got %v", ca["context_window"])
	}
	if asString(ca["id"]) != "coding-auto" || asString(ca["slug"]) != "coding-auto" {
		t.Fatalf("expected id and slug to be coding-auto, got id=%v slug=%v", ca["id"], ca["slug"])
	}
}

// Task 2.3: Existing static metadata overlaps supplement
func TestAISIXExistingStaticMetadataOverlapsSupplement(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-model"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"static-overlap-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
		StaticModels: []map[string]any{
			{
				"slug":      "static-overlap-model",
				"overrides": map[string]any{"context_window": 999999},
			},
		},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-static-overlap", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	count := 0
	var model map[string]any
	for _, m := range manifest.Models {
		if asString(m["slug"]) == "static-overlap-model" {
			count++
			model = m
		}
	}

	if count != 1 {
		t.Fatalf("expected static-overlap-model exactly once, got %d", count)
	}
	if toInt(model["context_window"]) != 999999 {
		t.Fatalf("expected static override context_window 999999, got %v", model["context_window"])
	}
}

// Task 2.3: Slash IDs stay exact and avoid CPA channel admission
func TestAISIXSlashIDsStayExactAndAvoidCPAAdmission(t *testing.T) {
	stubSources(t)
	// CPA has a channel "failing-chan" whose api-call fails
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-ok"}]}`),
		channelsBody: []byte(`{"openai-compatibility": [
			{"name": "Failing", "prefix": "failing-chan", "base-url": "https://fail.invalid/v1", "api-key-entries": [{"auth-index": "k1"}], "models": [{"name": "ch-model"}]}
		]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	// AISIX returns a slash model under "failing-chan/my-model"
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"failing-chan/my-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels: map[string]ChannelConfig{
			"failing-chan": {SourcePriority: []string{}},
		},
		CustomChannels: map[string]ChannelConfig{},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v-slash-exact", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	var found map[string]any
	for _, m := range manifest.Models {
		if asString(m["slug"]) == "failing-chan/my-model" {
			found = m
		}
	}

	if found == nil {
		t.Fatalf("expected failing-chan/my-model to be admitted without CPA channel admission")
	}
	if asString(found["id"]) != "failing-chan/my-model" || asString(found["slug"]) != "failing-chan/my-model" {
		t.Fatalf("expected exact slash ID preserved, got id=%v slug=%v", found["id"], found["slug"])
	}
}

// Task 2.3: Unchanged CPA-only output when AISIX returns no difference
func TestAISIXUnchangedCPAOnlyOutput(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"cpa-model-1","context_window":32000}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	// Run 1: AISIX disabled
	cfgDisabled := &Config{
		CPABaseURL:         cpaServer.URL,
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}
	pool1 := newHTTPPool(8, 5*time.Second)
	handler1 := testAISIXHandler(cfgDisabled, cpaServer, pool1)
	rec1 := httptest.NewRecorder()
	handler1.ServeHTTP(rec1, httptest.NewRequest("GET", "/v1/models?client_version=v-cpa-only-1", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("run 1 failed: %d", rec1.Code)
	}

	// Run 2: AISIX enabled, returns same ID (no difference)
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"cpa-model-1"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfgEnabled := &Config{
		CPABaseURL:         cpaServer.URL,
		AISIXModelsURL:    aisixServer.URL + "/v1/models",
		HTTPConcurrency:    8,
		ChannelTimeout:     2 * time.Second,
		OverallDeadline:    3 * time.Second,
		BareModelsTakeover: true,
		Channels:           map[string]ChannelConfig{},
		CustomChannels:     map[string]ChannelConfig{},
	}
	pool2 := newHTTPPool(8, 5*time.Second)
	handler2 := testAISIXHandler(cfgEnabled, cpaServer, pool2)
	rec2 := httptest.NewRecorder()
	handler2.ServeHTTP(rec2, httptest.NewRequest("GET", "/v1/models?client_version=v-cpa-only-2", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("run 2 failed: %d", rec2.Code)
	}

	var m1, m2 Manifest
	json.Unmarshal(rec1.Body.Bytes(), &m1)
	json.Unmarshal(rec2.Body.Bytes(), &m2)

	b1, _ := json.Marshal(m1)
	b2, _ := json.Marshal(m2)
	if string(b1) != string(b2) {
		t.Fatalf("expected identical output when supplement has zero difference, got:\nrun1: %s\nrun2: %s", string(b1), string(b2))
	}
}

// Issue 1: slow optional read must not destroy CPA metadata
func TestSlowAISIXDoesNotDestroyCPAMetadata(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		oauthModels: []string{"m1"},
		native: []byte(`{"models": [
			{"slug":"oauth/m1","context_window":1000000},
			{"slug":"oc/deepseek-v4-flash:preview"}
		]}`),
		channelsBody: testChannels,
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	// AISIX takes 250ms, while OverallDeadline is 150ms
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"slow-extra-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := testCfg()
	cfg.CPABaseURL = cpaServer.URL
	cfg.AISIXModelsURL = aisixServer.URL + "/v1/models"
	// AISIXTimeout is unconfigured (defaults to ChannelTimeout / capped by OverallDeadline)
	cfg.OverallDeadline = 150 * time.Millisecond
	cfg.ChannelTimeout = 100 * time.Millisecond

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer reqCancel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models?client_version=v-slow-newapi", nil).WithContext(reqCtx)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	by := map[string]map[string]any{}
	for _, m := range manifest.Models {
		by[asString(m["slug"])] = m
	}

	// CPA channel model MUST still be enriched with ollama_cloud metadata (context_window = 262144)
	// and parsed display_name ("DeepSeek V4 Flash"). A slow AISIX read must not destroy CPA metadata!
	m := by["oc/deepseek-v4-flash:preview"]
	if m == nil {
		t.Fatalf("channel model missing: %v", by)
	}
	if toInt(m["context_window"]) != 262144 {
		t.Fatalf("slow AISIX destroyed CPA metadata! expected context_window 262144, got: %+v", m)
	}
	if m["display_name"] != "DeepSeek V4 Flash" {
		t.Fatalf("slow AISIX destroyed CPA metadata! expected display_name 'DeepSeek V4 Flash', got: %+v", m)
	}
}

// Regression test: HTTPConcurrency=1 pool capacity starvation
func TestAISIXPoolCapacity1DoesNotStarveCPA(t *testing.T) {
	stubSources(t)
	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond) // holds until its deadline
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"slow-extra-model"}]}`))
	}))
	t.Cleanup(aisixServer.Close)

	fake := &fakeCPA{
		oauthModels: []string{"m1"},
		native: []byte(`{"models": [
			{"slug":"oauth/m1","context_window":1000000},
			{"slug":"oc/deepseek-v4-flash:preview"}
		]}`),
		channelsBody: testChannels,
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	cfg := testCfg()
	cfg.CPABaseURL = cpaServer.URL
	cfg.AISIXModelsURL = aisixServer.URL + "/v1/models"
	cfg.HTTPConcurrency = 1 // Sole slot: pool capacity 1!
	cfg.AISIXTimeout = 100 * time.Millisecond
	cfg.OverallDeadline = 150 * time.Millisecond
	cfg.ChannelTimeout = 80 * time.Millisecond

	pool := newHTTPPool(1, 5*time.Second) // Capacity 1
	handler := testAISIXHandler(cfg, cpaServer, pool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models?client_version=v-pool-starve", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	by := map[string]map[string]any{}
	for _, m := range manifest.Models {
		by[asString(m["slug"])] = m
	}

	m := by["oc/deepseek-v4-flash:preview"]
	if m == nil {
		t.Fatalf("channel model missing: %v", by)
	}
	if toInt(m["context_window"]) != 262144 {
		t.Fatalf("pool starvation destroyed CPA metadata! expected context_window 262144, got: %+v", m)
	}
}

// Test that empty or overlapping AISIX results trigger NO extra source calls where CPA needs none.
func TestAISIXNoExtraSourceCallsOnEmptyOrOverlap(t *testing.T) {
	var sourceCalls atomic.Int64
	srcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "api.json"):
			w.Write([]byte(`{"openai":{"models":{"brand-new-model":{"limit":{"context":128000}}}}}`))
		case strings.HasSuffix(r.URL.Path, "models.json"):
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{"models":[]}`))
		}
	}))
	t.Cleanup(srcServer.Close)

	oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = srcServer.URL+"/api.json", srcServer.URL+"/models.json", srcServer.URL+"/mp.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })

	fake := &fakeCPA{
		native: []byte(`{"models":[{"slug":"cpa-base-model"}]}`),
	}
	cpaServer := httptest.NewServer(fake.handler())
	t.Cleanup(cpaServer.Close)

	var aisixBody atomic.Value
	var aisixStatus atomic.Int64
	aisixStatus.Store(200)

	aisixServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := int(aisixStatus.Load())
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(aisixBody.Load().(string)))
	}))
	t.Cleanup(aisixServer.Close)

	cfg := &Config{
		CPABaseURL:           cpaServer.URL,
		AISIXModelsURL:      aisixServer.URL + "/v1/models",
		HTTPConcurrency:      8,
		ChannelTimeout:       2 * time.Second,
		OverallDeadline:      3 * time.Second,
		BareModelsTakeover:   false, // CPA needs NO external sources
		Channels:             map[string]ChannelConfig{},
		CustomChannels:       map[string]ChannelConfig{},
		GlobalSourcePriority: []string{"models.dev/openai"},
	}

	pool := newHTTPPool(8, 5*time.Second)
	handler := testAISIXHandler(cfg, cpaServer, pool)

	// Subtest 1: Valid empty list -> 0 extra source calls
	sourceCalls.Store(0)
	aisixBody.Store(`{"object":"list","data":[]}`)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest("GET", "/v1/models?client_version=v-src-empty", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("subtest 1 failed: %d", rec1.Code)
	}
	if calls := sourceCalls.Load(); calls != 0 {
		t.Fatalf("expected 0 source calls on empty AISIX list, got %d", calls)
	}

	// Subtest 2: Overlapping model -> 0 extra source calls
	sourceCalls.Store(0)
	aisixBody.Store(`{"object":"list","data":[{"id":"cpa-base-model"}]}`)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest("GET", "/v1/models?client_version=v-src-overlap", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("subtest 2 failed: %d", rec2.Code)
	}
	if calls := sourceCalls.Load(); calls != 0 {
		t.Fatalf("expected 0 source calls on overlapping AISIX list, got %d", calls)
	}

	// Subtest 3: Failing AISIX -> 0 extra source calls
	sourceCalls.Store(0)
	aisixStatus.Store(500)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, httptest.NewRequest("GET", "/v1/models?client_version=v-src-failed", nil))
	if rec3.Code != http.StatusOK {
		t.Fatalf("subtest 3 failed: %d", rec3.Code)
	}
	if calls := sourceCalls.Load(); calls != 0 {
		t.Fatalf("expected 0 source calls on failing AISIX, got %d", calls)
	}

	// Subtest 4: Actual extra model -> fetches source and enriches
	sourceCalls.Store(0)
	aisixStatus.Store(200)
	aisixBody.Store(`{"object":"list","data":[{"id":"brand-new-model"}]}`)
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, httptest.NewRequest("GET", "/v1/models?client_version=v-src-extra", nil))
	if rec4.Code != http.StatusOK {
		t.Fatalf("subtest 4 failed: %d", rec4.Code)
	}
	if calls := sourceCalls.Load(); calls == 0 {
		t.Fatalf("expected source calls for actual extra model, got 0")
	}
	var m4 Manifest
	json.Unmarshal(rec4.Body.Bytes(), &m4)
	found := false
	for _, m := range m4.Models {
		if asString(m["slug"]) == "brand-new-model" {
			found = true
			if toInt(m["context_window"]) != 128000 {
				t.Fatalf("expected brand-new-model to be enriched with context_window 128000, got %v", m["context_window"])
			}
		}
	}
	if !found {
		t.Fatalf("expected brand-new-model in manifest")
	}
}

// Issue 2: exact ID bytes preserved (whitespace/case/prefix preserved, blank-only rejected)
func TestAISIXPreservesExactIDBytesAndRejectsBlank(t *testing.T) {
	// 1. Blank-only rejected in validation
	blankPayload := []byte(`{"object":"list","data":[{"id":"   "}]}`)
	if err := validateAISIXModelsResponse(blankPayload); err == nil {
		t.Fatalf("expected error for blank-only model id")
	}

	// 2. Exact bytes preserved: whitespace, case, slashes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "  spaced-model  "},
				{"id": "Vendor/Model-Case:V1"}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	pool := newHTTPPool(8, 5*time.Second)
	client := newAISIXClient(server.URL+"/v1/models", "token", 2*time.Second, pool)
	ids, err := client.FetchModelIDs(t.Context())
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d: %v", len(ids), ids)
	}
	if ids[0] != "  spaced-model  " {
		t.Fatalf("expected exact bytes '  spaced-model  ', but got trimmed/rewritten: %q", ids[0])
	}
	if ids[1] != "Vendor/Model-Case:V1" {
		t.Fatalf("expected exact bytes 'Vendor/Model-Case:V1', got: %q", ids[1])
	}
}
