package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRoutingIndexFourOutcomes(t *testing.T) {
	var aisixStatus atomic.Int64
	aisixStatus.Store(http.StatusOK)
	var aisixCalls atomic.Int64
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aisixCalls.Add(1)
		w.WriteHeader(int(aisixStatus.Load()))
		if aisixStatus.Load() == http.StatusOK {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"overlap"},{"id":"aisix-only"}]}`))
		}
	}))
	t.Cleanup(aisix.Close)

	fake := &fakeCPA{native: []byte(`{"models":[{"slug":"cpa-only"},{"slug":"overlap"},{"slug":"plus+slash/雪 "}]}`)}
	cpa := httptest.NewServer(fake.handler())
	t.Cleanup(cpa.Close)

	pool := newHTTPPool(4, time.Second)
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		time.Minute,
		time.Second,
		testLog(),
	)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	handler := owner.handleRoutingIndex()
	lookup := func(model string) routingIndexResponse {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/routing-index?model="+url.QueryEscape(model), nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("lookup %q: HTTP %d: %s", model, recorder.Code, recorder.Body.String())
		}
		var response routingIndexResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil || len(fields) != 2 || fields["decision"] == nil || fields["generation"] == nil || recorder.Body.Len() > 128 {
			t.Fatalf("routing-index leaked an unbounded catalog shape: %s", recorder.Body.String())
		}
		return response
	}

	for model, want := range map[string]routingDecision{
		"cpa-only":      routingCPA,
		"overlap":       routingCPA,
		"aisix-only":    routingAISIX,
		"plus+slash/雪 ": routingCPA,
		"missing":       routingNotFound,
	} {
		response := lookup(model)
		if response.Decision != want {
			t.Fatalf("lookup %q decision = %q, want %q", model, response.Decision, want)
		}
		if response.Generation == 0 {
			t.Fatalf("lookup %q returned cold generation", model)
		}
	}
	if owner.current().cpaIDs["aisix-only"] {
		t.Fatal("AISIX-only supplemented ID leaked into CPA raw membership")
	}

	beforeCPA, beforeAISIX := fake.nativeCalls.Load(), aisixCalls.Load()
	for range 3 {
		_ = lookup("cpa-only")
	}
	if fake.nativeCalls.Load() != beforeCPA || aisixCalls.Load() != beforeAISIX {
		t.Fatal("routing-index lookup triggered raw catalog collection")
	}

	aisixStatus.Store(http.StatusUnauthorized)
	if err := owner.refresh(context.Background()); err == nil {
		t.Fatal("AISIX authorization failure must be reported")
	}
	if got := lookup("cpa-only").Decision; got != routingCPA {
		t.Fatalf("known CPA model after AISIX failure = %q, want %q", got, routingCPA)
	}
	if got := lookup("aisix-only").Decision; got != routingUnavailable {
		t.Fatalf("AISIX-dependent model after AISIX failure = %q, want %q", got, routingUnavailable)
	}
	if got := owner.current().aisixAvailability; got != sourceFailed {
		t.Fatalf("AISIX authorization failure availability = %q, want %q", got, sourceFailed)
	}
}

func TestRoutingSnapshotRejectsInvalidCPAAuthority(t *testing.T) {
	for name, body := range map[string]string{
		"missing models":    `{}`,
		"null body":         `null`,
		"null models":       `{"models":null}`,
		"record without ID": `{"models":[{"display_name":"not an ID"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(cpa.Close)
			aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m"}]}`))
			}))
			t.Cleanup(aisix.Close)

			pool := newHTTPPool(2, time.Second)
			owner := newRoutingSnapshotOwner(
				newCPAClient(cpa.URL, "management", "client", pool, testLog()),
				newAISIXClient(aisix.URL, "", time.Second, pool),
				time.Minute,
				time.Second,
				testLog(),
			)
			if err := owner.refresh(context.Background()); err == nil {
				t.Fatal("invalid CPA inventory became authoritative")
			}
			snapshot := owner.current()
			if snapshot.cpaAvailability != sourceFailed || snapshot.decision("m") != routingUnavailable || snapshot.decision("absent") != routingUnavailable {
				t.Fatalf("invalid CPA inventory enabled an authoritative decision: %+v", snapshot)
			}
		})
	}
}

func TestInvalidCPAResponseCannotBeResurrectedByTransientFallback(t *testing.T) {
	var status atomic.Int64
	status.Store(http.StatusOK)
	var body atomic.Value
	body.Store(`{"models":[{"slug":"old"}]}`)
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
		if status.Load() == http.StatusOK {
			_, _ = w.Write([]byte(body.Load().(string)))
		}
	}))
	t.Cleanup(cpa.Close)
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	t.Cleanup(aisix.Close)

	pool := newHTTPPool(2, time.Second)
	cache, err := newReadCache(t.TempDir(), testLog())
	if err != nil {
		t.Fatal(err)
	}
	pool.cache = cache
	var offset atomic.Int64
	cache.clock = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		time.Minute,
		time.Second,
		testLog(),
	)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	offset.Store(int64(10 * time.Minute))
	body.Store(`{"models":null}`)
	if err := owner.refresh(context.Background()); err == nil || owner.current().cpaAvailability != sourceFailed {
		t.Fatal("invalid CPA response did not invalidate authority")
	}
	offset.Store(int64(20 * time.Minute))
	status.Store(http.StatusBadGateway)
	if err := owner.refresh(context.Background()); err == nil || owner.current().cpaAvailability != sourceFailed || owner.current().cpaIDs["old"] {
		t.Fatal("transient failure resurrected CPA authority invalidated by malformed data")
	}
}

func TestCatalogColdFailureDoesNotRefreshOnRequest(t *testing.T) {
	var cpaCalls atomic.Int64
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cpaCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(cpa.Close)
	var aisixCalls atomic.Int64
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aisixCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(aisix.Close)

	pool := newHTTPPool(4, time.Second)
	cpaClient := newCPAClient(cpa.URL, "management", "client", pool, testLog())
	aisixClient := newAISIXClient(aisix.URL, "", time.Second, pool)
	owner := newRoutingSnapshotOwner(cpaClient, aisixClient, time.Hour, time.Second, testLog())
	if err := owner.refresh(context.Background()); err == nil {
		t.Fatal("failed cold collection must report its errors")
	}
	if owner.current().generation == 0 || owner.current().decision("any") != routingUnavailable {
		t.Fatalf("first failed collection was not published: %+v", owner.current())
	}
	beforeCPA, beforeAISIX := cpaCalls.Load(), aisixCalls.Load()
	handler := handleModels(testCfg(), cpaClient, aisixClient, pool, testLog(), owner)
	var requests sync.WaitGroup
	for range 8 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
			if recorder.Code != http.StatusBadGateway {
				t.Errorf("cold catalog HTTP %d, want %d", recorder.Code, http.StatusBadGateway)
			}
		}()
	}
	requests.Wait()
	if cpaCalls.Load() != beforeCPA || aisixCalls.Load() != beforeAISIX {
		t.Fatalf("catalog traffic bypassed the refresh cadence: CPA %d→%d AISIX %d→%d", beforeCPA, cpaCalls.Load(), beforeAISIX, aisixCalls.Load())
	}
}

func TestRoutingSnapshotPublicationIsAtomic(t *testing.T) {
	var cpaBody atomic.Value
	cpaBody.Store(`{"models":[{"slug":"old-cpa"}]}`)
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(cpaBody.Load().(string)))
	}))
	t.Cleanup(cpa.Close)

	var aisixBody atomic.Value
	aisixBody.Store(`{"object":"list","data":[{"id":"old-aisix"}]}`)
	blockAISIX := make(chan struct{}, 1)
	releaseAISIX := make(chan struct{})
	var blockNext atomic.Bool
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if blockNext.CompareAndSwap(true, false) {
			blockAISIX <- struct{}{}
			<-releaseAISIX
		}
		_, _ = w.Write([]byte(aisixBody.Load().(string)))
	}))
	t.Cleanup(aisix.Close)

	pool := newHTTPPool(4, time.Second)
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		time.Minute,
		time.Second,
		testLog(),
	)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	var readers sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(8)
	start := make(chan struct{})
	failed := make(chan string, 1)
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			ready.Done()
			<-start
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				snapshot := owner.current()
				validOld := snapshot.generation == 1 && snapshot.cpaIDs["old-cpa"] && snapshot.aisixIDs["old-aisix"] && !snapshot.cpaIDs["new-cpa"] && !snapshot.aisixIDs["new-aisix"]
				validNew := snapshot.generation == 2 && snapshot.cpaIDs["new-cpa"] && snapshot.aisixIDs["new-aisix"] && !snapshot.cpaIDs["old-cpa"] && !snapshot.aisixIDs["old-aisix"]
				if !validOld && !validNew {
					select {
					case failed <- "reader observed a mixed routing snapshot":
					default:
					}
					return
				}
				if validNew {
					return
				}
			}
			select {
			case failed <- "reader did not observe the published refresh":
			default:
			}
		}()
	}
	ready.Wait()
	close(start)

	cpaBody.Store(`{"models":[{"slug":"new-cpa"}]}`)
	aisixBody.Store(`{"object":"list","data":[{"id":"new-aisix"}]}`)
	blockNext.Store(true)
	refreshDone := make(chan error, 1)
	go func() { refreshDone <- owner.refresh(context.Background()) }()
	<-blockAISIX
	if snapshot := owner.current(); snapshot.generation != 1 || !snapshot.cpaIDs["old-cpa"] || !snapshot.aisixIDs["old-aisix"] {
		t.Fatalf("partial refresh escaped before publication: %+v", snapshot)
	}
	close(releaseAISIX)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	readers.Wait()
	select {
	case message := <-failed:
		t.Fatal(message)
	default:
	}
}

func TestRoutingSnapshotLifecycleRefreshesWithoutCatalogTraffic(t *testing.T) {
	var cpaBody atomic.Value
	cpaBody.Store(`{"models":[{"slug":"cpa-old"}]}`)
	var cpaCalls atomic.Int64
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cpaCalls.Add(1)
		_, _ = w.Write([]byte(cpaBody.Load().(string)))
	}))
	t.Cleanup(cpa.Close)

	var aisixBody atomic.Value
	aisixBody.Store(`{"object":"list","data":[{"id":"aisix-old"}]}`)
	var aisixCalls atomic.Int64
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aisixCalls.Add(1)
		_, _ = w.Write([]byte(aisixBody.Load().(string)))
	}))
	t.Cleanup(aisix.Close)

	pool := newHTTPPool(4, time.Second)
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		10*time.Millisecond,
		100*time.Millisecond,
		testLog(),
	)
	ctx, cancel := context.WithCancel(context.Background())
	owner.start(ctx)
	t.Cleanup(cancel)

	waitFor := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatal("timed out waiting for routing snapshot lifecycle")
	}
	waitFor(func() bool { return owner.current().cpaIDs["cpa-old"] && owner.current().aisixIDs["aisix-old"] })
	firstGeneration := owner.current().generation
	cpaBody.Store(`{"models":[{"slug":"cpa-new"}]}`)
	aisixBody.Store(`{"object":"list","data":[{"id":"aisix-new"}]}`)
	waitFor(func() bool {
		snapshot := owner.current()
		return snapshot.generation > firstGeneration && snapshot.cpaIDs["cpa-new"] && snapshot.aisixIDs["aisix-new"]
	})
	if cpaCalls.Load() < 2 || aisixCalls.Load() < 2 {
		t.Fatalf("periodic lifecycle did not refresh both sources: CPA=%d AISIX=%d", cpaCalls.Load(), aisixCalls.Load())
	}
}

func TestRoutingLifecycleRecoversFromInferenceOnlyColdStart(t *testing.T) {
	var cpaReady atomic.Bool
	var cpaCalls atomic.Int64
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cpaCalls.Add(1)
		if !cpaReady.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"slug":"recovered"}]}`))
	}))
	t.Cleanup(cpa.Close)
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"aisix-only"}]}`))
	}))
	t.Cleanup(aisix.Close)

	pool := newHTTPPool(4, time.Second)
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		10*time.Millisecond,
		50*time.Millisecond,
		testLog(),
	)
	ctx, cancel := context.WithCancel(context.Background())
	owner.start(ctx)
	t.Cleanup(cancel)

	deadline := time.Now().Add(time.Second)
	for cpaCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := owner.current().decision("aisix-only"); got != routingUnavailable {
		t.Fatalf("cold CPA membership guessed AISIX-only: %q", got)
	}
	time.Sleep(35 * time.Millisecond)
	if calls := cpaCalls.Load(); calls > 8 {
		t.Fatalf("failed refresh retried in a tight loop: %d calls", calls)
	}

	cpaReady.Store(true)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if owner.current().decision("recovered") == routingCPA {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("bounded lifecycle did not recover without catalog traffic")
}

func TestRoutingRefreshUsesIndependentSourceBudgets(t *testing.T) {
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(cpa.Close)
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"aisix-fast"}]}`))
	}))
	t.Cleanup(aisix.Close)

	pool := newHTTPPool(4, time.Second)
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		time.Minute,
		20*time.Millisecond,
		testLog(),
	)
	if err := owner.refresh(context.Background()); err == nil {
		t.Fatal("timed-out CPA source must report its failure")
	}
	snapshot := owner.current()
	if snapshot.cpaAvailability != sourceFailed || snapshot.aisixAvailability != sourceUsable || !snapshot.aisixIDs["aisix-fast"] {
		t.Fatalf("one slow source starved the independent fast source: %+v", snapshot)
	}
	if got := snapshot.decision("aisix-fast"); got != routingUnavailable {
		t.Fatalf("failed CPA membership guessed AISIX-only: %q", got)
	}
}

func TestRoutingReadinessRequiresAnInitializedCPASnapshot(t *testing.T) {
	owner := newRoutingSnapshotOwner(nil, nil, time.Minute, time.Second, testLog())
	handler := owner.handleReadiness()
	check := func(want int) {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if recorder.Code != want {
			t.Fatalf("readiness HTTP %d, want %d", recorder.Code, want)
		}
	}
	check(http.StatusServiceUnavailable)
	owner.snapshot.Store(&routingSnapshot{generation: 1, cpaAvailability: sourceUsable, aisixAvailability: sourceFailed})
	check(http.StatusOK)
	owner.snapshot.Store(&routingSnapshot{generation: 2, cpaAvailability: sourceFailed, aisixAvailability: sourceUsable})
	check(http.StatusServiceUnavailable)
}

func TestRoutingSnapshotSourceAvailabilityFollowsReadCachePolicy(t *testing.T) {
	var aisixStatus atomic.Int64
	aisixStatus.Store(http.StatusOK)
	var aisixBody atomic.Value
	aisixBody.Store(`{"object":"list","data":[{"id":"aisix-stale"}]}`)
	aisix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status := int(aisixStatus.Load())
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(aisixBody.Load().(string)))
		}
	}))
	t.Cleanup(aisix.Close)
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"slug":"cpa-only"}]}`))
	}))
	t.Cleanup(cpa.Close)

	pool := newHTTPPool(4, time.Second)
	cache, err := newReadCache("", testLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cache.dir) })
	pool.cache = cache
	var offset atomic.Int64
	cache.clock = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	owner := newRoutingSnapshotOwner(
		newCPAClient(cpa.URL, "management", "client", pool, testLog()),
		newAISIXClient(aisix.URL, "", time.Second, pool),
		time.Minute,
		time.Second,
		testLog(),
	)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	offset.Store(int64(10 * time.Minute))
	aisixStatus.Store(http.StatusBadGateway)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatalf("eligible stale read must remain usable: %v", err)
	}
	if snapshot := owner.current(); snapshot.aisixAvailability != sourceUsable || snapshot.decision("aisix-stale") != routingAISIX {
		t.Fatalf("eligible stale AISIX snapshot not retained: %+v", snapshot)
	}

	offset.Store(int64(20 * time.Minute))
	aisixStatus.Store(http.StatusUnauthorized)
	if err := owner.refresh(context.Background()); err == nil {
		t.Fatal("authorization failure must invalidate the AISIX source")
	}
	if got := owner.current().aisixAvailability; got != sourceFailed {
		t.Fatalf("authorization failure availability = %q, want %q", got, sourceFailed)
	}

	offset.Store(int64(30 * time.Minute))
	aisixStatus.Store(http.StatusOK)
	aisixBody.Store(`{"object":"list","data":[]}`)
	if err := owner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := owner.current(); snapshot.aisixAvailability != sourceEmpty || snapshot.decision("missing") != routingNotFound {
		t.Fatalf("valid empty AISIX inventory was not authoritative: %+v", snapshot)
	}
}

func TestRoutingIndexRejectsMalformedRequests(t *testing.T) {
	owner := newRoutingSnapshotOwner(nil, nil, time.Minute, time.Second, testLog())
	handler := owner.handleRoutingIndex()
	for _, test := range []struct {
		method string
		target string
		status int
	}{
		{http.MethodPost, "/routing-index?model=m", http.StatusMethodNotAllowed},
		{http.MethodGet, "/routing-index", http.StatusBadRequest},
		{http.MethodGet, "/routing-index?model=", http.StatusBadRequest},
		{http.MethodGet, "/routing-index?model=a&model=b", http.StatusBadRequest},
		{http.MethodGet, "/routing-index?model=" + strings.Repeat("x", 1025), http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.target, nil))
		if recorder.Code != test.status {
			t.Fatalf("%s %s: HTTP %d, want %d", test.method, test.target, recorder.Code, test.status)
		}
		if recorder.Body.Len() > 1024 {
			t.Fatalf("error response exceeded bounded routing-index shape: %d bytes", recorder.Body.Len())
		}
	}
}
