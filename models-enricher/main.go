package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	cpa := newCPAClient(cfg.CPABaseURL, mgmt, clientKey, cfg.OverallDeadline+5*time.Second, log)
	sources := newSourceCache(cfg.SourceTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cv := r.URL.Query().Get("client_version")
		if cv == "" {
			http.Error(w, "client_version required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), cfg.OverallDeadline+5*time.Second)
		defer cancel()
		base, err := cpa.NativeManifest(ctx, cv)
		if err != nil {
			log.Warn("native manifest failed", "err", err.Error())
			base = &Manifest{}
		}
		channels, err := cpa.Discover(ctx)
		if err != nil {
			log.Warn("discover failed", "err", err.Error())
		}
		fetched := fetchChannelModels(ctx, cpa, cfg, channels, cv, log)
		manifest := mergeManifest(base, fetched, cfg, sources)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(manifest)
	})

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

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
