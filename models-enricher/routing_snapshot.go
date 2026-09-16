package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type sourceAvailability string

const (
	sourceUsable sourceAvailability = "usable"
	sourceEmpty  sourceAvailability = "empty"
	sourceFailed sourceAvailability = "failed"
)

type routingDecision string

const (
	routingCPA         routingDecision = "cpa"
	routingAISIX       routingDecision = "aisix"
	routingNotFound    routingDecision = "not_found"
	routingUnavailable routingDecision = "unavailable"
)

type routingSnapshot struct {
	generation        uint64
	cpaAvailability   sourceAvailability
	aisixAvailability sourceAvailability
	cpaIDs            map[string]bool
	aisixIDs          map[string]bool
	cpaManifest       *Manifest
	aisixOrderedIDs   []string
}

type routingIndexResponse struct {
	Decision   routingDecision `json:"decision"`
	Generation uint64          `json:"generation"`
}

var routingSnapshotOwnerSequence atomic.Uint64

type routingSnapshotOwner struct {
	id              uint64
	cpa             *CPAClient
	aisix           *AISIXClient
	refreshInterval time.Duration
	refreshTimeout  time.Duration
	log             *slog.Logger
	refreshMu       sync.Mutex
	snapshot        atomic.Pointer[routingSnapshot]
}

func newRoutingSnapshotOwner(cpa *CPAClient, aisix *AISIXClient, refreshInterval, refreshTimeout time.Duration, log *slog.Logger) *routingSnapshotOwner {
	owner := &routingSnapshotOwner{
		id:              routingSnapshotOwnerSequence.Add(1),
		cpa:             cpa,
		aisix:           aisix,
		refreshInterval: refreshInterval,
		refreshTimeout:  refreshTimeout,
		log:             log,
	}
	owner.snapshot.Store(&routingSnapshot{
		cpaAvailability:   sourceFailed,
		aisixAvailability: sourceFailed,
		cpaIDs:            map[string]bool{},
		aisixIDs:          map[string]bool{},
	})
	return owner
}

func (o *routingSnapshotOwner) current() *routingSnapshot {
	return o.snapshot.Load()
}

func availability(ids map[string]bool) sourceAvailability {
	if len(ids) == 0 {
		return sourceEmpty
	}
	return sourceUsable
}

func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func sameRoutingSnapshot(a, b *routingSnapshot) bool {
	return a != nil && b != nil &&
		a.cpaAvailability == b.cpaAvailability &&
		a.aisixAvailability == b.aisixAvailability &&
		reflect.DeepEqual(a.cpaIDs, b.cpaIDs) &&
		reflect.DeepEqual(a.aisixIDs, b.aisixIDs) &&
		reflect.DeepEqual(a.cpaManifest, b.cpaManifest) &&
		reflect.DeepEqual(a.aisixOrderedIDs, b.aisixOrderedIDs)
}

// refresh 独立读取两个原始目录并一次性发布，读取失败绝不伪装成空集合。
func (o *routingSnapshotOwner) refresh(parent context.Context) error {
	o.refreshMu.Lock()
	defer o.refreshMu.Unlock()
	return o.refreshLocked(parent)
}

func (o *routingSnapshotOwner) forceRefresh(parent context.Context) error {
	return o.refresh(withReadCacheBypass(parent))
}

func (o *routingSnapshotOwner) ensureInitialized(parent context.Context) error {
	o.refreshMu.Lock()
	defer o.refreshMu.Unlock()
	if o.current().generation != 0 {
		return nil
	}
	return o.refreshLocked(parent)
}

func (o *routingSnapshotOwner) refreshLocked(parent context.Context) error {
	next := &routingSnapshot{
		cpaAvailability:   sourceFailed,
		aisixAvailability: sourceFailed,
		cpaIDs:            map[string]bool{},
		aisixIDs:          map[string]bool{},
	}
	newContext := func() (context.Context, context.CancelFunc) {
		if o.refreshTimeout > 0 {
			return context.WithTimeout(parent, o.refreshTimeout)
		}
		return context.WithCancel(parent)
	}

	var failures []error
	if o.cpa == nil {
		failures = append(failures, errors.New("CPA catalog source is not configured"))
	} else {
		ctx, cancel := newContext()
		manifest, err := o.cpa.NativeManifest(ctx)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("CPA catalog: %w", err))
		} else {
			next.cpaManifest = manifest
			next.cpaIDs = extractCPAIDs(manifest)
			next.cpaAvailability = availability(next.cpaIDs)
		}
	}

	if o.aisix == nil {
		failures = append(failures, errors.New("AISIX catalog source is not configured"))
	} else {
		ctx, cancel := newContext()
		if timeout := o.aisix.timeout; timeout > 0 {
			var apiCancel context.CancelFunc
			ctx, apiCancel = context.WithTimeout(ctx, timeout)
			defer apiCancel()
		}
		ids, err := o.aisix.FetchModelIDs(ctx)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("AISIX catalog: %w", err))
		} else {
			next.aisixOrderedIDs = append([]string(nil), ids...)
			next.aisixIDs = idSet(ids)
			next.aisixAvailability = availability(next.aisixIDs)
		}
	}

	previous := o.current()
	if previous.generation != 0 && sameRoutingSnapshot(previous, next) {
		return errors.Join(failures...)
	}
	next.generation = previous.generation + 1
	o.snapshot.Store(next)
	return errors.Join(failures...)
}

// start 立即初始化并按固定周期刷新；查询路径只读已发布快照。
func (o *routingSnapshotOwner) start(ctx context.Context) {
	go func() {
		if err := o.refresh(ctx); err != nil && ctx.Err() == nil {
			o.log.Warn("routing snapshot refresh failed", "err", err)
		}
		interval := o.refreshInterval
		if interval <= 0 {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := o.refresh(ctx); err != nil && ctx.Err() == nil {
					o.log.Warn("routing snapshot refresh failed", "err", err)
				}
			}
		}
	}()
}

func (s *routingSnapshot) decision(model string) routingDecision {
	if s.cpaAvailability == sourceFailed {
		return routingUnavailable
	}
	if s.cpaIDs[model] {
		return routingCPA
	}
	if s.aisixAvailability == sourceFailed {
		return routingUnavailable
	}
	if s.aisixIDs[model] {
		return routingAISIX
	}
	return routingNotFound
}

func (o *routingSnapshotOwner) handleReadiness() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		snapshot := o.current()
		if snapshot.generation == 0 || snapshot.cpaAvailability == sourceFailed {
			writeJSONError(w, http.StatusServiceUnavailable, "catalog_not_ready", "CPA catalog snapshot is not ready")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ready", "generation": snapshot.generation})
	}
}

func (o *routingSnapshotOwner) handleRoutingIndex() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		models, ok := r.URL.Query()["model"]
		if !ok || len(models) != 1 || models[0] == "" || len(models[0]) > 1024 {
			writeJSONError(w, http.StatusBadRequest, "invalid_model", "exactly one bounded model parameter is required")
			return
		}
		snapshot := o.current()
		_ = json.NewEncoder(w).Encode(routingIndexResponse{
			Decision:   snapshot.decision(models[0]),
			Generation: snapshot.generation,
		})
	}
}
