package main

import (
	"context"
	"log/slog"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func testCfg(upstream string) *Config {
	return &Config{
		Upstream: upstream,
		Aliases: map[string][]string{
			"fast-model": []string{"t1/fast", "t2/fast"},
		},
		StickinessHeader: "Session-Id",
		MaxFrameBytes:    16 << 20,
		DialTimeout:      5 * time.Second,
	}
}

func TestParseCreate(t *testing.T) {
	p := &proxy{cfg: testCfg("http://x")}
	if _, ok := parseCreate([]byte(`{"type":"response.create","model":"fast-model"}`)); !ok {
		t.Fatal("create not parsed")
	}
	if _, ok := parseCreate([]byte(`{"type":"response.append","model":"fast-model"}`)); ok {
		t.Fatal("append misclassified")
	}
	if _, ok := parseCreate([]byte(`not json`)); ok {
		t.Fatal("garbage classified")
	}
	if _, isAlias := p.cfg.Aliases["fast-model"]; !isAlias {
		t.Fatal("alias missing")
	}
	if _, isAlias := p.cfg.Aliases["other"]; isAlias {
		t.Fatal("non-alias misclassified")
	}
}

func TestStickyPick(t *testing.T) {
	p := &proxy{cfg: testCfg("http://x")}
	cs := &connState{sessionID: "s1", targets: map[string]int{}}
	a := p.pickTarget(cs, "fast-model")
	for i := 0; i < 10; i++ {
		if p.pickTarget(cs, "fast-model") != a {
			t.Fatal("sticky broken")
		}
	}
	cs2 := &connState{targets: map[string]int{}}
	p.pickTarget(cs2, "fast-model")
	if p.pickTarget(cs2, "fast-model") != cs2.targets["fast-model"] {
		t.Fatal("rr sticky broken")
	}
}

func TestRewriteBody(t *testing.T) {
	out := rewriteBody([]byte(`{"type":"response.create","model":"fast-model"}`), "t1/fast")
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["model"] != "t1/fast" || m["type"] != "response.create" || m["stream"] != true {
		t.Fatalf("bad rewrite: %v", m)
	}
}

func TestIsDeltaEvent(t *testing.T) {
	if !isDeltaEvent("response.output_text.delta") || isDeltaEvent("response.created") {
		t.Fatal("delta misclassified")
	}
}

// fakeCPA：HTTP /v1/responses SSE（model 含 dead → 429）+ WS 透传回显
func fakeCPA(t *testing.T, gotModel *atomic.Value) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "" {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
			if err != nil {
				return
			}
			defer c.Close(websocket.StatusNormalClosure, "bye")
			for {
				mt, data, err := c.Read(r.Context())
				if err != nil {
					return
				}
				c.Write(r.Context(), mt, data) // 回显
			}
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		model, _ := m["model"].(string)
		gotModel.Store(model)
		if strings.Contains(model, "dead") {
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"PO\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\"}}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	})
	return httptest.NewServer(mux)
}

func wsClient(t *testing.T, url string) *websocket.Conn {
	c, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEndToEndManagedAlias(t *testing.T) {
	var gotModel atomic.Value
	up := fakeCPA(t, &gotModel)
	defer up.Close()
	p := &proxy{cfg: testCfg(up.URL), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	srv := httptest.NewServer(p)
	defer srv.Close()

	c := wsClient(t, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/responses")
	defer c.Close(websocket.StatusNormalClosure, "")
	ctx := context.Background()
	c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create","model":"fast-model"}`))

	var types []string
	for i := 0; i < 3; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ev map[string]any
		json.Unmarshal(data, &ev)
		types = append(types, ev["type"].(string))
		if _, ok := ev["sequence_number"]; !ok {
			t.Fatalf("missing sequence_number in %s", data)
		}
	}
	if gotModel.Load() != "t1/fast" && gotModel.Load() != "t2/fast" {
		t.Fatalf("model not rewritten, got %v", gotModel.Load())
	}
	if types[0] != "response.created" || types[2] != "response.completed" {
		t.Fatalf("bad event flow: %v", types)
	}
}

func TestEndToEndManagedReplay(t *testing.T) {
	var gotModel atomic.Value
	up := fakeCPA(t, &gotModel)
	defer up.Close()
	cfg := testCfg(up.URL)
	cfg.Aliases["fast-model"] = []string{"dead/fast", "t2/fast"}
	p := &proxy{cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	srv := httptest.NewServer(p)
	defer srv.Close()

	c := wsClient(t, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/responses")
	defer c.Close(websocket.StatusNormalClosure, "")
	ctx := context.Background()
	c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create","model":"fast-model"}`))

	// 应无错误帧，直接得到 t2 的流
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ := eventTypeOf(data); typ != "response.created" {
		t.Fatalf("expected created from second target, got %s (%s)", typ, data)
	}
	if gotModel.Load() != "t2/fast" {
		t.Fatalf("replay target wrong: %v", gotModel.Load())
	}
}

func TestEndToEndPoolExhausted(t *testing.T) {
	var gotModel atomic.Value
	up := fakeCPA(t, &gotModel)
	defer up.Close()
	cfg := testCfg(up.URL)
	cfg.Aliases["fast-model"] = []string{"dead/a", "dead/b"}
	p := &proxy{cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	srv := httptest.NewServer(p)
	defer srv.Close()

	c := wsClient(t, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/responses")
	defer c.Close(websocket.StatusNormalClosure, "")
	c.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.create","model":"fast-model"}`))
	_, data, err := c.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if eventTypeOf(data) != "error" || !strings.Contains(string(data), "pool_exhausted") {
		t.Fatalf("expected pool_exhausted error, got %s", data)
	}
}

func TestEndToEndPassthrough(t *testing.T) {
	var gotModel atomic.Value
	up := fakeCPA(t, &gotModel)
	defer up.Close()
	p := &proxy{cfg: testCfg(up.URL), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	srv := httptest.NewServer(p)
	defer srv.Close()

	c := wsClient(t, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/responses")
	defer c.Close(websocket.StatusNormalClosure, "")
	ctx := context.Background()
	c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create","model":"other-model"}`))
	_, data, err := c.Read(ctx) // 回显
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "other-model") {
		t.Fatalf("passthrough echo wrong: %s", data)
	}
}
