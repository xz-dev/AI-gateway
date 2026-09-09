package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadCacheSourceIsolationAndInvalidJSON(t *testing.T) {
	var broken atomic.Bool
	var otherCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/one" && broken.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/invalid" {
			fmt.Fprint(w, "not-json")
			return
		}
		if r.URL.Path == "/two" {
			otherCalls.Add(1)
		}
		fmt.Fprintf(w, `{"source":%q}`, r.URL.Path)
	}))
	defer server.Close()
	pool := cachedTestPool(t)
	now := time.Now()
	pool.cache.clock = func() time.Time { return now }
	read := func(path string) ([]byte, error) {
		r, _ := http.NewRequest("GET", server.URL+path, nil)
		return pool.readJSON(r, 1024, true, nil)
	}
	if _, err := read("/one"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Minute)
	if _, err := read("/two"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	broken.Store(true)
	if b, err := read("/one"); err != nil || string(b) != `{"source":"/one"}` {
		t.Fatal("source stale fallback lost")
	}
	if b, err := read("/two"); err != nil || string(b) != `{"source":"/two"}` || otherCalls.Load() != 1 {
		t.Fatal("unrelated source cache affected")
	}
	if _, err := read("/invalid"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if len(pool.cache.entries) != 2 {
		t.Fatal("invalid response cached")
	}
}

func TestReadCacheEvictsAndBoundsFiles(t *testing.T) {
	pool := cachedTestPool(t)
	for i := 0; i < readCacheEntries+1; i++ {
		key := fmt.Sprintf("%064x", i)
		if _, err := pool.cache.load(context.Background(), key, func() ([]byte, error) { return []byte(`{}`), nil }); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(pool.cache.dir)
	if err != nil || len(files) != readCacheEntries {
		t.Fatalf("file count not bounded: %d %v", len(files), err)
	}
	if _, ok := pool.cache.entries[fmt.Sprintf("%064x", 0)]; ok {
		t.Fatal("oldest cache not evicted")
	}
	body := make([]byte, readCacheBytes)
	pool.cache.mu.Lock()
	if err := pool.cache.store("large", body); err != nil {
		pool.cache.mu.Unlock()
		t.Fatal(err)
	}
	if pool.cache.bytes != readCacheBytes || len(pool.cache.entries) != 1 {
		pool.cache.mu.Unlock()
		t.Fatal("byte budget exceeded")
	}
	if err := pool.cache.store("oversized", append(body, 0)); err == nil {
		pool.cache.mu.Unlock()
		t.Fatal("oversized cache accepted")
	}
	pool.cache.mu.Unlock()
}

func TestReadCacheNetworkFailureWithoutPreviousValue(t *testing.T) {
	pool := cachedTestPool(t)
	_, err := pool.cache.load(context.Background(), "missing", func() ([]byte, error) { return nil, io.ErrUnexpectedEOF })
	if err == nil || len(pool.cache.entries) != 0 {
		t.Fatal("cold failure fabricated cache")
	}
}
