package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestRoutingHTTPFixture runs only inside the pinned APISIX image. It proves the
// standalone route, Lua selector and real proxy path together without Compose.
func TestRoutingHTTPFixture(t *testing.T) {
	if os.Getenv("ROUTING_HTTP_FIXTURE") != "1" {
		t.Skip("set ROUTING_HTTP_FIXTURE=1 inside the APISIX fixture")
	}
	root := os.Getenv("ROUTING_HTTP_REPO_ROOT")
	if root == "" {
		t.Fatal("ROUTING_HTTP_REPO_ROOT is required")
	}

	serve := func(address string, handler http.Handler) {
		t.Helper()
		listener, err := net.Listen("tcp4", address)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: handler}
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		})
	}

	decisions := map[string]routingDecision{
		"cpa-only":        routingCPA,
		"overlap":         routingCPA,
		"cpa-stream":      routingCPA,
		"cpa-disconnect":  routingCPA,
		"cpa-error":       routingCPA,
		"cpa-rate":        routingCPA,
		"cpa-timeout":     routingCPA,
		"gpt-5.6-sol":     routingAISIX,
		"axis/模型 + exact": routingAISIX,
		"not-found":       routingNotFound,
		"unavailable":     routingUnavailable,
	}
	var indexCalls atomic.Int64
	var indexMu sync.Mutex
	var indexModels []string
	serve("127.0.0.4:8090", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexCalls.Add(1)
		model := r.URL.Query().Get("model")
		indexMu.Lock()
		indexModels = append(indexModels, model)
		indexMu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/routing-index" || model == "lookup-failure" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		decision, ok := decisions[model]
		if !ok {
			decision = routingNotFound
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(routingIndexResponse{Decision: decision, Generation: 7})
	}))

	var cpaCalls atomic.Int64
	var aisixCalls atomic.Int64
	var wsCalls atomic.Int64
	streamRelease := make(chan struct{})
	serve("127.0.0.2:8317", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fixture-ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		cpaCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		var document map[string]any
		_ = json.Unmarshal(body, &document)
		model, _ := document["model"].(string)
		if r.Header.Get("Authorization") != "Bearer fixture-cpa" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("session_id") == "session-exact" {
			w.Header().Set("X-Session-Seen", "session-exact")
		}
		w.Header().Set("X-Backend", "cpa")
		switch model {
		case "cpa-error":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"provider failed"}}`))
		case "cpa-rate":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		case "cpa-timeout":
			time.Sleep(1500 * time.Millisecond)
			_, _ = w.Write([]byte(`{"late":true}`))
		case "cpa-disconnect":
			connection, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			partial := "data: partial\n\n"
			_, _ = fmt.Fprintf(buffer, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n%x\r\n%s\r\n", len(partial), partial)
			_ = buffer.Flush()
			_ = connection.Close()
		case "cpa-stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = w.Write([]byte("data: first\n\n"))
			flusher.Flush()
			<-streamRelease
			_, _ = w.Write([]byte("data: second\n\n"))
			flusher.Flush()
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(body)
		}
	}))
	serve("127.0.0.3:3000", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aisixCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer caller-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Backend", "aisix")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write(body)
	}))
	serve("127.0.0.5:8090", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wsCalls.Add(1)
		w.Header().Set("X-Backend", "wsalias")
		_, _ = w.Write([]byte("legacy-ws"))
	}))

	load := func(path string) map[string]any {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := yaml.Unmarshal(body, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	config := load(filepath.Join(root, "apisix-models/config.yaml"))
	api := config["apisix"].(map[string]any)
	api["node_listen"] = 9080
	api["extra_lua_path"] = filepath.Join(root, "apisix-models/?.lua")

	routesBody, err := os.ReadFile(filepath.Join(root, "apisix-models/apisix.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var standalone map[string]any
	if err := yaml.Unmarshal([]byte(strings.ReplaceAll(string(routesBody), "__CPA_API_KEY__", "fixture-cpa")), &standalone); err != nil {
		t.Fatal(err)
	}
	addresses := map[string]string{
		"cpa":     "127.0.0.2:8317",
		"aisix":   "127.0.0.3:3000",
		"wsalias": "127.0.0.5:8090",
	}
	var upstreams []any
	for _, raw := range standalone["upstreams"].([]any) {
		upstream := raw.(map[string]any)
		id := upstream["id"].(string)
		if address, ok := addresses[id]; ok {
			upstream["nodes"] = map[string]int{address: 1}
			if id == "cpa" {
				upstream["timeout"] = map[string]int{"connect": 1, "send": 1, "read": 1}
			}
			upstreams = append(upstreams, upstream)
		}
	}
	standalone["upstreams"] = upstreams
	for name, value := range map[string]any{"config.yaml": config, "apisix.yaml": standalone} {
		body, err := yaml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("/usr/local/apisix/conf", name), append(body, []byte("\n#END\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	apisix := exec.Command("apisix", "start")
	apisix.Stdout, apisix.Stderr = os.Stdout, os.Stderr
	if err := apisix.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("apisix", "stop").Run()
		_ = apisix.Wait()
	})

	client := &http.Client{Timeout: 5 * time.Second}
	target := "http://127.0.0.1:9080"
	for deadline := time.Now().Add(30 * time.Second); ; {
		response, requestErr := client.Get(target + "/fixture-ready")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("APISIX did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}

	request := func(path, body string) (int, []byte, http.Header) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, target+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer caller-key")
		req.Header.Set("session_id", "session-exact")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, responseBody, response.Header
	}
	assertOneLookup := func(before int64) {
		t.Helper()
		if got := indexCalls.Load(); got != before+1 {
			t.Fatalf("classified request performed %d routing lookups, want one", got-before)
		}
	}

	for _, model := range []string{"cpa-only", "overlap"} {
		body := fmt.Sprintf(`{"model":%q,"input":"exact"}`, model)
		before := indexCalls.Load()
		status, responseBody, headers := request("/v1/responses", body)
		assertOneLookup(before)
		if status != http.StatusCreated || string(responseBody) != body || headers.Get("X-Backend") != "cpa" || headers.Get("X-Session-Seen") != "session-exact" {
			t.Fatalf("CPA HTTP fidelity for %q: status=%d body=%s headers=%v", model, status, responseBody, headers)
		}
	}

	exactBody := `{"model":"axis/模型 + exact","messages":[{"role":"user","content":"字节"}]}`
	before := indexCalls.Load()
	status, responseBody, headers := request("/v1/chat/completions", exactBody)
	assertOneLookup(before)
	if status != http.StatusAccepted || string(responseBody) != exactBody || headers.Get("X-Backend") != "aisix" {
		t.Fatalf("AISIX HTTP fidelity: status=%d body=%s headers=%v", status, responseBody, headers)
	}
	indexMu.Lock()
	lastIndexed := indexModels[len(indexModels)-1]
	indexMu.Unlock()
	if lastIndexed != "axis/模型 + exact" {
		t.Fatalf("routing index changed exact model ID: %q", lastIndexed)
	}
	aliasBody := `{"model":"gpt-5.6-sol","input":"legacy alias stays exact"}`
	before = indexCalls.Load()
	status, responseBody, headers = request("/v1/responses", aliasBody)
	assertOneLookup(before)
	if status != http.StatusAccepted || string(responseBody) != aliasBody || headers.Get("X-Backend") != "aisix" {
		t.Fatalf("legacy HTTP alias was rewritten before classification: status=%d body=%s", status, responseBody)
	}

	backendCalls := cpaCalls.Load() + aisixCalls.Load()
	for _, test := range []struct {
		body string
		want int
		code string
	}{
		{`{"model":"not-found"}`, http.StatusNotFound, "model_not_found"},
		{`{"model":"unavailable"}`, http.StatusServiceUnavailable, "service_unavailable"},
		{`{"model":"lookup-failure"}`, http.StatusServiceUnavailable, "service_unavailable"},
	} {
		before = indexCalls.Load()
		status, responseBody, _ = request("/v1/responses", test.body)
		assertOneLookup(before)
		if status != test.want || !strings.Contains(string(responseBody), `"code":"`+test.code+`"`) {
			t.Fatalf("selector error: status=%d body=%s", status, responseBody)
		}
	}
	if cpaCalls.Load()+aisixCalls.Load() != backendCalls {
		t.Fatal("selector error reached a backend")
	}

	before = indexCalls.Load()
	status, responseBody, _ = request("/v1/responses", "not-json")
	if status != http.StatusBadRequest || !strings.Contains(string(responseBody), `"code":"invalid_request_error"`) || indexCalls.Load() != before {
		t.Fatalf("malformed request was not rejected before lookup: status=%d body=%s", status, responseBody)
	}

	aisixBefore := aisixCalls.Load()
	for _, test := range []struct {
		model string
		want  int
		body  string
	}{
		{"cpa-error", http.StatusBadGateway, `{"error":{"message":"provider failed"}}`},
		{"cpa-rate", http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`},
	} {
		before = indexCalls.Load()
		status, responseBody, _ = request("/v1/responses", fmt.Sprintf(`{"model":%q}`, test.model))
		assertOneLookup(before)
		if status != test.want || string(responseBody) != test.body || aisixCalls.Load() != aisixBefore {
			t.Fatalf("CPA provider error crossed backends or changed: model=%s status=%d body=%s", test.model, status, responseBody)
		}
	}
	before = indexCalls.Load()
	status, _, _ = request("/v1/responses", `{"model":"cpa-timeout"}`)
	assertOneLookup(before)
	if status != http.StatusGatewayTimeout || aisixCalls.Load() != aisixBefore {
		t.Fatalf("CPA timeout crossed backends or changed class: status=%d", status)
	}

	before = indexCalls.Load()
	req, _ := http.NewRequest(http.MethodPost, target+"/v1/responses", strings.NewReader(`{"model":"cpa-disconnect","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer caller-key")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	disconnectedBody, disconnectedErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	assertOneLookup(before)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(disconnectedBody), "data: partial") || strings.Contains(string(disconnectedBody), "[DONE]") || disconnectedErr == nil || aisixCalls.Load() != aisixBefore {
		t.Fatalf("committed CPA stream disconnect crossed backends or lost its client-visible transport error: status=%d body=%q err=%v", response.StatusCode, disconnectedBody, disconnectedErr)
	}

	type streamReadResult struct {
		status int
		body   string
		err    error
	}
	firstEvent := make(chan error, 1)
	streamDone := make(chan streamReadResult, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, target+"/v1/responses", strings.NewReader(`{"model":"cpa-stream","stream":true}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer caller-key")
		response, err := client.Do(req)
		if err != nil {
			firstEvent <- err
			streamDone <- streamReadResult{err: err}
			return
		}
		defer response.Body.Close()
		reader := bufio.NewReader(response.Body)
		firstLine, readErr := reader.ReadString('\n')
		separator, separatorErr := reader.ReadString('\n')
		if readErr != nil {
			err = readErr
		} else if separatorErr != nil {
			err = separatorErr
		} else if firstLine+separator != "data: first\n\n" {
			err = fmt.Errorf("first SSE event = %q", firstLine+separator)
		}
		firstEvent <- err
		remainder, remainderErr := io.ReadAll(reader)
		streamDone <- streamReadResult{status: response.StatusCode, body: firstLine + separator + string(remainder), err: errors.Join(err, remainderErr)}
	}()
	var firstErr error
	select {
	case firstErr = <-firstEvent:
	case <-time.After(time.Second):
		firstErr = errors.New("APISIX buffered the first SSE event")
	}
	close(streamRelease)
	select {
	case result := <-streamDone:
		if firstErr != nil {
			t.Fatal(firstErr)
		}
		if result.status != http.StatusOK || result.err != nil || result.body != "data: first\n\ndata: second\n\n" {
			t.Fatalf("SSE event sequence changed: status=%d body=%q err=%v", result.status, result.body, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("APISIX did not complete the released SSE stream")
	}

	before = indexCalls.Load()
	req, _ = http.NewRequest(http.MethodGet, target+"/v1/responses", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	response, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(responseBody) != "legacy-ws" || wsCalls.Load() != 1 || indexCalls.Load() != before {
		t.Fatalf("legacy WS route entered classifier: status=%d body=%s ws=%d", response.StatusCode, responseBody, wsCalls.Load())
	}

	t.Logf("real APISIX: CPA-first, overlap, AISIX-only, exact ID/body, 404/503, lookup failure, 429/502/timeout, client-visible committed disconnect, complete SSE event order and WS selector exclusion PASS; CPA=%d AISIX=%d index=%d", cpaCalls.Load(), aisixCalls.Load(), indexCalls.Load())
}
