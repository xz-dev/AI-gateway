package main

import (
	"context"
	"encoding/json"
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
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	catalogCacheTTL.Store(int64(5 * time.Minute))
	catalogCacheMaxBytes.Store(16 << 20)
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

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	catalog := handleModels(cfg, cpa, aisix, pool, log)
	mux.HandleFunc("/v1/models", catalog)
	mux.HandleFunc("/models-table", catalog)

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// catalogFlight：同版本的重叠构建共享结果，不保留最终目录缓存。
// 不同版本仍可并发构建；这里不是跨版本的全局内存/准入限制。
// catalogCacheTTL：构建结果按client_version缓存的时长（纳秒，atomic）。
// 0=禁用；生产在main()启用。APISIX交集Lua每请求都拉original腿，
// 没有这层缓存时每个前门请求都触发一次全量构建。
var catalogCacheTTL atomic.Int64
var catalogCacheMaxBytes atomic.Int64
var catalogBuildMu sync.Mutex

const catalogCacheMaxEntries = 24

var catalogFlights = struct {
	sync.Mutex
	byVersion map[string]*flightResult
}{byVersion: make(map[string]*flightResult)}

// flightResult：done非nil=构建进行中（singleflight等待）；done为nil=已完成并缓存。
type flightResult struct {
	done   chan struct{}
	status int
	body   []byte
	stored time.Time
}

// cachedCatalogBytesLocked 返回已完成缓存体的总字节数；进行中的构建不计入。
func cachedCatalogBytesLocked() int64 {
	var total int64
	for _, f := range catalogFlights.byVersion {
		if f.done == nil {
			total += int64(len(f.body))
		}
	}
	return total
}

// evictOldestCachedFlightLocked 同时约束缓存条目和总字节数；只驱逐已完成条目。
func evictOldestCachedFlightLocked() {
	for {
		byteLimit := catalogCacheMaxBytes.Load()
		overEntries := len(catalogFlights.byVersion) > catalogCacheMaxEntries
		overBytes := byteLimit > 0 && cachedCatalogBytesLocked() > byteLimit
		if !overEntries && !overBytes {
			return
		}
		var oldest string
		var oldestAt time.Time
		for cv, f := range catalogFlights.byVersion {
			if f.done != nil {
				continue
			}
			if oldest == "" || f.stored.Before(oldestAt) {
				oldest, oldestAt = cv, f.stored
			}
		}
		if oldest == "" {
			return
		}
		delete(catalogFlights.byVersion, oldest)
	}
}

// handleModels：GET /v1/models?client_version=... 的合成管线。
// 失败语义：native无可用结果 → 502；配置/身份冲突 → 500 JSON；
// 已启用的渠道步骤无可用结果 → WARN + 该渠道整批CPA基线，其他渠道继续。
func handleModels(cfg *Config, cpa *CPAClient, aisix *AISIXClient, pool *httpPool, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		table := r.URL.Path == "/models-table"
		write := func(status int, body []byte) {
			if table {
				writeModelsTable(w, status, body)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}
		if r.Method != http.MethodGet {
			if table {
				write(errorJSON(http.StatusMethodNotAllowed, "method_not_allowed", "GET only"))
			} else {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			}
			return
		}
		cv := r.URL.Query().Get("client_version")
		if table {
			cv = tableInventoryClientVersion
		}
		if cv == "" {
			writeJSONError(w, http.StatusBadRequest, "client_version_required", "client_version required")
			return
		}

		catalogFlights.Lock()
		if f := catalogFlights.byVersion[cv]; f != nil {
			if f.done != nil {
				// 同版本构建进行中：合并等待结果。
				catalogFlights.Unlock()
				select {
				case <-f.done:
				case <-r.Context().Done():
					return
				}
				write(f.status, f.body)
				return
			}
			if ttl := catalogCacheTTL.Load(); ttl > 0 && time.Since(f.stored) < time.Duration(ttl) {
				status, body := f.status, f.body
				catalogFlights.Unlock()
				write(status, body)
				return
			}
			// 过期缓存：落到下面重建并替换。
		}
		f := &flightResult{done: make(chan struct{})}
		catalogFlights.byVersion[cv] = f
		evictOldestCachedFlightLocked()
		catalogFlights.Unlock()

		// 不同 client_version 的完整目录构建同样可能很大；串行化构建，避免并发峰值越过容器内存边界。
		catalogBuildMu.Lock()
		status, body := buildCatalog(r, cfg, cpa, aisix, pool, log, cv)
		catalogBuildMu.Unlock()
		f.status, f.body = status, body
		close(f.done)

		catalogFlights.Lock()
		if catalogFlights.byVersion[cv] == f {
			if status >= 200 && status < 300 && catalogCacheTTL.Load() > 0 {
				// 只缓存成功结果；失败保持每次重建，避免把瞬时错误钉死在前门。
				f.stored = time.Now()
				f.done = nil
				evictOldestCachedFlightLocked()
			} else {
				delete(catalogFlights.byVersion, cv)
			}
		}
		catalogFlights.Unlock()
		write(status, body)
	}
}

// buildCatalog 执行完整合成管线，返回 HTTP 状态与响应体（供单飞共享）。
func buildCatalog(r *http.Request, cfg *Config, cpa *CPAClient, aisix *AISIXClient, pool *httpPool, log *slog.Logger, cv string) (int, []byte) {
	ctx, cancel := context.WithTimeout(r.Context(), cfg.OverallDeadline+5*time.Second)
	defer cancel()

	// --- Phase 1: Mandatory CPA Phase ---
	// CPA native manifest 是成员基线；读取层可回退成功旧缓存。
	// 无可用缓存且获取失败时整体 fail-closed，禁止用空 Manifest 替代。
	base, err := cpa.NativeManifest(ctx)
	if err != nil {
		log.Warn("native manifest failed", "err", err.Error())
		return errorJSON(http.StatusBadGateway, "native_manifest_failed", "CPA native manifest unavailable: "+err.Error())
	}

	// 身份过滤前先捕获 CPA 原始模型 ID 全集；用于精确排除 AISIX 重复项。
	originalCPAIDs := extractCPAIDs(base)

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
	// CPA 必需项完全完成后，在调用方派生的有界独立预算内执行可选读取。
	// 绝不抢占 CPA 并发槽，绝不消耗 CPA 的时间预算。
	var supplementalIDs []string
	if aisix != nil {
		apiTimeout := aisix.timeout
		if apiTimeout <= 0 || apiTimeout > cfg.OverallDeadline {
			apiTimeout = cfg.OverallDeadline
		}
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline); remaining < apiTimeout {
				apiTimeout = remaining
			}
		}
		if apiTimeout > 0 {
			apiCtx, apiCancel := context.WithTimeout(ctx, apiTimeout)
			aisixIDs, apiErr := aisix.FetchModelIDs(apiCtx)
			apiCancel()
			if apiErr != nil {
				log.Warn("aisix models supplement unavailable", "err", apiErr.Error())
			} else {
				supplementalIDs = computeSupplementalIDs(aisixIDs, originalCPAIDs)
			}
		}
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
