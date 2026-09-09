package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// 仅由离线 APISIX 容器运行；普通 Go 测试不启动外部程序。
func TestCatalogHTTPFixture(t *testing.T) {
	root := os.Getenv("CATALOG_HTTP_FIXTURE_ROOT")
	if root == "" {
		t.Skip("requires the task-scoped APISIX container")
	}
	work := "/tmp/layered-http"
	if err := os.MkdirAll(work+"/logs", 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(name, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	load := func(name string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := yaml.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	serve := func(address string, handler http.Handler) {
		t.Helper()
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
		t.Cleanup(func() { server.Close() })
		go server.Serve(listener)
	}

	// 整个容器无网络；固定域名只解析到此 namespace 的 loopback fixture。
	dns, err := net.ListenPacket("udp", "127.0.0.1:1053")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dns.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := dns.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 17 {
				continue
			}
			pos := 12
			var labels []string
			for pos < n && buf[pos] != 0 {
				length := int(buf[pos])
				if pos+1+length >= n {
					break
				}
				labels = append(labels, string(buf[pos+1:pos+1+length]))
				pos += 1 + length
			}
			if pos+5 > n {
				continue
			}
			ip := map[string]byte{"ai-sse-keepalive-ingress-relay": 2, "model-catalog-sidecar": 3, "models-enricher": 4}[strings.Join(labels, ".")]
			answer := append([]byte(nil), buf[:pos+5]...)
			binary.BigEndian.PutUint16(answer[2:4], 0x8180)
			binary.BigEndian.PutUint16(answer[6:8], 0)
			binary.BigEndian.PutUint16(answer[8:10], 0)
			binary.BigEndian.PutUint16(answer[10:12], 0)
			if ip != 0 && binary.BigEndian.Uint16(buf[pos+1:pos+3]) == 1 {
				binary.BigEndian.PutUint16(answer[6:8], 1)
				answer = append(answer, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 127, 0, 0, ip)
			}
			dns.WriteTo(answer, peer)
		}
	}()

	blob := strings.Repeat("x", (3<<20)+37)
	native, _ := json.Marshal(map[string]any{"models": []any{
		map[string]any{"slug": "c/vision", "id": "c/vision", "context_window": 272000, "max_output_tokens": 128000, "input_modalities": []string{"text", "image"}, "output_modalities": []string{"text"}, "vendor": map[string]any{"sequence": json.Number("9007199254740993"), "price": json.Number("0.1234567890123456789012345"), "blob": blob}},
		map[string]any{"slug": "c/image", "output_modalities": []string{"image"}},
		map[string]any{"slug": "c/audio", "output_modalities": []string{"audio"}},
		map[string]any{"slug": "c/video", "output_modalities": []string{"video"}},
		map[string]any{"slug": "c/unknown"},
		map[string]any{"slug": "c/denied", "vendor": map[string]any{"private_to_other_group": true}},
	}})
	// 模拟原生目录为每条模型携带长指令；保留完整授权目录也必须可返回。
	var growthRows, growthAllowed []any
	for i := 0; i < 251; i++ {
		id := fmt.Sprintf("c/growth-%d", i)
		growthRows = append(growthRows, map[string]any{"slug": id, "id": id, "base_instructions": strings.Repeat("x", 20000), "model_messages": map[string]any{"instructions": strings.Repeat("y", 24000), "sequence": json.Number("9007199254740993")}})
		growthAllowed = append(growthAllowed, map[string]any{"slug": id})
	}
	growth, _ := json.Marshal(map[string]any{"models": growthRows})
	if name := os.Getenv("CATALOG_HTTP_GROWTH_FIXTURE"); name != "" {
		growth, err = os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := decodeJSON(growth, &m); err != nil {
			t.Fatal(err)
		}
		growthRows, growthAllowed = nil, nil
		for _, model := range m.Models {
			growthRows = append(growthRows, model)
			growthAllowed = append(growthAllowed, map[string]any{"slug": model["slug"]})
		}
	}
	var growthManifest Manifest
	if err := decodeJSON(growth, &growthManifest); err != nil {
		t.Fatal(err)
	}
	growthWant := map[string]map[string]any{}
	for _, model := range growthManifest.Models {
		growthWant[model["slug"].(string)] = model
	}
	growthACL, _ := json.Marshal(map[string]any{"models": growthAllowed})
	oversized, _ := json.Marshal(map[string]any{"models": []any{map[string]any{"slug": "c/vision", "blob": strings.Repeat("x", 16<<20)}}})
	// 此夹具验证传输与资源；独立声明管理模型，避免以身份缺失绕过大响应断言。
	var registered []map[string]string
	for _, name := range []string{"vision", "image", "audio", "video", "unknown", "denied"} {
		registered = append(registered, map[string]string{"name": name})
	}
	for i := 0; i < 251; i++ {
		registered = append(registered, map[string]string{"name": fmt.Sprintf("growth-%d", i)})
	}
	kind, _ := json.Marshal(map[string]any{"openai-compatibility": []any{map[string]any{"name": "fixture", "prefix": "c", "base-url": "https://unused.invalid", "auth-index": "fixture", "models": registered}}})
	fake := &fakeCPA{native: native, channelsBody: kind, nativeByVersion: map[string][]byte{"growth": growth, "concurrent-growth": growth, "oversized": oversized, "native-fail": []byte(`invalid-json`), "v0.65.0": growth}}
	var concurrentBasic atomic.Int32
	bothAdmitted := make(chan struct{})
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	stubSources(t)
	external := os.Getenv("CATALOG_HTTP_EXTERNAL_APISIX") == "1"
	var enricherRequests atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"source":"CPA"}`)
	}))
	defer fallback.Close()
	if external {
		raw, _ := json.Marshal(map[string]any{"cpa_base_url": cpa.URL, "http_concurrency": 8, "channels": map[string]any{}})
		write(work+"/enricher.json", raw)
	} else {
		pool := newHTTPPool(8, 5*time.Second)
		pool.cache, err = newReadCache(t.TempDir(), testLog())
		if err != nil {
			t.Fatal(err)
		}
		handler := handleModels(testCfg(), newCPAClient(cpa.URL, "m", "c", pool, testLog()), pool, testLog())
		serve("127.0.0.4:8090", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enricherRequests.Add(1)
			handler.ServeHTTP(w, r)
		}))
	}
	serve("127.0.0.2:8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-only" {
			// 生产 basic 腿经既有入口包装后返回空 404，而非直接 Sub2API 的 401。
			w.WriteHeader(404)
			return
		}
		version := r.URL.Query().Get("client_version")
		if version == "concurrent-growth" {
			if concurrentBasic.Add(1) == 2 {
				close(bothAdmitted)
			}
			select {
			case <-bothAdmitted:
			case <-time.After(10 * time.Second):
				w.WriteHeader(503)
				return
			}
		}
		if strings.Contains(version, "growth") {
			w.Write(growthACL)
			return
		}
		fmt.Fprint(w, `{"models":[{"slug":"c/vision"},{"slug":"c/image"},{"slug":"c/audio"},{"slug":"c/video"},{"slug":"c/unknown"}]}`)
	}))

	// 使用已有配置，仅将测试进程地址、DNS、文件位置绑定到隔离 namespace。
	config := load("apisix-models/config.yaml")
	api := config["apisix"].(map[string]any)
	api["node_listen"] = []int{9000, 9080}
	api["dns_resolver"] = []string{"127.0.0.1:1053"}
	api["extra_lua_path"] = root + "/apisix/lua/?.lua"
	config["nginx_config"].(map[string]any)["http"].(map[string]any)["custom_lua_shared_dict"] = map[string]any{"model-list-intersection": "1m"}
	front := load("apisix/apisix.yaml")
	catalog := load("apisix-models/apisix.yaml")
	var routes []any
	for _, bundle := range []struct {
		data map[string]any
		port string
		ids  map[string]bool
	}{
		{front, "9000", map[string]bool{"ai-api-models-intersect": true, "ai-api-models-get": true}},
		{catalog, "9080", map[string]bool{"codex-models": true, "models-table": true, "catch-all": true}},
	} {
		for _, raw := range bundle.data["routes"].([]any) {
			route := raw.(map[string]any)
			if !bundle.ids[route["id"].(string)] {
				continue
			}
			vars, _ := route["vars"].([]any)
			if route["id"] == "models-table" {
				// 仅替换夹具网络地址，保留实际路由的来源匹配条件。
				for _, raw := range vars {
					condition := raw.([]any)
					if condition[0] == "realip_remote_addr" && condition[2] == "172.30.42.3" {
						condition[2] = "127.0.0.3"
					}
				}
			}
			route["vars"] = append(vars, []any{"server_port", "==", bundle.port})
			routes = append(routes, route)
		}
	}
	var upstreams []any
	for _, raw := range catalog["upstreams"].([]any) {
		if raw.(map[string]any)["id"] == "enricher" {
			upstreams = append(upstreams, raw)
		}
	}
	upstreams = append(upstreams, map[string]any{"id": "sub2api", "type": "roundrobin", "nodes": map[string]int{"127.0.0.2:8080": 1}})
	upstreams = append(upstreams, map[string]any{"id": "cpa", "type": "roundrobin", "nodes": map[string]int{strings.TrimPrefix(fallback.URL, "http://"): 1}})
	for name, value := range map[string]any{
		"config.yaml": config,
		"apisix.yaml": map[string]any{"routes": routes, "upstreams": upstreams, "plugin_configs": front["plugin_configs"]},
	} {
		raw, err := yaml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		write("/usr/local/apisix/conf/"+name, append(raw, []byte("\n#END\n")...))
	}
	template, err := os.ReadFile(filepath.Join(root, "model-catalog-sidecar/templates/default.conf.template"))
	if err != nil {
		t.Fatal(err)
	}
	sidecar := strings.ReplaceAll(string(template), "listen 8080;", "listen 127.0.0.3:8080;")
	sidecar = strings.ReplaceAll(sidecar, "172.30.42.2:9080", "127.0.0.1:9080")
	sidecar = strings.ReplaceAll(sidecar, "server_name _;", "server_name _;\nproxy_bind 127.0.0.3;")
	write(work+"/sidecar.conf", []byte("worker_processes 1;\npid "+work+"/sidecar.pid;\nerror_log stderr warn;\nevents {}\nhttp { access_log off; "+sidecar+"\n}\n"))
	command := func(program string, args ...string) {
		t.Helper()
		cmd := exec.Command(program, args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v", program, args, err)
		}
	}
	nginx := "/usr/local/openresty/nginx/sbin/nginx"
	if !external {
		command(nginx, "-p", work, "-c", work+"/sidecar.conf")
		t.Cleanup(func() { exec.Command(nginx, "-p", work, "-c", work+"/sidecar.conf", "-s", "stop").Run() })
	}
	if external {
		// 外部受限 APISIX 共享 namespace；fixture 的 Go/假上游内存不计入前端预算。
		write("/usr/local/apisix/conf/fixture-ready", []byte("ready\n"))
	} else {
		apisix := exec.Command("apisix", "start")
		apisix.Stdout, apisix.Stderr = os.Stdout, os.Stderr
		if err := apisix.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			exec.Command("apisix", "stop").Run()
			apisix.Wait()
		})
	}
	client := &http.Client{Timeout: 60 * time.Second}
	request := func(address, path string, authorized bool) (int, []byte, http.Header) {
		t.Helper()
		req, _ := http.NewRequest("GET", "http://"+address+path, nil)
		if authorized {
			req.Header.Set("Authorization", "Bearer fixture-only")
		}
		response, err := client.Do(req)
		if err != nil {
			return 0, []byte(err.Error()), nil
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body, response.Header
	}
	for deadline := time.Now().Add(60 * time.Second); ; {
		status, _, _ := request("127.0.0.1:9000", "/v1/models", true)
		if status == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("APISIX startup: HTTP %d", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, path := range []string{"/v1/models", "/v1/models?client_version="} {
		status, body, _ := request("127.0.0.1:9080", path, false)
		if status != 200 || string(body) != `{"source":"CPA"}` {
			t.Fatalf("no-version request did not reach CPA: %s %d", path, status)
		}
	}
	beforeEnricher := enricherRequests.Load()
	status, original, _ := request("127.0.0.3:8080", "/v1/models?client_version=fixture", false)
	if status != 200 {
		t.Fatalf("sidecar: HTTP %d: %.300s", status, original)
	}
	status, body, headers := request("127.0.0.1:9000", "/v1/models?client_version=fixture", true)
	if status != 200 {
		t.Fatalf("front: HTTP %d: %.300s", status, body)
	}
	var result Manifest
	if err := decodeJSON(body, &result); err != nil || len(result.Models) != 5 {
		t.Fatalf("front inventory: models=%d err=%v", len(result.Models), err)
	}
	for _, model := range result.Models {
		if model["slug"] == "c/denied" {
			t.Fatal("entitlement leak")
		}
		if model["slug"] == "c/vision" {
			vendor := model["vendor"].(map[string]any)
			sequence, integer := vendor["sequence"].(json.Number)
			price, decimal := vendor["price"].(json.Number)
			if !integer || !decimal || vendor["blob"] != blob || sequence.String() != "9007199254740993" || price.String() != "0.1234567890123456789012345" {
				t.Fatal("HTTP metadata precision/completeness lost")
			}
			assertMetadataJSON(t, model["input_modalities"], `["text","image"]`)
			assertMetadataJSON(t, model["output_modalities"], `["text"]`)
		}
	}
	if headers.Get("Apisix-Cache-Status") == "HIT" || headers.Get("Apisix-Cache-Status") == "STALE" {
		t.Fatalf("final catalog unexpectedly cached: %v", headers)
	}
	if !external && enricherRequests.Load() != beforeEnricher+2 {
		t.Fatal("same-version requests did not both reach enricher")
	}
	status, grown, _ := request("127.0.0.1:9000", "/v1/models?client_version=growth", true)
	if status != 200 || len(grown) <= 8<<20 || len(grown) > 16<<20 {
		t.Fatalf("representative expanded catalog: status=%d bytes=%d", status, len(grown))
	}
	var grownResult Manifest
	if err := decodeJSON(grown, &grownResult); err != nil || len(grownResult.Models) != len(growthRows) {
		t.Fatalf("expanded catalog model loss: models=%d err=%v", len(grownResult.Models), err)
	}
	for _, model := range grownResult.Models {
		want := growthWant[model["slug"].(string)]
		if want == nil {
			t.Fatal("unexpected grown routing identity")
		}
		for _, field := range []string{"base_instructions", "model_messages"} {
			value, _ := json.Marshal(want[field])
			assertMetadataJSON(t, model[field], string(value))
		}
	}
	wantHash := sha256.Sum256(grown)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			req, _ := http.NewRequest("GET", "http://127.0.0.1:9000/v1/models?client_version=concurrent-growth", nil)
			req.Header.Set("Authorization", "Bearer fixture-only")
			r, err := client.Do(req)
			if err != nil {
				results <- err
				return
			}
			defer r.Body.Close()
			h := sha256.New()
			n, err := io.Copy(h, r.Body)
			if err == nil && (r.StatusCode != 200 || n != int64(len(grown)) || fmt.Sprintf("%x", h.Sum(nil)) != fmt.Sprintf("%x", wantHash)) {
				err = fmt.Errorf("concurrent catalog: status=%d bytes=%d", r.StatusCode, n)
			}
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if concurrentBasic.Load() != 2 {
		t.Fatal("two admitted requests were not observed")
	}
	t.Logf("filtered catalog: %d bytes, %d exact records, two concurrent admitted requests PASS", len(grown), len(growthRows))
	before := fake.nativeCalls.Load()
	status, _, _ = request("127.0.0.1:9000", "/v1/models?client_version=unauthorized-cold", false)
	if status != 404 || fake.nativeCalls.Load() != before {
		t.Fatal("unauthorized request reached cold enriched inventory")
	}
	status, _, _ = request("127.0.0.1:9000", "/v1/models?client_version=native-fail", true)
	if status != 502 {
		t.Fatalf("native must fail closed: %d", status)
	}
	status, _, _ = request("127.0.0.1:9000", "/v1/models?client_version=oversized", true)
	if status != 502 {
		t.Fatalf("16 MiB path boundary: %d", status)
	}
	status, plain, _ := request("127.0.0.3:8080", "/v1/models", false)
	if status != 200 || string(plain) != `{"source":"CPA"}` {
		t.Fatalf("sidecar no-version CPA passthrough: %d", status)
	}
	status, page, pageHeaders := request("127.0.0.3:8080", "/models-table", false)
	if status != 200 || !strings.HasPrefix(pageHeaders.Get("Content-Type"), "text/html") || strings.Count(string(page), "<td") != len(growthRows)*11 || len(page) > 1<<20 || strings.Contains(string(page), "<script") {
		t.Fatalf("sidecar SSR: HTTP %d, %d bytes, %d cells", status, len(page), strings.Count(string(page), "<td"))
	}
	// 即使伪造来源头，非边车连接也不能进入完整目录HTML路由。
	beforeEnricher = enricherRequests.Load()
	spoof, _ := http.NewRequest("GET", "http://127.0.0.1:9080/models-table", nil)
	spoof.Header.Set("X-Real-IP", "127.0.0.3")
	spoof.Header.Set("X-Forwarded-For", "127.0.0.3")
	denied, err := client.Do(spoof)
	if err != nil {
		t.Fatal(err)
	}
	deniedBody, err := io.ReadAll(denied.Body)
	denied.Body.Close()
	if err != nil || string(deniedBody) != `{"source":"CPA"}` || enricherRequests.Load() != beforeEnricher {
		t.Fatal("non-sidecar caller reached the HTML route")
	}
	for _, path := range []string{"/v1/responses", "/v0/management/config", "/arbitrary"} {
		status, _, _ := request("127.0.0.3:8080", path, false)
		if status != 0 {
			t.Fatalf("sidecar restricted path %s returned %d, want closed connection", path, status)
		}
	}
	t.Logf("real HTTP: Go enricher → uncached APISIX → sidecar nginx → front APISIX; %d original bytes, %d filtered bytes; numeric fidelity, entitlement-first, native failure, 16 MiB path cap, page and sidecar path restrictions PASS", len(original), len(body))
}
