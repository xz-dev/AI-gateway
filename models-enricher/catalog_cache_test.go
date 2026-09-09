package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 构建结果缓存：APISIX交集Lua每请求都拉original腿，成功结果必须按client_version复用；
// 失败结果不得缓存，保证瞬时故障恢复后前门立即好转。
func TestHandlerCatalogResultCache(t *testing.T) {
	stubSources(t)
	fake := &fakeCPA{
		oauthModels:  []string{"m1"},
		native:       []byte(`{"models": [{"slug": "oauth/m1", "id": "oauth/m1"}]}`),
		channelsBody: testChannels,
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	h := newTestHandler(t, testCfg(), cpa)

	catalogCacheTTL.Store(int64(time.Minute))
	t.Cleanup(func() { catalogCacheTTL.Store(0) })

	get := func(handler http.Handler, cv string) (int, []byte) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models?client_version="+cv, nil))
		return rec.Code, rec.Body.Bytes()
	}

	code1, body1 := get(h, "v-cache")
	code2, body2 := get(h, "v-cache")
	if code1 != 200 || code2 != 200 {
		t.Fatalf("cached catalog statuses: %d, %d", code1, code2)
	}
	if got := fake.nativeCalls.Load(); got != 1 {
		t.Fatalf("native manifest calls = %d, want 1 (successful result cached)", got)
	}
	if !bytes.Equal(body1, body2) {
		t.Fatal("cached catalog body changed between hits")
	}

	// 不同client_version各自独立构建。
	if code, _ := get(h, "v-cache-other"); code != 200 {
		t.Fatal("distinct client_version must build its own catalog")
	}
	if got := fake.nativeCalls.Load(); got != 2 {
		t.Fatalf("native manifest calls = %d, want 2 (one per client_version)", got)
	}

	// 失败结果不缓存：用全新handler（空读缓存）制造native故障，恢复后下一次必须立即成功。
	flaky := newTestHandler(t, testCfg(), cpa)
	fake.nativeStatus = 500
	if code, _ := get(flaky, "v-flaky"); code != 502 {
		t.Fatalf("native failure status = %d, want 502", code)
	}
	fake.nativeStatus = 0
	if code, _ := get(flaky, "v-flaky"); code != 200 {
		t.Fatalf("failed build must not be cached: got %d, want 200 after recovery", code)
	}

	// 禁用缓存（默认）：每次请求都重建。
	catalogCacheTTL.Store(0)
	before := fake.nativeCalls.Load()
	get(h, "v-nocache")
	get(h, "v-nocache")
	if got := fake.nativeCalls.Load() - before; got != 2 {
		t.Fatalf("disabled cache must rebuild per request: rebuilt %d, want 2", got)
	}
}
