package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeCPA 模拟 CPA 的 native manifest、management kinds 与 api-call。
// api-call 按 url 后缀分发；/api/show 记录收到的 data body 供断言。
type fakeCPA struct {
	nativeStatus    int
	native          []byte
	nativeDelay     time.Duration
	nativeCalls     atomic.Int64
	nativeByVersion map[string][]byte
	channelsBody    []byte // openai-compatibility kind 响应
	showBodies      atomic.Value
}

func (f *fakeCPA) handler() http.Handler {
	kinds := []string{
		"openai-compatibility", "claude-api-key", "codex-api-key",
		"gemini-api-key", "xai-api-key", "interactions-api-key", "vertex-api-key",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		f.nativeCalls.Add(1)
		if f.nativeDelay > 0 {
			time.Sleep(f.nativeDelay)
		}
		if f.nativeStatus != 0 && f.nativeStatus != 200 {
			w.WriteHeader(f.nativeStatus)
			return
		}
		body := f.native
		if versionBody, ok := f.nativeByVersion[r.URL.Query().Get("client_version")]; ok {
			body = versionBody
		}
		w.Write(body)
	})
	mux.HandleFunc("POST /v0/management/api-call", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		url, _ := payload["url"].(string)
		if strings.HasSuffix(url, "/api/show") {
			data, _ := payload["data"].(string)
			f.showBodies.Store(data)
			json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body":        map[string]any{"model_info": map[string]any{"deepseek.context_length": 262144}},
			})
			return
		}
		// GET models
		json.NewEncoder(w).Encode(map[string]any{
			"status_code": 200,
			"body":        map[string]any{"data": []map[string]any{{"id": "deepseek-v4-flash:preview", "display_name": "DeepSeek V4 Flash"}}},
		})
	})
	for _, kind := range kinds {
		kind := kind
		mux.HandleFunc("GET /v0/management/"+kind, func(w http.ResponseWriter, r *http.Request) {
			if kind == "openai-compatibility" && f.channelsBody != nil {
				w.Write(f.channelsBody)
				return
			}
			w.Write([]byte(`{"` + kind + `": []}`))
		})
	}
	return mux
}

func stubSources(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "api.json"):
			w.Write([]byte(`{"deepseek": {"models": {"deepseek-v4-flash": {"id":"deepseek-v4-flash","limit":{"context":128000,"output":8192}}}}}`))
		case strings.HasSuffix(r.URL.Path, "models.json"):
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{"models": []}`))
		}
	}))
	t.Cleanup(srv.Close)
	oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = srv.URL+"/api.json", srv.URL+"/models.json", srv.URL+"/mp.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })
}

func newTestHandler(t *testing.T, cfg *Config, cpa *httptest.Server) http.Handler {
	t.Helper()
	pool := newHTTPPool(8, 5*time.Second)
	return handleModels(cfg, newCPAClient(cpa.URL, "m", "c", pool, testLog()), pool, testLog())
}

func testCfg() *Config {
	return &Config{
		HTTPConcurrency: 8,
		ChannelTimeout:  2 * time.Second,
		OverallDeadline: 3 * time.Second,
		Channels: map[string]ChannelConfig{
			"oc": {
				SourcePriority:   []string{"ollama_cloud", "models.dev/deepseek"},
				OllamaNativeBase: "https://ollama.com",
			},
		},
		CustomChannels: map[string]ChannelConfig{},
	}
}

var testChannels = []byte(`{"openai-compatibility": [
	{"name":"Ollama Cloud","prefix":"oc","base-url":"https://ollama.com/v1","api-key-entries":[{"auth-index":"k1"}]}
]}`)

func TestHandlerFailClosedOnNativeFailure(t *testing.T) {
	stubSources(t)
	cpa := httptest.NewServer((&fakeCPA{nativeStatus: 503, channelsBody: testChannels}).handler())
	t.Cleanup(cpa.Close)
	h := newTestHandler(t, testCfg(), cpa)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v1", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("native failure must be 502 fail-closed, got %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"].(map[string]any)["code"] != "native_manifest_failed" {
		t.Fatalf("error code: %v", body)
	}
}

func TestHandlerRejectsMissingClientVersion(t *testing.T) {
	h := newTestHandler(t, testCfg(), httptest.NewServer((&fakeCPA{}).handler()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing client_version must be 400, got %d", rec.Code)
	}
}

func TestHandlerConflictOnEmptyChannelPrefix(t *testing.T) {
	stubSources(t)
	channels := []byte(`{"openai-compatibility": [
		{"name":"NoPrefix","base-url":"https://x/v1","api-key-entries":[{"auth-index":"k"}]}
	]}`)
	cpa := httptest.NewServer((&fakeCPA{native: []byte(`{"models": []}`), channelsBody: channels}).handler())
	t.Cleanup(cpa.Close)
	h := newTestHandler(t, testCfg(), cpa)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v1", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("empty channel prefix must fail validation, got %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"].(map[string]any)["code"] != "catalog_configuration_conflict" {
		t.Fatalf("code: %v", body)
	}
}

func TestHandlerHappyPath(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native:       []byte(`{"models": [{"slug":"oauth/m1","context_window":1000000}]}`),
		channelsBody: testChannels,
	}
	cpa := httptest.NewServer(fake.handler())
	t.Cleanup(cpa.Close)
	cfg := testCfg()
	oc := cfg.Channels["oc"]
	oc.Models = map[string]ModelConfig{
		"deepseek-v4-flash:preview": {LookupIDs: map[string]string{"models.dev/deepseek": "deepseek-v4-flash"}},
	}
	cfg.Channels["oc"] = oc
	h := newTestHandler(t, cfg, cpa)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("happy path must be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	by := map[string]map[string]any{}
	for _, m := range manifest.Models {
		by[asString(m["slug"])] = m
	}
	// OAuth 原样保留
	if by["oauth/m1"]["context_window"] != 1000000.0 {
		t.Fatalf("native passthrough: %+v", by["oauth/m1"])
	}
	m := by["oc/deepseek-v4-flash:preview"]
	if m == nil {
		t.Fatalf("channel model missing: %v", by)
	}
	// ollama_cloud 链首命中 → ctx=262144（压过 models.dev 的 128000）
	if m["context_window"] != 262144.0 {
		t.Fatalf("ollama chain-first context: %+v", m)
	}
	// models.dev 填补 max_tokens 空缺（lookup_ids 改写命中）
	if m["max_tokens"] != 8192.0 {
		t.Fatalf("dev gap-fill: %+v", m)
	}
	// parsed display_name 优先
	if m["display_name"] != "DeepSeek V4 Flash" {
		t.Fatalf("parsed display_name must win: %+v", m)
	}
	// /api/show 必须经 api-call data 字段携带 {"model":"deepseek-v4-flash:preview"}
	got, _ := fake.showBodies.Load().(string)
	if !strings.Contains(got, `"model":"deepseek-v4-flash:preview"`) {
		t.Fatalf("api/show data body: %q", got)
	}
}

func TestHandlerChannelFailureFailOpen(t *testing.T) {
	stubSources(t)
	// 渠道 fetch 失败（api-call 500）时整体仍 200，仅含 native
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug":"oauth/m1"}]}`),
		channelsBody: []byte(`{"openai-compatibility": [
			{"name":"Ollama Cloud","prefix":"oc","base-url":"","api-key-entries":[{"auth-index":"k1"}]}
		]}`),
	}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "api-call") {
			w.WriteHeader(500)
			return
		}
		fake.handler().ServeHTTP(w, r)
	}))
	t.Cleanup(cpa.Close)
	h := newTestHandler(t, testCfg(), cpa)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("channel failure must fail-open with 200, got %d", rec.Code)
	}
	var manifest Manifest
	json.Unmarshal(rec.Body.Bytes(), &manifest)
	if len(manifest.Models) != 1 || manifest.Models[0]["slug"] != "oauth/m1" {
		t.Fatalf("only native models expected: %+v", manifest.Models)
	}
}

// TestHandlerConcurrentBuildsCoalesce：并发 MISS 必须单飞合并——
// 6 个并发请求共享 1 次真实构建（native 恰好调用 1 次），响应体完全一致。
// 回归防线：并发全量构建曾把 enricher 的 128m cgroup 打到 OOM。
func TestHandlerConcurrentBuildsAreVersionScoped(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native: []byte(`{"models": [{"slug": "oauth/default"}]}`),
		nativeByVersion: map[string][]byte{
			"v0.65.0": []byte(`{"models": [{"slug": "oauth/old"}]}`),
			"v0.66.0": []byte(`{"models": [{"slug": "oauth/new"}]}`),
		},
		nativeDelay:  150 * time.Millisecond,
		channelsBody: testChannels,
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	h := newTestHandler(t, testCfg(), cpa)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	bodies := make([][]byte, 2)
	versions := []string{"v0.65.0", "v0.66.0"}
	for i, version := range versions {
		wg.Add(1)
		go func(i int, version string) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version="+version, nil))
			codes[i] = rec.Code
			bodies[i] = rec.Body.Bytes()
		}(i, version)
	}
	wg.Wait()

	if got := fake.nativeCalls.Load(); got != 2 {
		t.Fatalf("native manifest calls = %d, want 2 (one per client_version)", got)
	}
	for i, code := range codes {
		if code != 200 {
			t.Fatalf("request %d: status %d, want 200", i, code)
		}
	}
	if bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("different client_versions shared the same catalog body")
	}
}

func TestHandlerConcurrentBuildsCoalesce(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		native:       []byte(`{"models": [{"id": "oauth/m1"}]}`),
		nativeDelay:  150 * time.Millisecond,
		channelsBody: testChannels,
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	h := newTestHandler(t, testCfg(), cpa)

	const n = 6
	var wg sync.WaitGroup
	codes := make([]int, n)
	bodies := make([][]byte, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=v0.65.0", nil))
			codes[i] = rec.Code
			bodies[i] = rec.Body.Bytes()
		}(i)
	}
	wg.Wait()

	if got := fake.nativeCalls.Load(); got != 1 {
		t.Fatalf("native manifest calls = %d, want exactly 1 (singleflight)", got)
	}
	for i := 0; i < n; i++ {
		if codes[i] != 200 {
			t.Fatalf("request %d: status %d, want 200", i, codes[i])
		}
		if !bytes.Equal(bodies[0], bodies[i]) {
			t.Fatalf("request %d: body differs from leader", i)
		}
	}
}
