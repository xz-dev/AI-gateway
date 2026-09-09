package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 无需执行JS的HTTP正文即含完整表格；与JSON入口使用同一份已准入数据。
func TestModelsTableSSR(t *testing.T) {
	raw, err := os.ReadFile("testdata/models-table.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := decodeJSON(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCPA{}
	for _, model := range manifest.Models {
		name := asString(model["slug"])
		fake.oauthModels = append(fake.oauthModels, name)
		model["slug"] = "oauth/" + name // 与现有同账号原名/限定名准入夹具一致。
		if name == "00" {
			model["context_window"] = json.Number("9007199254740993")
		}
	}
	admitted, _ := json.Marshal(manifest)
	fake.native = []byte(`invalid-json`)
	fake.nativeByVersion = map[string][]byte{"1": admitted}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	cfg := testCfg()
	cfg.Channels = map[string]ChannelConfig{}
	handler := newTestHandler(t, cfg, cpa)
	save := func(name string, w *httptest.ResponseRecorder) {
		t.Helper()
		if dir := os.Getenv("MODELS_TABLE_FIXTURE_OUTPUT"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, name), w.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/models-table", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected server-rendered HTML, got HTTP %d %s: %.200s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "<script") || strings.Contains(body, "<img") || strings.Contains(body, "<svg") || strings.Contains(body, "loading…") {
		t.Fatal("page requires JS or injected active markup")
	}
	if strings.Count(body, "<th>") != 11 || strings.Count(body, "<td") != 121 || !strings.Contains(body, "11 models · client_version=1") {
		t.Fatal("complete table/count missing from HTML response")
	}
	if strings.Index(body, ">oauth/00</td>") > strings.Index(body, ">oauth/10-hostile</td>") || !strings.Contains(body, "&lt;img") {
		t.Fatal("ordering or text escaping lost")
	}
	save("table.html", w)

	// 不改变已存在的JSON接口、类型和字段缺失状态。
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/v1/models?client_version=v0.65.0", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatal("JSON endpoint changed representation")
	}
	var result Manifest
	if err := decodeJSON(w.Body.Bytes(), &result); err != nil || len(result.Models) != len(manifest.Models) {
		t.Fatalf("JSON membership changed: %v", err)
	}
	want := map[string]map[string]any{}
	for _, model := range manifest.Models {
		want[asString(model["slug"])] = model
	}
	for _, model := range result.Models {
		slug := asString(model["slug"])
		original, ok := want[slug]
		if !ok {
			t.Fatalf("unexpected or duplicate model %s", slug)
		}
		encoded, _ := json.Marshal(original)
		assertMetadataJSON(t, model, string(encoded))
		delete(want, slug)
	}

	// 上游失败是错误HTML，不能伪装成200空目录。
	fake.nativeStatus = http.StatusBadGateway
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/models-table", nil))
	if w.Code != 502 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "HTTP 502") || strings.Contains(w.Body.String(), "<table") {
		t.Fatalf("failed catalog did not produce error HTML: HTTP %d %.200s", w.Code, w.Body.String())
	}
	save("table-error.html", w)

	cpa.Close()
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/models-table", nil))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "native_manifest_failed") || strings.Contains(w.Body.String(), "<table") {
		t.Fatal("transport failure became an empty success page")
	}
	save("table-network-error.html", w)
}
