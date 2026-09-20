package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
		response, err := client.Get("http://127.0.0.1:8090/readyz")
		if err != nil || response.StatusCode != http.StatusOK {
			if response != nil {
				_ = response.Body.Close()
			}
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "validate-config" {
		os.Exit(runValidateConfig(os.Args[2], os.Stdin, os.Stdout, os.Stderr))
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	catalogCacheTTL.Store(int64(5 * time.Minute))
	catalogCacheMaxBytes.Store(64 << 20)
	cfgPath := env("CONFIG_PATH", "/app/config.yaml")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		log.Error("config", "err", err.Error())
		os.Exit(1)
	}
	mgmt := os.Getenv("CPA_MANAGEMENT_KEY")
	clientKey := os.Getenv("CPA_CLIENT_KEY")
	if clientKey == "" {
		clientKey = os.Getenv("CPA_API_KEY")
	}
	if mgmt == "" || clientKey == "" {
		log.Error("missing CPA_MANAGEMENT_KEY or CPA_CLIENT_KEY")
		os.Exit(1)
	}
	pool := newHTTPPool(cfg.HTTPConcurrency, cfg.OverallDeadline+5*time.Second)
	pool.cache, err = newReadCache("", log)
	if err != nil {
		log.Error("read cache initialization failed", "err", err)
		os.Exit(1)
	}
	defer os.RemoveAll(pool.cache.dir)
	cpa := newCPAClient(cfg.CPABaseURL, mgmt, clientKey, pool, log)
	aisix := newAISIXClient(cfg.AISIXModelsURL, cfg.AISIXToken, cfg.AISIXTimeout, pool)
	snapshotCtx, stopSnapshots := context.WithCancel(context.Background())
	defer stopSnapshots()
	snapshots := newRoutingSnapshotOwner(cpa, aisix, readCacheTTL, cfg.OverallDeadline+5*time.Second, log)
	snapshots.start(snapshotCtx)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", snapshots.handleReadiness())
	catalog := handleModels(cfg, cpa, aisix, pool, log, snapshots)
	mux.HandleFunc("/v1/models", catalog)
	mux.HandleFunc("/models-table", catalog)
	mux.HandleFunc("/models-table/refresh", catalog)
	mux.HandleFunc("/routing-index", snapshots.handleRoutingIndex())

	addr := env("LISTEN_ADDR", ":8090")
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Info("listen", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server", "err", err.Error())
			os.Exit(1)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	stopSnapshots()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// catalogCacheTTL：构建结果按原始快照generation、格式和client_version缓存。
// 0=禁用；生产在main()启用。旧generation的结果不会被新请求命中。
var catalogCacheTTL atomic.Int64
var catalogCacheMaxBytes atomic.Int64
var catalogBuildMu sync.Mutex

const catalogCacheMaxEntries = 24

type catalogFormat string

const (
	catalogCodex    catalogFormat = "codex"
	catalogStandard catalogFormat = "standard"
	catalogTable    catalogFormat = "table"
)

var catalogFlights = struct {
	sync.Mutex
	byKey map[string]*flightResult
}{byKey: make(map[string]*flightResult)}

// flightResult 的done通道创建后不再替换；complete与stored仅在catalogFlights锁内访问。
type flightResult struct {
	done     chan struct{}
	complete bool
	status   int
	body     []byte
	stored   time.Time
}

func catalogCacheKey(owner *routingSnapshotOwner, snapshot *routingSnapshot, format catalogFormat, cv string) string {
	return fmt.Sprintf("%d:%d:%s:%s", owner.id, snapshot.generation, format, cv)
}

func existingCatalogFlight(key string) *flightResult {
	catalogFlights.Lock()
	defer catalogFlights.Unlock()
	f := catalogFlights.byKey[key]
	if f == nil || (f.complete && (catalogCacheTTL.Load() <= 0 || time.Since(f.stored) >= time.Duration(catalogCacheTTL.Load()))) {
		return nil
	}
	return f
}

func claimCatalogFlight(key string) (*flightResult, bool) {
	catalogFlights.Lock()
	defer catalogFlights.Unlock()
	if f := catalogFlights.byKey[key]; f != nil {
		if !f.complete || (catalogCacheTTL.Load() > 0 && time.Since(f.stored) < time.Duration(catalogCacheTTL.Load())) {
			return f, false
		}
	}
	f := &flightResult{done: make(chan struct{})}
	catalogFlights.byKey[key] = f
	evictOldestCachedFlightLocked()
	return f, true
}

// cachedCatalogBytesLocked 返回已完成缓存体的总字节数；进行中的构建不计入。
func cachedCatalogBytesLocked() int64 {
	var total int64
	for _, f := range catalogFlights.byKey {
		if f.complete {
			total += int64(len(f.body))
		}
	}
	return total
}

// evictOldestCachedFlightLocked 同时约束缓存条目和总字节数；只驱逐已完成条目。
func evictOldestCachedFlightLocked() {
	for {
		byteLimit := catalogCacheMaxBytes.Load()
		overEntries := len(catalogFlights.byKey) > catalogCacheMaxEntries
		overBytes := byteLimit > 0 && cachedCatalogBytesLocked() > byteLimit
		if !overEntries && !overBytes {
			return
		}
		var oldest string
		var oldestAt time.Time
		for key, f := range catalogFlights.byKey {
			if !f.complete {
				continue
			}
			if oldest == "" || f.stored.Before(oldestAt) {
				oldest, oldestAt = key, f.stored
			}
		}
		if oldest == "" {
			return
		}
		delete(catalogFlights.byKey, oldest)
	}
}

type standardModel struct {
	ID     string `json:"id"`
	Object string `json:"object"`
}

type standardModelsResponse struct {
	Object string          `json:"object"`
	Data   []standardModel `json:"data"`
}

func standardModelsProjection(body []byte) ([]byte, error) {
	var manifest Manifest
	if err := decodeJSON(body, &manifest); err != nil {
		return nil, err
	}
	response := standardModelsResponse{Object: "list", Data: make([]standardModel, 0, len(manifest.Models))}
	for _, model := range manifest.Models {
		if id := exactManifestModelID(model); id != "" {
			response.Data = append(response.Data, standardModel{ID: id, Object: "model"})
		}
	}
	return json.Marshal(response)
}

type refreshResult struct {
	done   chan struct{}
	status int
	body   []byte
	at     time.Time
}

type modelsHandler struct {
	cfg           *Config
	cpa           *CPAClient
	pool          *httpPool
	log           *slog.Logger
	owner         *routingSnapshotOwner
	refreshOnMiss bool

	refreshMu sync.Mutex
	refresh   *refreshResult
	lastGood  atomic.Pointer[renderedModelsTable]
}

func newModelsHandler(cfg *Config, cpa *CPAClient, aisix *AISIXClient, pool *httpPool, log *slog.Logger, owners ...*routingSnapshotOwner) *modelsHandler {
	h := &modelsHandler{cfg: cfg, cpa: cpa, pool: pool, log: log, refreshOnMiss: len(owners) == 0 || owners[0] == nil}
	if h.refreshOnMiss {
		h.owner = newRoutingSnapshotOwner(cpa, aisix, readCacheTTL, cfg.ChannelTimeout, log)
	} else {
		h.owner = owners[0]
	}
	return h
}

// handleModels：标准OpenAI目录、Codex目录与HTML表格共用同一原始快照。
func handleModels(cfg *Config, cpa *CPAClient, aisix *AISIXClient, pool *httpPool, log *slog.Logger, owners ...*routingSnapshotOwner) http.HandlerFunc {
	return newModelsHandler(cfg, cpa, aisix, pool, log, owners...).ServeHTTP
}

func (h *modelsHandler) write(w http.ResponseWriter, table bool, status int, body []byte) {
	if table {
		renderedAt := time.Now()
		status, html := renderModelsTable(status, body, renderedAt, tableNotice{})
		if status == http.StatusOK {
			h.lastGood.Store(&renderedModelsTable{body: append([]byte(nil), html...), at: renderedAt})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(html)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (h *modelsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/models-table/refresh" {
		h.handleForceRefresh(w, r)
		return
	}
	table := r.URL.Path == "/models-table"
	if r.Method != http.MethodGet {
		status, body := errorJSON(http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		h.write(w, table, status, body)
		return
	}

	format := catalogCodex
	cv := r.URL.Query().Get("client_version")
	if table {
		format, cv = catalogTable, tableInventoryClientVersion
	} else if cv == "" {
		format, cv = catalogStandard, cpaCatalogClientVersion
	}
	serve := func(f *flightResult) bool {
		if f == nil {
			return false
		}
		select {
		case <-f.done:
		case <-r.Context().Done():
			return true
		}
		h.write(w, table, f.status, f.body)
		return true
	}

	wasCold := h.owner.current().generation == 0
	if wasCold {
		if err := h.owner.ensureInitialized(r.Context()); err != nil {
			h.log.Warn("routing snapshot initialization incomplete", "err", err)
		}
	}
	snapshot := h.owner.current()
	key := catalogCacheKey(h.owner, snapshot, format, cv)
	if serve(existingCatalogFlight(key)) {
		return
	}
	if h.refreshOnMiss && !wasCold {
		if err := h.owner.refresh(r.Context()); err != nil {
			h.log.Warn("routing snapshot request refresh incomplete", "err", err)
		}
		snapshot = h.owner.current()
		key = catalogCacheKey(h.owner, snapshot, format, cv)
		if serve(existingCatalogFlight(key)) {
			return
		}
	}

	f, leader := claimCatalogFlight(key)
	if !leader {
		serve(f)
		return
	}
	status, body := h.build(r, cv, snapshot, format)
	f.status, f.body = status, body
	close(f.done)
	catalogFlights.Lock()
	if catalogFlights.byKey[key] == f {
		if status >= 200 && status < 300 && catalogCacheTTL.Load() > 0 {
			f.stored = time.Now()
			f.complete = true
			evictOldestCachedFlightLocked()
		} else {
			delete(catalogFlights.byKey, key)
		}
	}
	catalogFlights.Unlock()
	h.write(w, table, status, body)
}

func (h *modelsHandler) build(r *http.Request, cv string, snapshot *routingSnapshot, format catalogFormat) (int, []byte) {
	catalogBuildMu.Lock()
	defer catalogBuildMu.Unlock()
	status, body := buildCatalog(r, h.cfg, h.cpa, h.pool, h.log, cv, snapshot)
	if status >= 200 && status < 300 && format == catalogStandard {
		projected, err := standardModelsProjection(body)
		if err != nil {
			return errorJSON(http.StatusInternalServerError, "standard_models_encode_failed", err.Error())
		}
		body = projected
	}
	return status, body
}

func (h *modelsHandler) forceRefresh(r *http.Request) (int, []byte) {
	ctx, cancel := context.WithTimeout(withReadCacheBypass(context.Background()), h.cfg.OverallDeadline+5*time.Second)
	defer cancel()
	if err := h.owner.forceRefresh(ctx); err != nil {
		return errorJSON(http.StatusBadGateway, "forced_refresh_failed", err.Error())
	}
	clone := r.Clone(ctx)
	return h.build(clone, tableInventoryClientVersion, h.owner.current(), catalogTable)
}

func (h *modelsHandler) handleForceRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		status, body := errorJSON(http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		h.write(w, true, status, body)
		return
	}
	h.refreshMu.Lock()
	f := h.refresh
	if f == nil {
		f = &refreshResult{done: make(chan struct{})}
		h.refresh = f
		go func() {
			f.status, f.body = h.forceRefresh(r)
			f.at = time.Now()
			close(f.done)
			h.refreshMu.Lock()
			h.refresh = nil
			h.refreshMu.Unlock()
		}()
	}
	h.refreshMu.Unlock()
	select {
	case <-f.done:
	case <-r.Context().Done():
		return
	}
	attemptedAt := f.at
	if f.status == http.StatusOK {
		_, clean := renderModelsTable(f.status, f.body, attemptedAt, tableNotice{})
		status, html := renderModelsTable(f.status, f.body, attemptedAt, tableNotice{Class: "success", Text: "刷新成功 · " + attemptedAt.UTC().Format(time.RFC3339)})
		h.lastGood.Store(&renderedModelsTable{body: clean, at: attemptedAt})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(html)
		return
	}
	if last := h.lastGood.Load(); last != nil {
		warning := tableNotice{Class: "warning", Text: fmt.Sprintf("刷新失败 · %s · 显示最后可用表格（%s）", attemptedAt.UTC().Format(time.RFC3339), last.at.UTC().Format(time.RFC3339))}
		html := injectTableNotice(last.body, warning)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(html)
		return
	}
	h.write(w, true, f.status, f.body)
}

func injectTableNotice(body []byte, notice tableNotice) []byte {
	marker := []byte(`<div id="err"></div>`)
	banner := []byte(fmt.Sprintf(`<div id="notice" class="%s">%s</div>`, template.HTMLEscapeString(notice.Class), template.HTMLEscapeString(notice.Text)))
	if bytes.Contains(body, marker) {
		return bytes.Replace(body, marker, append(banner, marker...), 1)
	}
	return body
}

// buildCatalog 执行完整合成管线，返回 HTTP 状态与响应体（供单飞共享）。
func buildCatalog(r *http.Request, cfg *Config, cpa *CPAClient, pool *httpPool, log *slog.Logger, cv string, snapshot *routingSnapshot) (int, []byte) {
	ctx, cancel := context.WithTimeout(r.Context(), cfg.OverallDeadline+5*time.Second)
	defer cancel()

	// --- Phase 1: Mandatory CPA Phase ---
	// 目录构建固定使用请求开始时的原始快照；CPA不可用时整体fail-closed。
	if snapshot == nil || snapshot.cpaAvailability == sourceFailed || snapshot.cpaManifest == nil {
		return errorJSON(http.StatusBadGateway, "native_manifest_failed", "CPA native manifest unavailable")
	}
	base := snapshot.cpaManifest
	originalCPAIDs := snapshot.cpaIDs

	channels, err := cpa.Discover(ctx)
	if err != nil {
		log.Warn("discover failed", "err", err.Error())
	}
	if cerr := validateRuntime(cfg, channels, base); cerr != nil {
		ce := cerr.(*configError)
		log.Warn("runtime config validation failed", "code", ce.code, "err", ce.Error())
		return errorJSON(http.StatusInternalServerError, ce.code, ce.Error())
	}

	identities := cpa.catalogIdentities(ctx, channels, err)
	base, fallbackCount := identities.filter(base, cfg)
	if fallbackCount > 0 {
		log.Warn("catalog identity unavailable; preserving CPA records", "count", fallbackCount)
	}

	// 先确定实际成员与启用步骤，再并发读取所需来源；关闭的步骤不是失败。
	fetched := fetchChannelModels(ctx, cpa, cfg, channels, cv, log)
	cpaNeeded := requiredSources(cfg, base, fetched)
	tables := fetchSources(ctx, pool, cpaNeeded, log)

	ollama := map[string]map[string]sourceHit{}
	var owg sync.WaitGroup
	var omu sync.Mutex
	for i, pack := range fetched {
		chCfg := cfg.Channels[pack.Channel.Prefix]
		if pack.Failed || tables.channelFailed(chCfg, pack.Channel.Prefix, modelsForEnrichment(base, pack)) {
			fetched[i].Failed = true
			continue
		}
		if !channelUsesOllama(chCfg) {
			continue
		}
		owg.Add(1)
		go func(i int, pack channelModels) {
			defer owg.Done()
			hits, err := fetchOllamaForChannel(ctx, cpa, cfg, pack.Channel, modelsForEnrichment(base, pack), log)
			omu.Lock()
			defer omu.Unlock()
			if err != nil {
				fetched[i].Failed = true
			} else if len(hits) > 0 {
				ollama[pack.Channel.Prefix] = hits
			}
		}(i, pack)
	}
	owg.Wait()
	for _, pack := range fetched {
		if pack.Failed {
			log.Warn("channel enrichment skipped; using CPA baseline", "prefix", pack.Channel.Prefix)
		}
	}

	// --- Phase 2: Optional AISIX Phase ---
	// AISIX失败不破坏成功的CPA目录；只从同一代原始快照计算增量。
	var supplementalIDs []string
	if snapshot.aisixAvailability != sourceFailed {
		supplementalIDs = computeSupplementalIDs(snapshot.aisixOrderedIDs, originalCPAIDs)
	}

	// --- Phase 3: Enrich only the actual difference ---
	// 仅当存在实际增量且依赖尚未获取的来源时，才拉取额外源表。
	if len(supplementalIDs) > 0 {
		extraNeeded := requiredSupplementalSources(cfg, supplementalIDs, cpaNeeded)
		if len(extraNeeded) > 0 {
			extraTables := fetchSources(ctx, pool, extraNeeded, log)
			tables.merge(extraTables)
		}
	}

	// --- Phase 4: Merge Manifest ---
	manifest := mergeManifest(base, fetched, cfg, tables, ollama, supplementalIDs)
	body, err := json.Marshal(manifest)
	if err != nil {
		return errorJSON(http.StatusInternalServerError, "manifest_encode_failed", err.Error())
	}
	return http.StatusOK, body
}

func errorJSON(status int, code, msg string) (int, []byte) {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
	return status, body
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
