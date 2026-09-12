package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	refreshSeconds = 5
	maxAdminBody   = 1 << 20
)

var version = "dev"

//go:embed status.html
var statusHTML string

var statusTemplate = template.Must(template.New("status").Parse(statusHTML))

type adminConfig struct {
	Admin struct {
		AdminKeys []string `yaml:"admin_keys"`
	} `yaml:"admin"`
}

type modelEntry struct {
	ID    string     `json:"id"`
	Value modelValue `json:"value"`
}

type modelValue struct {
	DisplayName string          `json:"display_name"`
	Routing     *routingConfig  `json:"routing"`
	Ensemble    json.RawMessage `json:"ensemble"`
	Semantic    json.RawMessage `json:"semantic"`
}

type routingConfig struct {
	Strategy           string          `json:"strategy"`
	Targets            []routingTarget `json:"targets"`
	MaxFallbacks       *int            `json:"max_fallbacks"`
	WhenAllUnavailable string          `json:"when_all_unavailable"`
}

type routingTarget struct {
	Model    string   `json:"model"`
	Priority *int     `json:"priority"`
	Weight   *int     `json:"weight"`
	Tags     []string `json:"tags"`
}

type systemTime struct {
	Seconds     int64 `json:"secs_since_epoch"`
	Nanoseconds int64 `json:"nanos_since_epoch"`
}

func (value systemTime) time() time.Time {
	return time.Unix(value.Seconds, value.Nanoseconds).UTC()
}

type runtimeStatus struct {
	ID              string      `json:"id"`
	DisplayName     string      `json:"display_name"`
	Kind            string      `json:"kind"`
	Status          string      `json:"status"`
	CooldownUntil   *systemTime `json:"cooldown_until"`
	StatusReason    string      `json:"status_reason"`
	LastCheckStatus *int        `json:"last_check_status"`
	LastCheckedAt   *systemTime `json:"last_checked_at"`
}

type targetView struct {
	Order, Priority, Weight int
	Name, State, Detail     string
	Tags                    []string
}

type comboView struct {
	Name, Strategy, Budget, Candidate string
	Targets                           []targetView
}

type directView struct {
	ID, Name, State, Detail string
}

type pageData struct {
	Version, Generated, Error string
	Combos                    []comboView
	Direct                    []directView
	Eligible, Excluded        int
	Unresolved                int
}

type statusHandler struct {
	adminURL string
	adminKey string
	client   *http.Client
	now      func() time.Time
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler, err := newStatusHandler(
		env("AISIX_ADMIN_URL", "http://aisix:3002"),
		env("AISIX_CONFIG_PATH", "/etc/aisix/config.yaml"),
		time.Now,
	)
	if err != nil {
		log.Error("configuration failed", "err", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              env("LISTEN_ADDR", ":8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		log.Info("listen", "addr", server.Addr, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func newStatusHandler(adminURL, configPath string, now func() time.Time) (http.Handler, error) {
	parsed, err := url.Parse(adminURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid AISIX admin URL")
	}
	key, err := readAdminKey(configPath)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &statusHandler{
		adminURL: strings.TrimRight(adminURL, "/"),
		adminKey: key,
		client: &http.Client{
			Timeout: 3 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: now,
	}, nil
}

func readAdminKey(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read AISIX config: %w", err)
	}
	var cfg adminConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("parse AISIX config: %w", err)
	}
	for _, key := range cfg.Admin.AdminKeys {
		if key = strings.TrimSpace(key); key != "" {
			return key, nil
		}
	}
	return "", errors.New("AISIX admin key is missing")
}

func (h *statusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/status" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var models []modelEntry
	var statuses []runtimeStatus
	if h.getJSON(r.Context(), "/admin/v1/models", &models) != nil || h.getJSON(r.Context(), "/admin/v1/models/status", &statuses) != nil {
		h.writePage(w, r.Method == http.MethodHead, http.StatusServiceUnavailable, pageData{
			Version: version,
			Error:   "AISIX status unavailable",
		})
		return
	}
	h.writePage(w, r.Method == http.MethodHead, http.StatusOK, buildPage(models, statuses, h.now()))
}

func (h *statusHandler) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.adminURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.adminKey)
	resp, err := h.client.Do(req)
	if err != nil {
		return errors.New("AISIX admin request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("AISIX admin request failed")
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxAdminBody))
	if err := decoder.Decode(dst); err != nil {
		return errors.New("AISIX admin response invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("AISIX admin response invalid")
	}
	return nil
}

func buildPage(models []modelEntry, statuses []runtimeStatus, now time.Time) pageData {
	byID := make(map[string]runtimeStatus, len(statuses))
	byName := make(map[string]runtimeStatus, len(statuses))
	for _, status := range statuses {
		byID[status.ID] = status
		byName[status.DisplayName] = status
	}

	data := pageData{
		Version:   version,
		Generated: now.UTC().Format(time.RFC3339),
	}
	for _, entry := range models {
		name := entry.Value.DisplayName
		if name == "" {
			name = entry.ID
		}
		if entry.Value.Routing != nil {
			data.Combos = append(data.Combos, buildCombo(name, entry.Value.Routing, byName, now))
			continue
		}
		if isVirtual(entry.Value) {
			continue
		}
		status, ok := byID[entry.ID]
		if !ok {
			status, ok = byName[name]
		}
		state, detail := statusDisplay(status, ok, now)
		data.Direct = append(data.Direct, directView{ID: entry.ID, Name: name, State: state, Detail: detail})
		switch state {
		case "eligible":
			data.Eligible++
		case "unresolved":
			data.Unresolved++
		default:
			data.Excluded++
		}
	}
	sort.Slice(data.Combos, func(i, j int) bool { return data.Combos[i].Name < data.Combos[j].Name })
	sort.Slice(data.Direct, func(i, j int) bool { return data.Direct[i].Name < data.Direct[j].Name })
	return data
}

func buildCombo(name string, routing *routingConfig, statuses map[string]runtimeStatus, now time.Time) comboView {
	combo := comboView{
		Name:      name,
		Strategy:  routing.Strategy,
		Budget:    fallbackBudget(routing),
		Candidate: firstCandidate(routing, statuses),
	}
	for index, target := range routing.Targets {
		status, ok := statuses[target.Model]
		state, detail := statusDisplay(status, ok, now)
		combo.Targets = append(combo.Targets, targetView{
			Order:    index + 1,
			Name:     target.Model,
			State:    state,
			Detail:   detail,
			Priority: intOr(target.Priority, 0),
			Weight:   intOr(target.Weight, 1),
			Tags:     target.Tags,
		})
	}
	return combo
}

func firstCandidate(routing *routingConfig, statuses map[string]runtimeStatus) string {
	if routing.Strategy != "failover" {
		return "request-dependent"
	}
	for _, target := range routing.Targets {
		if len(target.Tags) != 0 {
			return "request-dependent"
		}
	}

	targets := append([]routingTarget(nil), routing.Targets...)
	sort.SliceStable(targets, func(i, j int) bool {
		return intOr(targets[i].Priority, 0) > intOr(targets[j].Priority, 0)
	})
	attempts := len(targets)
	if routing.MaxFallbacks != nil {
		attempts = min(attempts, max(0, *routing.MaxFallbacks)+1)
	}
	targets = targets[:attempts]
	for _, target := range targets {
		status, ok := statuses[target.Model]
		if state, _ := statusDisplay(status, ok, time.Time{}); state == "eligible" {
			return target.Model
		}
	}
	if routing.WhenAllUnavailable == "try_anyway" && len(targets) != 0 {
		return targets[0].Model + " (try_anyway; excluded)"
	}
	return "none currently eligible"
}

func fallbackBudget(routing *routingConfig) string {
	if routing.MaxFallbacks == nil {
		return fmt.Sprintf("all %d configured targets", len(routing.Targets))
	}
	fallbacks := max(0, *routing.MaxFallbacks)
	return fmt.Sprintf("%d fallbacks / %d attempts", fallbacks, min(len(routing.Targets), fallbacks+1))
}

func statusDisplay(status runtimeStatus, exists bool, now time.Time) (string, string) {
	if !exists {
		return "unresolved", "target does not resolve to an AISIX runtime status"
	}
	state := "unresolved"
	detail := "runtime state is not applicable or unknown"
	switch status.Status {
	case "healthy":
		state = "eligible"
		detail = "not currently excluded by AISIX; not independently health-checked"
	case "cooldown":
		state = "cooldown"
		detail = "currently excluded by AISIX cooldown"
	case "unhealthy":
		state = "unavailable"
		detail = "currently excluded by AISIX background status"
	}
	parts := []string{detail}
	if status.CooldownUntil != nil {
		until := status.CooldownUntil.time()
		remaining := max(int64(0), until.Sub(now).Milliseconds())
		seconds := (remaining + 999) / 1000
		parts = append(parts, fmt.Sprintf("until %s (%ds remaining)", until.Format(time.RFC3339), seconds))
	}
	if status.StatusReason != "" {
		parts = append(parts, "last reason: "+status.StatusReason)
	}
	if status.LastCheckStatus != nil {
		parts = append(parts, fmt.Sprintf("last check HTTP %d", *status.LastCheckStatus))
	}
	if status.LastCheckedAt != nil {
		parts = append(parts, "checked "+status.LastCheckedAt.time().Format(time.RFC3339))
	}
	return state, strings.Join(parts, " · ")
}

func isVirtual(model modelValue) bool {
	return model.Routing != nil || presentJSON(model.Ensemble) || presentJSON(model.Semantic)
}

func presentJSON(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func (h *statusHandler) writePage(w http.ResponseWriter, head bool, status int, data pageData) {
	var body bytes.Buffer
	if err := statusTemplate.Execute(&body, data); err != nil {
		http.Error(w, "AISIX status unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if !head {
		_, _ = w.Write(body.Bytes())
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
