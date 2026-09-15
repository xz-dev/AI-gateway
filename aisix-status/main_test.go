package main

import (
	"context"
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
		"Combos",
		`http-equiv="refresh" content="10"`,
		"refreshes every 10s",
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
	backupRowStart := strings.Index(body, "<code>direct-backup</code>")
	if backupRowStart < 0 {
		t.Fatal("direct backup row missing")
	}
	backupRowEnd := strings.Index(body[backupRowStart:], "</tr>")
	if backupRowEnd < 0 {
		t.Fatal("direct backup row is incomplete")
	}
	backupRow := body[backupRowStart : backupRowStart+backupRowEnd]
	if !strings.Contains(backupRow, "combo&lt;&amp;&gt;<br>dynamic-combo") {
		t.Errorf("direct backup row missing sorted combo memberships: %s", backupRow)
	}
	for _, forbidden := range []string{testAdminKey, "provider-key-must-not-leak", "backup-key-must-not-leak", "gpt-primary", "<script", admin.URL, "Eligibility is not a health proof", "not independently health-checked", "PRIVATE MANAGEMENT VIEW", "Configured declaration order is shown", "Recent actual outcomes", "refreshes every 5s"} {
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

func TestFallbackCountersAndWindow(t *testing.T) {
	metrics := `
# TYPE aisix_routing_successful_fallbacks_total counter
aisix_routing_successful_fallbacks_total{model="combo",fallback_model="target-b"} 7
aisix_routing_failed_fallbacks_total{model="combo",fallback_model="target-c"} 2
aisix_routing_successful_fallbacks_total{model="other",fallback_model="target-z"} 1
other_metric{model="combo",fallback_model="ignored"} 99
`
	counters := parseFallbackCounters(metrics)
	if got := counters[fallbackMetricKey{Model: "combo", Target: "target-b", Outcome: "success"}]; got != 7 {
		t.Fatalf("successful fallback count = %d, want 7", got)
	}
	if got := counters[fallbackMetricKey{Model: "combo", Target: "target-c", Outcome: "failed"}]; got != 2 {
		t.Fatalf("failed fallback count = %d, want 2", got)
	}
	if len(counters) != 3 {
		t.Fatalf("parsed %d counters, want 3", len(counters))
	}

	start := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	tracker := &fallbackTracker{
		previous: make(map[fallbackMetricKey]uint64),
		recent:   make(map[fallbackMetricKey]fallbackEvent),
		window:   time.Minute,
	}
	tracker.observe(counters, start)
	if events := tracker.recentForModel("combo", start); len(events) != 0 {
		t.Fatalf("initial baseline produced %d fallback events", len(events))
	}

	counters[fallbackMetricKey{Model: "combo", Target: "target-b", Outcome: "success"}] = 8
	counters[fallbackMetricKey{Model: "combo", Target: "target-c", Outcome: "failed"}] = 0
	tracker.observe(counters, start.Add(10*time.Second))
	events := tracker.recentForModel("combo", start.Add(10*time.Second))
	if len(events) != 1 || events[0].Target != "target-b" || events[0].Outcome != "success" {
		t.Fatalf("unexpected recent events: %#v", events)
	}
	if detail := fallbackDetail(events, start.Add(28*time.Second)); detail != "success · 18s ago" {
		t.Fatalf("fallback detail = %q", detail)
	}
	if events := tracker.recentForModel("combo", start.Add(71*time.Second)); len(events) != 0 {
		t.Fatalf("expired event remained visible: %#v", events)
	}

	resetKey := keyFor("combo", "target-b", "success")
	tracker.observe(map[fallbackMetricKey]uint64{resetKey: 0}, start.Add(80*time.Second))
	if events := tracker.recentForModel("combo", start.Add(80*time.Second)); len(events) != 0 {
		t.Fatalf("counter reset produced fallback events: %#v", events)
	}
	tracker.observe(map[fallbackMetricKey]uint64{resetKey: 1}, start.Add(90*time.Second))
	if events := tracker.recentForModel("combo", start.Add(90*time.Second)); len(events) != 1 {
		t.Fatalf("post-reset increment produced %d fallback events", len(events))
	}

	tracker.observe(map[fallbackMetricKey]uint64{}, start.Add(151*time.Second))
	tracker.observe(map[fallbackMetricKey]uint64{resetKey: 1}, start.Add(152*time.Second))
	if events := tracker.recentForModel("combo", start.Add(152*time.Second)); len(events) != 1 {
		t.Fatalf("reappearing nonzero series produced %d fallback events", len(events))
	}
}

func keyFor(model, target, outcome string) fallbackMetricKey {
	return fallbackMetricKey{Model: model, Target: target, Outcome: outcome}
}

func TestStatusPageShowsRecentFallbackOnCombo(t *testing.T) {
	fixed := time.Date(2026, 9, 15, 8, 0, 30, 0, time.UTC)
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/admin/v1/models":
			_, _ = w.Write([]byte(`[
				{"id":"direct-a","value":{"display_name":"target-a"}},
				{"id":"routing-combo","value":{"display_name":"combo","routing":{"strategy":"failover","targets":[{"model":"target-a"}]}}}
			]`))
		case "/admin/v1/models/status":
			_, _ = w.Write([]byte(`[
				{"id":"direct-a","display_name":"target-a","kind":"direct","status":"healthy"},
				{"id":"routing-combo","display_name":"combo","kind":"routing","status":"not_applicable"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()

	handler, err := newStatusHandler(admin.URL, writeTestConfig(t), func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	key := fallbackMetricKey{Model: "combo", Target: "target-a", Outcome: "success"}
	handler.fallbacks.observe(map[fallbackMetricKey]uint64{key: 2}, fixed.Add(-20*time.Second))
	handler.fallbacks.observe(map[fallbackMetricKey]uint64{key: 3}, fixed.Add(-10*time.Second))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	body := w.Body.String()
	targetStart := strings.Index(body, "<strong>target-a</strong>")
	if targetStart < 0 {
		t.Fatal("combo target row missing")
	}
	targetEnd := strings.Index(body[targetStart:], "</li>")
	if targetEnd < 0 {
		t.Fatal("combo target row is incomplete")
	}
	targetRow := body[targetStart : targetStart+targetEnd]
	for _, want := range []string{`class="pill eligible">eligible`, `class="pill fallback">fallback`, "success · 10s ago"} {
		if !strings.Contains(targetRow, want) {
			t.Errorf("target row missing %q: %s", want, targetRow)
		}
	}
	headEnd := strings.Index(body, "Runtime first candidate")
	if headEnd < 0 {
		t.Fatal("combo header missing")
	}
	if strings.Contains(body[:headEnd], `class="pill fallback">fallback`) {
		t.Error("fallback pill remained on combo header")
	}
	if strings.Contains(body, "Recent fallback:") {
		t.Error("combo-level recent fallback line remained")
	}
}

func TestMetricsFailureDoesNotFailStatusPage(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v1/models", "/admin/v1/models/status":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()
	metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
	}))
	defer metrics.Close()

	handler, err := newStatusHandler(admin.URL, writeTestConfig(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler.metricsURL = metrics.URL
	handler.sampleFallbacksOnce(context.Background())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "AISIX status unavailable") {
		t.Fatalf("metrics failure changed status page: HTTP %d", w.Code)
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
