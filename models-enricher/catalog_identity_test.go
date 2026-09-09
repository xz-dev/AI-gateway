package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// 通过真实 handler 检查公开目录；夹具管理配置明确声明原名和路由前缀。
func TestCatalogIdentityVendorNamespace(t *testing.T) {
	fake := &fakeCPA{
		native: []byte(`{"models":[
			{"slug":"nvidia/nemotron-3-ultra-550b-a55b"},
			{"slug":"hub/nvidia/nemotron-3-ultra-550b-a55b"},
			{"slug":"opaque","id":"original-id","unknown":null,"enabled":false,"limit":9007199254740993}
		]}`),
		channelsBody: []byte(`{"openai-compatibility":[{
			"name":"Fixture","prefix":"hub","base-url":"https://example.invalid/v1",
			"api-key-entries":[{"auth-index":"fixture"}],
			"models":[{"name":"nvidia/nemotron-3-ultra-550b-a55b","alias":"nvidia/nemotron-3-ultra-550b-a55b"}]
		}]}`),
	}
	baseHandler := fake.handler()
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			w.Write([]byte(`{"files":[]}`))
			return
		}
		baseHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(cpa.Close)
	cfg := testCfg()
	cfg.Channels = map[string]ChannelConfig{}
	body := httptest.NewRecorder()
	newTestHandler(t, cfg, cpa).ServeHTTP(body, httptest.NewRequest("GET", "/v1/models?client_version=identity", nil))
	if body.Code != http.StatusOK {
		t.Fatalf("catalog status %d: %s", body.Code, body.Body.String())
	}
	var got Manifest
	if err := decodeJSON(body.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, model := range got.Models {
		if model["slug"] == "nvidia/nemotron-3-ultra-550b-a55b" {
			t.Fatal("vendor namespace was mistaken for a CPA routing prefix")
		}
	}
	want := []map[string]any{
		{"slug": "hub/nvidia/nemotron-3-ultra-550b-a55b"},
	}
	if !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("only the management-matched member should remain: got %#v, want %#v", got.Models, want)
	}
}
