package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAdminKey = "test-admin-secret-must-not-leak"

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "admin:\n  addr: 172.30.68.2:3002\n  admin_keys:\n    - " + testAdminKey + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusPageSSR(t *testing.T) {
	fixed := time.Date(2026, 9, 12, 13, 0, 0, 0, time.UTC)
	until := fixed.Add(45 * time.Second)
	models := `[
		{"id":"direct-primary","revision":1,"value":{"display_name":"primary<script>","provider":"openai","model_name":"gpt-primary","provider_key_id":"provider-key-must-not-leak"}},
		{"id":"direct-backup","revision":1,"value":{"display_name":"backup","provider":"openai","model_name":"gpt-backup","provider_key_id":"backup-key-must-not-leak"}},
		{"id":"direct-offline","revision":1,"value":{"display_name":"offline","provider":"openai","model_name":"gpt-offline","provider_key_id":"offline-key-must-not-leak"}},
		{"id":"direct-late","revision":1,"value":{"display_name":"late","provider":"openai","model_name":"gpt-late","provider_key_id":"late-key-must-not-leak"}},
		{"id":"routing-combo","revision":1,"value":{"display_name":"combo<&>","routing":{"strategy":"failover","max_fallbacks":1,"targets":[{"model":"primary<script>"},{"model":"backup"},{"model":"late"},{"model":"missing-target"}]}}},
		{"id":"routing-dynamic","revision":1,"value":{"display_name":"dynamic-combo","routing":{"strategy":"round_robin","targets":[{"model":"backup"}]}}}
	]`
	statuses := fmt.Sprintf(`[
		{"id":"direct-primary","display_name":"primary<script>","kind":"direct","status":"cooldown","cooldown_until":{"secs_since_epoch":%d,"nanos_since_epoch":%d},"status_reason":"upstream_rate_limited <retry>"},
		{"id":"direct-backup","display_name":"backup","kind":"direct","status":"healthy"},
		{"id":"direct-offline","display_name":"offline","kind":"direct","status":"unhealthy","status_reason":"background_check_failed"},
		{"id":"direct-late","display_name":"late","kind":"direct","status":"healthy"},
		{"id":"routing-combo","display_name":"combo<&>","kind":"routing","status":"not_applicable"},
		{"id":"routing-dynamic","display_name":"dynamic-combo","kind":"routing","status":"not_applicable"}
	]`, until.Unix(), until.Nanosecond())

	var calls atomic.Int32
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer "+testAdminKey {
			t.Errorf("unexpected authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/admin/v1/models":
			_, _ = w.Write([]byte(models))
		case "/admin/v1/models/status":
			_, _ = w.Write([]byte(statuses))
		default:
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()

	handler, err := newStatusHandler(admin.URL, writeTestConfig(t), func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))

	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected server-rendered HTML, got HTTP %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two fixed AISIX reads, got %d", calls.Load())
	}
	body := w.Body.String()
	for _, want := range []string{
		"AISIX routing status",
		"combo&lt;&amp;&gt;",
		"primary&lt;script&gt;",
		"backup",
		"offline",
		"eligible",
		"cooldown",
		"unavailable",
		"45s remaining",
		"upstream_rate_limited &lt;retry&gt;",
		"background_check_failed",
		"Runtime first candidate",
		"request-dependent",
		"missing-target",
		"unresolved",
		"direct-primary",
		"no authoritative retained last-served history",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
	orderedTargets := []string{"#1", "primary&lt;script&gt;", "#2", "backup", "#3", "late", "#4", "missing-target"}
	position := 0
	for _, want := range orderedTargets {
		next := strings.Index(body[position:], want)
		if next < 0 {
			t.Fatalf("configured target order missing %q after byte %d", want, position)
		}
		position += next + len(want)
	}
	for _, forbidden := range []string{testAdminKey, "provider-key-must-not-leak", "backup-key-must-not-leak", "gpt-primary", "<script", admin.URL} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaked forbidden value %q", forbidden)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Error("private no-store/CSP headers missing")
	}
}

func TestStatusListenerRejectsEveryOtherSurface(t *testing.T) {
	var calls atomic.Int32
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer admin.Close()
	handler, err := newStatusHandler(admin.URL, writeTestConfig(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/admin/v1/models", "/admin/openapi-scalar", "/playground/chat/completions", "/metrics", "/v1/models"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/status", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /status = %d Allow=%q", w.Code, w.Header().Get("Allow"))
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected routes reached AISIX %d times", calls.Load())
	}
}

func TestConfigurationFailureDoesNotStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("admin:\n  admin_keys: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newStatusHandler("http://aisix:3002", path, time.Now); err == nil {
		t.Fatal("missing AISIX admin key unexpectedly accepted")
	}
}

func TestStatusFailureIsGenericAndSecretSafe(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backend-secret-must-not-leak", http.StatusBadGateway)
	}))
	defer admin.Close()
	handler, err := newStatusHandler(admin.URL, writeTestConfig(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected generic HTML 503, got HTTP %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, forbidden := range []string{"backend-secret-must-not-leak", testAdminKey, admin.URL} {
		if strings.Contains(body, forbidden) {
			t.Errorf("error page leaked %q", forbidden)
		}
	}
	if !strings.Contains(body, "AISIX status unavailable") {
		t.Error("generic unavailable message missing")
	}
}
