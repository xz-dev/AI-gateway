package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cachedTestPool(t *testing.T) *httpPool {
	t.Helper()
	pool := newHTTPPool(2, time.Second)
	var err error
	pool.cache, err = newReadCache(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestReadCacheFreshnessStaleAndDenied(t *testing.T) {
	var status atomic.Int32
	var calls atomic.Int32
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, _ = io.WriteString(w, `{"value":12345678901234567890,"empty":[],"false":false}`)
	}))
	defer server.Close()
	pool := cachedTestPool(t)
	now := time.Now()
	pool.cache.clock = func() time.Time { return now }
	get := func() ([]byte, error) {
		r, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		return pool.readJSON(r, 1024, true, nil)
	}
	original, err := get()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(5*time.Minute - time.Nanosecond)
	if body, err := get(); err != nil || !bytes.Equal(body, original) || calls.Load() != 1 {
		t.Fatalf("fresh cache: calls=%d err=%v", calls.Load(), err)
	}
	status.Store(503)
	for _, age := range []time.Duration{time.Nanosecond, 365 * 24 * time.Hour} {
		now = now.Add(age)
		if body, err := get(); err != nil || !bytes.Equal(body, original) {
			t.Fatalf("stale cache lost exact successful bytes: %v", err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("expired cache must attempt refresh")
	}
	status.Store(403)
	if _, err := get(); err == nil {
		t.Fatal("403 hidden by old cache")
	}
	status.Store(503)
	if _, err := get(); err == nil {
		t.Fatal("denied cache resurrected")
	}
	status.Store(200)
	if _, err := get(); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	// 新进程不读取旧目录，即使同一个父目录仍存在。
	pool.cache, err = newReadCache(filepath.Dir(pool.cache.dir), pool.cache.log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := get(); err != nil || calls.Load() != before+1 {
		t.Fatal("restart reused old cache")
	}
}

func TestReadCacheIdentityAndPrivateFiles(t *testing.T) {
	pool := cachedTestPool(t)
	keys := map[string]bool{}
	for _, spec := range []struct{ url, auth, body string }{
		{"http://cpa/api-call?v=1", "secret-a", `{"url":"https://one/models"}`},
		{"http://cpa/api-call?v=1", "secret-b", `{"url":"https://one/models"}`},
		{"http://cpa/api-call?v=1", "secret-a", `{"url":"https://two/models"}`},
		{"http://cpa/api-call?v=2", "secret-a", `{"url":"https://one/models"}`},
	} {
		r, _ := http.NewRequest(http.MethodPost, spec.url, strings.NewReader(spec.body))
		r.Header.Set("Authorization", spec.auth)
		key, err := readCacheKey(r)
		if err != nil || keys[key] || len(key) != 64 {
			t.Fatalf("identity collision: %v", err)
		}
		keys[key] = true
		_, err = pool.cache.load(context.Background(), key, func() ([]byte, error) { return []byte(`{}`), nil })
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(pool.cache.path(key))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("cache file is not private")
		}
	}
	info, _ := os.Stat(pool.cache.dir)
	if info.Mode().Perm() != 0700 {
		t.Fatal("cache directory is not private")
	}
}

func TestReadCacheCoalescesAndHonorsCancellation(t *testing.T) {
	pool := cachedTestPool(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fetch := func() ([]byte, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return []byte(`{"ok":true}`), nil
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = pool.cache.load(context.Background(), "key", fetch) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.cache.load(ctx, "key", fetch); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := pool.cache.load(context.Background(), "key", fetch); err != nil {
				t.Error(err)
			}
		}()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate fetches: %d", calls.Load())
	}
}

func TestAPICallCacheChecksInnerStatusAndReadOnlyMethods(t *testing.T) {
	var status atomic.Int32
	var calls atomic.Int32
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status.Load() == 200 {
			_, _ = io.WriteString(w, `{"status_code":200,"body":"{\"models\":[]}"}`)
		} else {
			_, _ = io.WriteString(w, `{"status_code":403,"body":"denied"}`)
		}
	}))
	defer server.Close()
	pool := cachedTestPool(t)
	cpa := newCPAClient(server.URL, "management-secret", "client-secret", pool, pool.cache.log)
	call := func(method, endpoint string) error {
		_, _, err := cpa.APICall(context.Background(), Channel{AuthIndex: "one"}, method, endpoint, nil, []byte(`{"name":"model"}`))
		return err
	}
	if err := call("GET", "https://upstream/models"); err != nil {
		t.Fatal(err)
	}
	if err := call("GET", "https://upstream/models"); err != nil || calls.Load() != 1 {
		t.Fatal("api-call read not cached")
	}
	pool.cache.clock = func() time.Time { return time.Now().Add(time.Hour) }
	status.Store(403)
	if err := call("GET", "https://upstream/models"); err == nil {
		t.Fatal("inner 403 treated as success")
	}
	if err := call("GET", "https://upstream/models"); err == nil || calls.Load() != 3 {
		t.Fatal("inner failure cached")
	}
	status.Store(200)
	if err := call("POST", "https://upstream/api/show"); err != nil {
		t.Fatal(err)
	}
	if err := call("POST", "https://upstream/api/show"); err != nil || calls.Load() != 4 {
		t.Fatal("read-only POST not cached")
	}
	if err := call("POST", "https://upstream/change"); err != nil {
		t.Fatal(err)
	}
	if err := call("POST", "https://upstream/change"); err != nil || calls.Load() != 6 {
		t.Fatal("write POST cached")
	}
}
