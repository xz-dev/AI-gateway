package main

import (
	"context"
	"encoding/json"
	"fmt"
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

type forceRefreshFixture struct {
	handler       http.Handler
	owner         *routingSnapshotOwner
	version       atomic.Int32
	nativeStatus  atomic.Int32
	nativeCalls   atomic.Int32
	aisixCalls    atomic.Int32
	discoverCalls atomic.Int32
	channelCalls  atomic.Int32
	sourceCalls   atomic.Int32
	blockNative   atomic.Bool
	nativeEntered chan struct{}
	releaseNative chan struct{}
}

func newForceRefreshFixture(t *testing.T) *forceRefreshFixture {
	t.Helper()
	f := &forceRefreshFixture{nativeEntered: make(chan struct{}, 1), releaseNative: make(chan struct{})}
	f.version.Store(1)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.sourceCalls.Add(1)
		version := f.version.Load()
		switch r.URL.Path {
		case "/api.json":
			fmt.Fprintf(w, `{"p":{"models":{"m%d":{"id":"m%d","name":"source-v%d"}}}}`, version, version, version)
		case "/models.json":
			io.WriteString(w, `{}`)
		default:
			io.WriteString(w, `{"models":[]}`)
		}
	}))
	t.Cleanup(source.Close)
	oldAPI, oldFlat, oldParams := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = source.URL+"/api.json", source.URL+"/models.json", source.URL+"/params.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldAPI, oldFlat, oldParams })

	var cpaURL string
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := f.version.Load()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			f.nativeCalls.Add(1)
			if f.blockNative.Load() {
				select {
				case f.nativeEntered <- struct{}{}:
				default:
				}
				<-f.releaseNative
			}
			if status := f.nativeStatus.Load(); status != 0 {
				w.WriteHeader(int(status))
				return
			}
			fmt.Fprintf(w, `{"models":[{"slug":"p/m%d"}]}`, version)
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/openai-compatibility":
			f.discoverCalls.Add(1)
			fmt.Fprintf(w, `{"openai-compatibility":[{"name":"P","prefix":"p","base-url":"https://provider.invalid/v1","api-key-entries":[{"auth-index":"p"}],"models":[{"name":"m%d"}]}]}`, version)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v0/management/") && r.URL.Path != "/v0/management/auth-files" && r.URL.Path != "/v0/management/oauth-model-alias":
			kind := strings.TrimPrefix(r.URL.Path, "/v0/management/")
			fmt.Fprintf(w, `{"%s":[]}`, kind)
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			io.WriteString(w, `{"files":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/oauth-model-alias":
			io.WriteString(w, `{"oauth-model-alias":{}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			f.channelCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body":        map[string]any{"data": []map[string]any{{"id": fmt.Sprintf("m%d", version)}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	cpaURL = cpa.URL
	_ = cpaURL
	t.Cleanup(cpa.Close)

	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.aisixCalls.Add(1)
		io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	t.Cleanup(aisix.Close)

	pool := cachedTestPool(t)
	cfg := &Config{
		CPABaseURL: cpa.URL, HTTPConcurrency: 8,
		ChannelTimeout: time.Second, OverallDeadline: 2 * time.Second,
		AISIXModelsURL: aisix.URL, AISIXTimeout: time.Second,
		Channels:       map[string]ChannelConfig{"p": {SourcePriority: []string{"models.dev/p"}}},
		CustomChannels: map[string]ChannelConfig{},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := newCPAClient(cpa.URL, "management", "client", pool, log)
	owner := newRoutingSnapshotOwner(client, newAISIXClient(aisix.URL, "", time.Second, pool), time.Hour, 2*time.Second, log)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.owner = owner
	f.handler = handleModels(cfg, client, newAISIXClient(aisix.URL, "", time.Second, pool), pool, log, owner)
	catalogCacheTTL.Store(int64(time.Minute))
	catalogCacheMaxBytes.Store(64 << 20)
	t.Cleanup(func() {
		catalogCacheTTL.Store(0)
		catalogCacheMaxBytes.Store(0)
	})
	return f
}

func requestForceRefresh(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestReadCacheForceRefreshDoesNotJoinOrdinaryFlight(t *testing.T) {
	pool := cachedTestPool(t)
	ordinaryEntered := make(chan struct{})
	releaseOrdinary := make(chan struct{})
	var ordinaryCalls atomic.Int32
	var forcedCalls atomic.Int32
	ordinaryFetch := func() ([]byte, error) {
		ordinaryCalls.Add(1)
		close(ordinaryEntered)
		<-releaseOrdinary
		return []byte(`{"source":"ordinary"}`), nil
	}
	forcedFetch := func() ([]byte, error) {
		forcedCalls.Add(1)
		return []byte(`{"source":"forced"}`), nil
	}
	ordinaryDone := make(chan struct{})
	go func() {
		defer close(ordinaryDone)
		_, _ = pool.cache.load(context.Background(), "same-key", ordinaryFetch)
	}()
	<-ordinaryEntered
	body, err := pool.cache.load(withReadCacheBypass(context.Background()), "same-key", forcedFetch)
	if err != nil || string(body) != `{"source":"forced"}` || forcedCalls.Load() != 1 {
		t.Fatalf("forced read joined ordinary flight: body=%s calls=%d err=%v", body, forcedCalls.Load(), err)
	}
	close(releaseOrdinary)
	<-ordinaryDone
	if ordinaryCalls.Load() != 1 {
		t.Fatalf("ordinary calls = %d, want 1", ordinaryCalls.Load())
	}
}

func TestModelsTableForceRefreshMethodsAndFullChain(t *testing.T) {
	f := newForceRefreshFixture(t)
	first := requestForceRefresh(f.handler, http.MethodGet, "/models-table")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `method="post"`) || !strings.Contains(first.Body.String(), `action="/models-table/refresh"`) || strings.Contains(first.Body.String(), "刷新成功") {
		t.Fatalf("normal table response lacks read-only refresh form: HTTP %d %.300s", first.Code, first.Body.String())
	}
	baseline := []int32{f.nativeCalls.Load(), f.aisixCalls.Load(), f.discoverCalls.Load(), f.channelCalls.Load(), f.sourceCalls.Load()}
	second := requestForceRefresh(f.handler, http.MethodGet, "/models-table")
	if second.Code != http.StatusOK {
		t.Fatal(second.Code)
	}
	if got := []int32{f.nativeCalls.Load(), f.aisixCalls.Load(), f.discoverCalls.Load(), f.channelCalls.Load(), f.sourceCalls.Load()}; fmt.Sprint(got) != fmt.Sprint(baseline) {
		t.Fatalf("normal GET forced collection: before=%v after=%v", baseline, got)
	}
	if got := requestForceRefresh(f.handler, http.MethodGet, "/models-table/refresh"); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET refresh status = %d, want 405", got.Code)
	}

	f.version.Store(2)
	refreshed := requestForceRefresh(f.handler, http.MethodPost, "/models-table/refresh")
	if refreshed.Code != http.StatusOK || !strings.Contains(refreshed.Body.String(), "刷新成功") || !strings.Contains(refreshed.Body.String(), "p/m2") || !strings.Contains(refreshed.Body.String(), "source-v2") {
		t.Fatalf("forced refresh did not render fresh table: HTTP %d %.500s", refreshed.Code, refreshed.Body.String())
	}
	for i, after := range []int32{f.nativeCalls.Load(), f.aisixCalls.Load(), f.discoverCalls.Load(), f.channelCalls.Load(), f.sourceCalls.Load()} {
		if after <= baseline[i] {
			t.Fatalf("force refresh did not bypass cache %d: before=%d after=%d", i, baseline[i], after)
		}
	}
	if strings.Contains(refreshed.Body.String(), "<script") {
		t.Fatal("refresh page requires JavaScript")
	}
}

func TestModelsTableForceRefreshCoalesces(t *testing.T) {
	f := newForceRefreshFixture(t)
	if got := requestForceRefresh(f.handler, http.MethodGet, "/models-table"); got.Code != http.StatusOK {
		t.Fatal(got.Code)
	}
	before := f.nativeCalls.Load()
	f.blockNative.Store(true)
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- requestForceRefresh(f.handler, http.MethodPost, "/models-table/refresh")
		}()
	}
	select {
	case <-f.nativeEntered:
	case <-time.After(time.Second):
		t.Fatal("forced refresh did not enter native collection")
	}
	time.Sleep(20 * time.Millisecond)
	close(f.releaseNative)
	wg.Wait()
	close(responses)
	for response := range responses {
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "刷新成功") {
			t.Fatalf("coalesced response: HTTP %d %.200s", response.Code, response.Body.String())
		}
	}
	if calls := f.nativeCalls.Load() - before; calls != 1 {
		t.Fatalf("concurrent refresh native calls = %d, want 1", calls)
	}
}

func TestModelsTableForceRefreshWaiterCancellationDoesNotCancelSharedWork(t *testing.T) {
	f := newForceRefreshFixture(t)
	if got := requestForceRefresh(f.handler, http.MethodGet, "/models-table"); got.Code != http.StatusOK {
		t.Fatal(got.Code)
	}
	before := f.nativeCalls.Load()
	f.blockNative.Store(true)

	leaderDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { leaderDone <- requestForceRefresh(f.handler, http.MethodPost, "/models-table/refresh") }()
	select {
	case <-f.nativeEntered:
	case <-time.After(time.Second):
		t.Fatal("forced refresh did not enter native collection")
	}

	ctx, cancel := context.WithCancel(context.Background())
	waiting := httptest.NewRequest(http.MethodPost, "/models-table/refresh", nil).WithContext(ctx)
	waiterDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		f.handler.ServeHTTP(recorder, waiting)
		waiterDone <- recorder
	}()
	cancel()
	select {
	case recorder := <-waiterDone:
		if recorder.Body.Len() != 0 {
			t.Fatalf("canceled waiter received a body: %.100s", recorder.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter remained blocked")
	}
	close(f.releaseNative)
	leader := <-leaderDone
	if leader.Code != http.StatusOK || !strings.Contains(leader.Body.String(), "刷新成功") {
		t.Fatalf("leader failed after waiter cancellation: HTTP %d %.200s", leader.Code, leader.Body.String())
	}
	if calls := f.nativeCalls.Load() - before; calls != 1 {
		t.Fatalf("waiter cancellation changed shared work count: %d", calls)
	}
}

func TestModelsTableForceRefreshHTTPAcceptance(t *testing.T) {
	f := newForceRefreshFixture(t)
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)
	client := server.Client()
	do := func(method string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+map[bool]string{true: "/models-table/refresh", false: "/models-table"}[method == http.MethodPost], nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(body)
	}

	before := f.nativeCalls.Load()
	status, body := do(http.MethodGet)
	if status != http.StatusOK || !strings.Contains(body, "p/m1") || f.nativeCalls.Load() != before {
		t.Fatalf("read-only GET: status=%d native=%d→%d body=%.200s", status, before, f.nativeCalls.Load(), body)
	}
	f.version.Store(2)
	status, body = do(http.MethodPost)
	if status != http.StatusOK || !strings.Contains(body, "刷新成功") || !strings.Contains(body, "p/m2") || f.nativeCalls.Load() <= before {
		t.Fatalf("fresh POST: status=%d native=%d→%d body=%.300s", status, before, f.nativeCalls.Load(), body)
	}

	before = f.nativeCalls.Load()
	f.blockNative.Store(true)
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			status, _ := do(http.MethodPost)
			statuses <- status
		}()
	}
	select {
	case <-f.nativeEntered:
	case <-time.After(time.Second):
		t.Fatal("concurrent HTTP refresh did not start")
	}
	close(f.releaseNative)
	for range 2 {
		if status := <-statuses; status != http.StatusOK {
			t.Fatalf("concurrent HTTP refresh status=%d", status)
		}
	}
	if calls := f.nativeCalls.Load() - before; calls != 1 {
		t.Fatalf("concurrent HTTP refresh native calls=%d", calls)
	}

	f.blockNative.Store(false)
	f.nativeStatus.Store(http.StatusBadGateway)
	status, body = do(http.MethodPost)
	if status != http.StatusOK || !strings.Contains(body, "刷新失败") || !strings.Contains(body, "p/m2") {
		t.Fatalf("HTTP last-good fallback: status=%d body=%.300s", status, body)
	}
	t.Logf("GET=%d fresh_POST=200 concurrent_POST=200/200 failed_POST=%d native_calls=%d", http.StatusOK, status, f.nativeCalls.Load())
}

func TestModelsTableForceRefreshLastGoodFallback(t *testing.T) {
	f := newForceRefreshFixture(t)
	good := requestForceRefresh(f.handler, http.MethodGet, "/models-table")
	if good.Code != http.StatusOK || !strings.Contains(good.Body.String(), "p/m1") {
		t.Fatal("failed to establish last-good table")
	}
	f.nativeStatus.Store(http.StatusBadGateway)
	failed := requestForceRefresh(f.handler, http.MethodPost, "/models-table/refresh")
	if failed.Code != http.StatusOK || !strings.Contains(failed.Body.String(), "刷新失败") || !strings.Contains(failed.Body.String(), "p/m1") || strings.Contains(failed.Body.String(), "刷新成功") {
		t.Fatalf("failed refresh did not preserve last-good table: HTTP %d %.500s", failed.Code, failed.Body.String())
	}

	fresh := newForceRefreshFixture(t)
	fresh.nativeStatus.Store(http.StatusBadGateway)
	failed = requestForceRefresh(fresh.handler, http.MethodPost, "/models-table/refresh")
	if failed.Code < 400 || strings.Contains(failed.Body.String(), "<table") {
		t.Fatalf("failure without last-good table = HTTP %d %.300s", failed.Code, failed.Body.String())
	}
}
