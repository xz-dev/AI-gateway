package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// HTTP验收只访问本地CPA替身；不请求真实管理端或OAuth凭据文件。
func identityHandler(t *testing.T, fake *fakeCPA, extra http.HandlerFunc, cfg *Config, pool *httpPool) (http.Handler, *bytes.Buffer) {
	t.Helper()
	base := fake.handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0/management/auth-files") || strings.HasPrefix(r.URL.Path, "/v0/management/model-definitions/") || r.URL.Path == "/v0/management/oauth-model-alias" {
			if extra != nil {
				extra(w, r)
			} else if r.URL.Path == "/v0/management/auth-files" {
				w.Write([]byte(`{"files":[]}`))
			} else {
				t.Errorf("unexpected identity request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		base.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	if pool == nil {
		pool = newHTTPPool(8, time.Second)
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	return handleModels(cfg, newCPAClient(server.URL, "management-test", "client-test", pool, log), pool, log), logs
}

func identityCatalog(t *testing.T, handler http.Handler) map[string]map[string]any {
	t.Helper()
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/v1/models?client_version=identity-test", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("catalog status %d: %s", r.Code, r.Body.String())
	}
	var manifest Manifest
	if err := decodeJSON(r.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, model := range manifest.Models {
		out[asString(model["slug"])] = model
	}
	return out
}

func TestCatalogIdentityOAuthRequiresPositiveMatch(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = nil
	fake := &fakeCPA{native: []byte(`{"models":[
		{"slug":"gpt-5.6-sol"}, {"slug":"quick"}, {"slug":"private/gpt-5.6-sol","unknown":null},
		{"slug":"vendor/gpt-5.6-sol","unknown":null}, {"slug":"new-dynamic","id":"unchanged"}
	]}`)}
	extra := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			w.Write([]byte(`{"files":[{"name":"fixture.json","provider":"codex"}]}`))
		case "/v0/management/auth-files/models":
			if r.URL.Query().Get("name") != "fixture.json" {
				t.Error("registration query lost account association")
			}
			w.Write([]byte(`{"models":[{"id":"gpt-5.6-sol"},{"id":"quick"},{"id":"private/gpt-5.6-sol"},{"id":"vendor/gpt-5.6-sol"},{"id":"new-dynamic"},{"id":"private/unavailable"}]}`))
		case "/v0/management/model-definitions/codex":
			w.Write([]byte(`{"models":[{"id":"gpt-5.6-sol"},{"id":"vendor/gpt-5.6-sol"}]}`))
		case "/v0/management/oauth-model-alias":
			w.Write([]byte(`{"oauth-model-alias":{"codex":[{"name":"gpt-5.6-sol","alias":"quick","fork":true},{"name":"quick","alias":"new-dynamic"}]}}`))
		default:
			t.Errorf("unexpected identity endpoint: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}
	h, logs := identityHandler(t, fake, extra, cfg, nil)
	got := identityCatalog(t, h)
	if len(got) != 1 || got["private/gpt-5.6-sol"] == nil {
		t.Fatalf("originals and unmatched candidates must not survive: %#v", got)
	}
	assertMetadataJSON(t, got["private/gpt-5.6-sol"], `{"slug":"private/gpt-5.6-sol","unknown":null}`)
	if strings.Contains(logs.String(), "identity unavailable") {
		t.Fatal("successful lookup miss incorrectly treated as read failure")
	}
}

func TestCatalogIdentityKeepsRepeatedNamespace(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = map[string]ChannelConfig{"nvidia": {}}
	fake := &fakeCPA{
		native:       []byte(`{"models":[{"slug":"nvidia/nemotron"},{"slug":"nvidia/nvidia/nemotron"}]}`),
		channelsBody: []byte(`{"openai-compatibility":[{"name":"GPU","prefix":"nvidia","base-url":"https://unused.invalid/v1","api-key-entries":[{"auth-index":"key"}],"models":[{"name":"nvidia/nemotron"}]}]}`),
	}
	h, _ := identityHandler(t, fake, nil, cfg, nil)
	got := identityCatalog(t, h)
	if len(got) != 1 || got["nvidia/nvidia/nemotron"] == nil {
		t.Fatalf("upstream namespace was stripped or raw member resurrected: %#v", got)
	}
}

func TestCatalogIdentityAliasCollisionAndPublicMembership(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = nil
	fake := &fakeCPA{
		native: []byte(`{"models":[{"slug":"vendor/named"},{"slug":"hub/vendor/named"},{"slug":"vendor/original","null":null}]}`),
		channelsBody: []byte(`{"openai-compatibility":[
			{"prefix":"hub","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"vendor/original","alias":"vendor/named"},{"name":"unavailable"}]},
			{"prefix":"vendor","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"named"}]}
		]}`),
	}
	h, _ := identityHandler(t, fake, nil, cfg, nil)
	got := identityCatalog(t, h)
	if len(got) != 2 || got["vendor/named"] == nil || got["hub/vendor/named"] == nil || got["vendor/original"] != nil {
		t.Fatalf("alias/route collision or unknown source name mishandled: %#v", got)
	}
	if got["hub/unavailable"] != nil {
		t.Fatal("management declaration revived unavailable model")
	}
}

func TestCatalogIdentityReadCacheAndFailure(t *testing.T) {
	for _, code := range []int{503, 401, 403, 200} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var status, calls atomic.Int32
			status.Store(200)
			var badShape atomic.Bool
			extra := func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v0/management/auth-files" {
					t.Errorf("unexpected request %s", r.URL.Path)
				}
				calls.Add(1)
				w.WriteHeader(int(status.Load()))
				if badShape.Load() {
					w.Write([]byte(`{"files":"invalid"}`))
				} else {
					w.Write([]byte(`{"files":[]}`))
				}
			}
			cfg := testCfg()
			cfg.Channels = nil
			fake := &fakeCPA{native: []byte(`{"models":[{"slug":"vendor/m","id":"unchanged","null":null,"n":9007199254740993},{"slug":"p/vendor/m"}]}`), channelsBody: []byte(`{"openai-compatibility":[{"prefix":"p","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"vendor/m"}]}]}`)}
			pool := cachedTestPool(t)
			now := time.Now()
			pool.cache.clock = func() time.Time { return now }
			h, logs := identityHandler(t, fake, extra, cfg, pool)
			if len(identityCatalog(t, h)) != 1 || len(identityCatalog(t, h)) != 1 || calls.Load() != 1 {
				t.Fatal("fresh successful identity input not reused")
			}
			status.Store(int32(code))
			badShape.Store(code == 200)
			now = now.Add(365 * 24 * time.Hour)
			got := identityCatalog(t, h)
			if code == 503 {
				if len(got) != 1 {
					t.Fatal("eligible stale input lost")
				}
			} else {
				if len(got) != 2 {
					t.Fatal("denied/invalid identity input must preserve CPA baseline")
				}
				assertMetadataJSON(t, got["vendor/m"], `{"slug":"vendor/m","id":"unchanged","null":null,"n":9007199254740993}`)
				if !strings.Contains(logs.String(), "identity read failed") {
					t.Fatal("identity failure not warned")
				}
				status.Store(503)
				if len(identityCatalog(t, h)) != 2 {
					t.Fatal("denied/invalid old input resurrected on later 503")
				}
			}
		})
	}
}

func TestCatalogIdentityIncompleteDiscoveryPreservesBaseline(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = nil
	fake := &fakeCPA{
		native: []byte(`{"models":[{"slug":"vendor/m","id":"unchanged","null":null},{"slug":"p/vendor/m"}]}`),
		channelsBody: []byte(`{"openai-compatibility":[
			{"prefix":"p","base-url":"https://unused.invalid","api-key-entries":[{}],"models":[{"name":"vendor/m"}]},
			{"prefix":"unresolved","models":[{"name":"m"}]}
		]}`),
	}
	h, logs := identityHandler(t, fake, nil, cfg, cachedTestPool(t))
	for range 2 { // 缓存命中也必须保留发现不完整的信号。
		got := identityCatalog(t, h)
		if len(got) != 2 || got["p/vendor/m"] == nil {
			t.Fatalf("partial discovery silently treated as complete: %#v", got)
		}
		assertMetadataJSON(t, got["vendor/m"], `{"slug":"vendor/m","id":"unchanged","null":null}`)
	}
	if !strings.Contains(logs.String(), "discovery incomplete") {
		t.Fatal("partial discovery not warned")
	}
}

func TestCatalogIdentityFailurePreservesOriginal(t *testing.T) {
	cfg := testCfg()
	cfg.Channels = nil // 目录身份不能依赖Go补全渠道白名单。
	fake := &fakeCPA{
		native: []byte(`{"models":[
			{"slug":"nvidia/nemotron-3-ultra-550b-a55b"},
			{"slug":"hub/nvidia/nemotron-3-ultra-550b-a55b"},
			{"slug":"unknown","id":"original-id","null":null,"flag":false,"zero":0,"empty":[],"large":9007199254740993}
		]}`),
		channelsBody: []byte(`{"openai-compatibility":[{"name":"Different display label","prefix":"hub","base-url":"https://unused.invalid/v1","api-key-entries":[{"auth-index":"key"}],"models":[{"name":"nvidia/nemotron-3-ultra-550b-a55b","alias":""}]}]}`),
	}
	h, _ := identityHandler(t, fake, nil, cfg, nil)
	if got := identityCatalog(t, h); len(got) != 1 || got["hub/nvidia/nemotron-3-ultra-550b-a55b"] == nil {
		t.Fatalf("successful lookup should retain only the matched route: %#v", got)
	}
	h, logs := identityHandler(t, fake, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }, cfg, nil)
	got := identityCatalog(t, h)
	want := map[string]any{"slug": "unknown", "id": "original-id", "null": nil, "flag": false, "zero": json.Number("0"), "empty": []any{}, "large": json.Number("9007199254740993")}
	if !reflect.DeepEqual(got["unknown"], want) {
		t.Fatalf("unknown identity must preserve exact CPA record: %#v", got["unknown"])
	}
	if len(got) != 3 || !strings.Contains(logs.String(), "identity unavailable") {
		t.Fatalf("expected exact baseline records and read-failure warning: count=%d logs=%s", len(got), logs.String())
	}
}
