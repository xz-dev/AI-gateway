package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
	cpa := newCPAClient(cfg.CPABaseURL, mgmt, clientKey, pool, log)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/models", handleModels(cfg, cpa, pool, log))

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

// catalogFlight：并发 MISS 单飞合并。同一时刻只允许一个全量构建在飞，
// 后续请求等待并共享其结果——内存与上游压力都有界（并发 6 个构建曾触发 128m cgroup OOM）。
var catalogFlights = struct {
	sync.Mutex
	byVersion map[string]*flightResult
}{byVersion: make(map[string]*flightResult)}

type flightResult struct {
	done   chan struct{}
	status int
	body   []byte
}

// handleModels：GET /v1/models?client_version=... 的合成管线。
// 失败语义：native manifest 失败 → 502 fail-closed；运行时配置/身份问题 → 500 JSON；
// 单渠道/单源失败 → WARN + fail-open。
func handleModels(cfg *Config, cpa *CPAClient, pool *httpPool, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		cv := r.URL.Query().Get("client_version")
		if cv == "" {
			writeJSONError(w, http.StatusBadRequest, "client_version_required", "client_version required")
			return
		}

		catalogFlights.Lock()
		if f := catalogFlights.byVersion[cv]; f != nil {
			catalogFlights.Unlock()
			select {
			case <-f.done:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(f.status)
			_, _ = w.Write(f.body)
			return
		}
		f := &flightResult{done: make(chan struct{})}
		catalogFlights.byVersion[cv] = f
		catalogFlights.Unlock()
		defer func() {
			catalogFlights.Lock()
			if catalogFlights.byVersion[cv] == f {
				delete(catalogFlights.byVersion, cv)
			}
			catalogFlights.Unlock()
		}()

		status, body := buildCatalog(r, cfg, cpa, pool, log, cv)
		f.status, f.body = status, body
		close(f.done)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

// buildCatalog 执行完整合成管线，返回 HTTP 状态与响应体（供单飞共享）。
func buildCatalog(r *http.Request, cfg *Config, cpa *CPAClient, pool *httpPool, log *slog.Logger, cv string) (int, []byte) {
	ctx, cancel := context.WithTimeout(r.Context(), cfg.OverallDeadline+5*time.Second)
	defer cancel()

	// CPA native manifest 是 OAuth 模型与原始 manifest 的权威基线：
	// 失败即整体 fail-closed，禁止空 Manifest 替代、禁止继续合成。
	base, err := cpa.NativeManifest(ctx, cv)
	if err != nil {
		log.Warn("native manifest failed", "err", err.Error())
		return errorJSON(http.StatusBadGateway, "native_manifest_failed", "CPA native manifest unavailable: "+err.Error())
	}
	channels, err := cpa.Discover(ctx)
	if err != nil {
		log.Warn("discover failed", "err", err.Error())
	}
	if cerr := validateRuntime(cfg, channels, base); cerr != nil {
		ce := cerr.(*configError)
		log.Warn("runtime config validation failed", "code", ce.code, "err", ce.Error())
		return errorJSON(http.StatusInternalServerError, ce.code, ce.Error())
	}

	// bulk sources 与渠道抓取并发；ollama 依赖渠道抓取结果，随后并发。
	var tables *SourceTables
	var swg sync.WaitGroup
	swg.Add(1)
	go func() {
		defer swg.Done()
		tables = fetchSources(ctx, pool, log)
	}()
	fetched := fetchChannelModels(ctx, cpa, cfg, channels, cv, log)
	swg.Wait()

	ollama := map[string]map[string]sourceHit{}
	var owg sync.WaitGroup
	var omu sync.Mutex
	for _, pack := range fetched {
		chCfg := cfg.Channels[pack.Channel.Prefix]
		if !channelUsesOllama(chCfg) {
			continue
		}
		owg.Add(1)
		go func(pack channelModels) {
			defer owg.Done()
			hits := fetchOllamaForChannel(ctx, cpa, cfg, pack.Channel, pack.Models, log)
			if len(hits) > 0 {
				omu.Lock()
				ollama[pack.Channel.Prefix] = hits
				omu.Unlock()
			}
		}(pack)
	}
	owg.Wait()

	manifest := mergeManifest(base, fetched, cfg, tables, ollama)
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
