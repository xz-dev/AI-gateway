package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	oauthModels     []string // 显式原名夹具；账号同时注册原名和oauth/限定名。
	channelsBody    []byte   // openai-compatibility kind 响应
	showBodies      atomic.Value
}

func (f *fakeCPA) handler() http.Handler {
	kinds := []string{
		"openai-compatibility", "claude-api-key", "codex-api-key",
		"gemini-api-key", "xai-api-key", "interactions-api-key", "vertex-api-key",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0/management/auth-files", func(w http.ResponseWriter, r *http.Request) {
		if len(f.oauthModels) == 0 {
			w.Write([]byte(`{"files":[]}`))
			return
		}
		w.Write([]byte(`{"files":[{"name":"fixture.json","provider":"codex"}]}`))
	})
	mux.HandleFunc("GET /v0/management/auth-files/models", func(w http.ResponseWriter, r *http.Request) {
		models := []map[string]string{}
		for _, id := range f.oauthModels {
			models = append(models, map[string]string{"id": id}, map[string]string{"id": "oauth/" + id})
		}
		json.NewEncoder(w).Encode(map[string]any{"models": models})
	})
	mux.HandleFunc("GET /v0/management/model-definitions/codex", func(w http.ResponseWriter, r *http.Request) {
		models := []map[string]string{}
		for _, id := range f.oauthModels {
			models = append(models, map[string]string{"id": id})
		}
		json.NewEncoder(w).Encode(map[string]any{"models": models})
	})
	mux.HandleFunc("GET /v0/management/oauth-model-alias", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"oauth-model-alias":{}}`))
	})
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
	{"name":"Ollama Cloud","prefix":"oc","base-url":"https://ollama.com/v1","api-key-entries":[{"auth-index":"k1"}],"models":[{"name":"deepseek-v4-flash:preview"}]}
]}`)

func TestHandlerPinsCPACatalogVersion(t *testing.T) {
	const legacy = `{"models":[{"slug":"oauth/extended","supported_reasoning_levels":[{"effort":"high"}]},{"slug":"oauth/standard","supported_reasoning_levels":[{"effort":"high"}]}]}`
	const full = `{"models":[{"slug":"oauth/extended","supported_reasoning_levels":[{"effort":"high"},{"effort":"max"},{"effort":"ultra"}]},{"slug":"oauth/standard","supported_reasoning_levels":[{"effort":"high"}]}]}`
	fake := &fakeCPA{
		native: []byte(legacy), nativeByVersion: map[string][]byte{"1": []byte(full)}, oauthModels: []string{"extended", "standard"},
		channelsBody: []byte(`{"openai-compatibility":[{"prefix":"c","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"outside-native"}]}]}`),
	}
	upstreamVersions := make(chan string, 5)
	backend := fake.handler()
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Header.Get("Authorization") != "Bearer c" {
			t.Error("native catalog lost its client credential")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v0/management/api-call" {
			var payload struct {
				URL string `json:"url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			u, err := url.Parse(payload.URL)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			upstreamVersions <- u.Query().Get("client_version")
			io.WriteString(w, `{"status_code":200,"body":{"data":[{"id":"outside-native"}]}}`)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	defer cpa.Close()
	cfg := testCfg()
	cfg.Channels = map[string]ChannelConfig{"c": {}}
	h := newTestHandler(t, cfg, cpa)
	for _, version := range []string{"v0.65.0", "v0.144.0", "arbitrary", "1"} {
		t.Run(version, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/models?client_version="+version, nil))
			if w.Code != 200 {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			assertMetadataJSON(t, json.RawMessage(w.Body.Bytes()), full)
			if got := <-upstreamVersions; got != version {
				t.Fatalf("upstream inventory version = %q, want caller version %q", got, version)
			}
		})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/models-table?client_version=ignored", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "client_version=1") {
		t.Fatal("table did not use the fixed CPA catalog version")
	}
	if got := <-upstreamVersions; got != "v0.65.0" {
		t.Fatalf("table changed its upstream inventory version: %q", got)
	}
}

// 输出字段通过真实适配/合并路径到达HTTP目录，不在兄弟字段间互填。
func TestHandlerOutputTokenFields(t *testing.T) {
	cases := []struct {
		name, fields, want string
		overrides          map[string]any
	}{
		{"distinct", `"max_tokens":4096,"max_completion_tokens":16384,"max_output_tokens":8192`, `"max_tokens":4096,"max_completion_tokens":16384,"max_output_tokens":8192`, nil},
		{"legacy-only", `"max_tokens":128000`, `"max_tokens":128000`, nil},
		{"same-key-null", `"max_tokens":4096`, `"max_tokens":4096,"max_output_tokens":8192`, map[string]any{"max_tokens": nil, "max_output_tokens": 8192}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubSources(t)
			fake := (&fakeCPA{
				native:       []byte(`{"models":[{"slug":"c/m"}]}`),
				channelsBody: []byte(`{"openai-compatibility":[{"prefix":"c","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"m"}]}]}`),
			}).handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v0/management/api-call" {
					io.WriteString(w, `{"status_code":200,"body":{"data":[{"id":"m",`+tc.fields+`}]}}`)
					return
				}
				fake.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			cfg := testCfg()
			cfg.Channels = map[string]ChannelConfig{"c": {Models: map[string]ModelConfig{"m": {Overrides: tc.overrides}}}}
			rec := httptest.NewRecorder()
			newTestHandler(t, cfg, server).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=output-fields", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("catalog status %d: %s", rec.Code, rec.Body.String())
			}
			var got Manifest
			if err := decodeJSON(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			assertMetadataJSON(t, got.Models, `[{"slug":"c/m","id":"c/m",`+tc.want+`}]`)
		})
	}
}

// OAuth父项精确保留；static子项仅继承已声明的字段。日志供隔离页面验收使用。
func TestHandlerOutputTokenPassthroughAndInheritance(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		oauthModels: []string{"z", "a", "b", "c"},
		native: []byte(`{"models":[
			{"slug":"oauth/z","display_name":"<img src=x onerror=\"window.executed=true\">","input_modalities":["text","image"],"output_modalities":["text"],"context_window":null,"max_tokens":4096,"max_completion_tokens":16384,"max_output_tokens":8192},
			{"slug":"oauth/a","display_name":"","output_modalities":[],"max_input_tokens":0,"max_tokens":128000},
			{"slug":"oauth/b","max_tokens":null,"max_completion_tokens":0,"n":9007199254740993},
			{"slug":"oauth/c","max_tokens":false,"max_completion_tokens":"","max_output_tokens":[]}
		]}`),
	}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	cfg := testCfg()
	cfg.Channels = nil
	cfg.StaticModels = []map[string]any{{"slug": "static-a", "inherit": []any{"oauth/a"}}}
	rec := httptest.NewRecorder()
	newTestHandler(t, cfg, server).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version=output-fields", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog status %d: %s", rec.Code, rec.Body.String())
	}
	var got, native Manifest
	if err := decodeJSON(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := decodeJSON(fake.native, &native); err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 5 {
		t.Fatalf("membership changed: %+v", got.Models)
	}
	parents, _ := json.Marshal(native.Models)
	assertMetadataJSON(t, got.Models[:4], string(parents))
	assertMetadataJSON(t, got.Models[4], `{"slug":"static-a","display_name":"","output_modalities":[],"max_input_tokens":0,"max_tokens":128000}`)
	t.Logf("output-token-handler-fixture: %s", rec.Body.String())
}

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
		oauthModels:  []string{"m1"},
		native:       []byte(`{"models": [{"slug":"oauth/m1","context_window":1000000},{"slug":"oc/deepseek-v4-flash:preview"}]}`),
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
	// models.dev 的明确 output 映射只填补 max_output_tokens（lookup_ids 改写命中）。
	if m["max_output_tokens"] != 8192.0 {
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
		oauthModels: []string{"m1"},
		native:      []byte(`{"models": [{"slug":"oauth/m1"}]}`),
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
		oauthModels: []string{"current"},
		native:      []byte(`invalid-json`),
		nativeByVersion: map[string][]byte{
			"1": []byte(`{"models": [{"slug": "oauth/current"}]}`),
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
	// 非CPA渠道请求仍可能依赖调用方版本，所以完整构建保持分组；CPA基线统一。
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("fixed CPA catalog differed between caller versions")
	}
}

func TestHandlerConcurrentBuildsCoalesce(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		oauthModels:  []string{"m1"},
		native:       []byte(`{"models": [{"slug": "oauth/m1", "id": "oauth/m1"}]}`),
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
