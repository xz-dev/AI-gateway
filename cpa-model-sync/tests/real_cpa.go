package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"time"
)

func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func key() string {
	b := make([]byte, 24)
	_, err := rand.Read(b)
	check(err == nil, "random source failed")
	return hex.EncodeToString(b)
}
func main() {
	management, enabledKey, disabledKey := key(), key(), key()
	keysByKind := map[string]string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := enabledKey
		if strings.HasPrefix(r.URL.Path, "/disabled/") {
			expected = disabledKey
		}
		supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		for kind, token := range keysByKind {
			if strings.HasPrefix(r.URL.Path, "/"+kind+"/") {
				expected = token
				if kind == "claude-api-key" {
					supplied = r.Header.Get("x-api-key")
				}
			}
		}
		if supplied != expected {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"A"},{"id":"B"}]}`)
	}))
	defer upstream.Close()
	yaml := fmt.Sprintf(`host: "127.0.0.1"
port: 18317
remote-management:
  allow-remote: false
  secret-key: %q
  disable-control-panel: true
  disable-auto-update: true
auth-dir: /tmp/compat-auth
api-keys: [%q]
logging-to-file: false
usage-statistics-enabled: false
openai-compatibility:
  - name: enabled-probe
    prefix: enabled-probe
    base-url: %q
    api-key-entries:
      - api-key: %q
    models: [{name: old, alias: old}]
  - name: disabled-probe
    disabled: true
    prefix: disabled-probe
    base-url: %q
    api-key-entries:
      - api-key: %q
    models: [{name: old, alias: old}]
`, management, key(), upstream.URL+"/enabled/v1", enabledKey, upstream.URL+"/disabled/v1", disabledKey)
	kinds := []string{"claude-api-key", "gemini-api-key", "interactions-api-key", "codex-api-key", "xai-api-key", "vertex-api-key"}
	for _, kind := range kinds {
		keysByKind[kind] = key()
		yaml += fmt.Sprintf("%s:\n  - api-key: %q\n    prefix: %q\n    base-url: %q\n    models: [{name: old, alias: old}]\n", kind, keysByKind[kind], kind, upstream.URL+"/"+kind+"/v1")
	}
	check(os.WriteFile("/tmp/compat-config.yaml", []byte(yaml), 0600) == nil, "fixture config write failed")
	cpa := exec.Command("/CLIProxyAPI/CLIProxyAPI", "-config", "/tmp/compat-config.yaml")
	// Intentionally discard CPA startup/config logs; report only assertion outcomes.
	cpa.Stdout = io.Discard
	cpa.Stderr = io.Discard
	check(cpa.Start() == nil, "CPA start failed")
	defer func() { _ = cpa.Process.Kill(); _ = cpa.Wait() }()
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path string, body any) (int, map[string]any) {
		var b io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			check(err == nil, "encode failed")
			b = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, "http://127.0.0.1:18317/v0/management/"+path, b)
		check(err == nil, "request build failed")
		req.Header.Set("Authorization", "Bearer "+management)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return 0, nil
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
		return res.StatusCode, out
	}
	var before map[string]any
	for i := 0; i < 100; i++ {
		status, out := call("GET", "openai-compatibility", nil)
		if status == 200 {
			before = out
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	check(before != nil, "CPA management readiness failed")
	entries, ok := before["openai-compatibility"].([]any)
	check(ok && len(entries) == 2, "discovery did not return both fixtures")
	statuses := map[string]int{}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		name := entry["name"].(string)
		keys := entry["api-key-entries"].([]any)
		authIndex, _ := keys[0].(map[string]any)["auth-index"].(string)
		status, out := call("POST", "api-call", map[string]any{"auth_index": authIndex, "method": "GET", "url": entry["base-url"].(string) + "/models?client_version=compat-probe", "header": map[string]string{"Authorization": "Bearer $TOKEN$"}})
		upstreamStatus := 0
		if v, ok := out["status_code"].(float64); ok {
			upstreamStatus = int(v)
		}
		fmt.Printf("%s: auth_index_present=%t management_status=%d upstream_status=%d\n", name, authIndex != "", status, upstreamStatus)
		statuses[name] = upstreamStatus
		if name == "disabled-probe" {
			transientKey := keys[0].(map[string]any)["api-key"].(string)
			fallbackStatus, fallback := call("POST", "api-call", map[string]any{"method": "GET", "url": entry["base-url"].(string) + "/models?client_version=compat-probe", "header": map[string]string{"Authorization": "Bearer " + transientKey}})
			transientKey = ""
			check(fallbackStatus == 200 && fallback["status_code"] == float64(200), "approved transient credential forwarding failed")
			fmt.Println("disabled-probe: transient credential via CPA api-call upstream_status=200")
		}
	}
	status, _ := call("PATCH", "openai-compatibility", map[string]any{"name": "disabled-probe", "value": map[string]any{"models": []map[string]string{{"name": "A", "alias": "A"}}}})
	check(status == 200, "synthetic models-only PATCH failed")
	status, after := call("GET", "openai-compatibility", nil)
	check(status == 200, "read after PATCH failed")
	afterEntries := after["openai-compatibility"].([]any)
	for i, raw := range entries {
		old := raw.(map[string]any)
		now := afterEntries[i].(map[string]any)
		if old["name"] == "disabled-probe" {
			check(now["disabled"] == true, "PATCH enabled disabled provider")
			desired := []any{map[string]any{"name": "A", "alias": "A"}}
			check(reflect.DeepEqual(now["models"], desired), "models did not round-trip")
			delete(old, "models")
			delete(now, "models")
		}
		check(reflect.DeepEqual(old, now), "non-model fields or other provider changed")
	}
	fmt.Println("models-only PATCH: round-trip PASS; disabled preserved; other fields/provider unchanged")
	check(statuses["enabled-probe"] == 200, "enabled control failed")
	check(statuses["disabled-probe"] == 401, "expected disabled forwarding blocker not reproduced")
	fmt.Println("PASS: approved transient forwarding refreshes disabled provider without activation or direct upstream access")
	for _, kind := range kinds {
		getStatus, beforeKind := call("GET", kind, nil)
		check(getStatus == 200, "kind discovery failed: "+kind)
		patchStatus, _ := call("PATCH", kind, map[string]any{"match": keysByKind[kind], "value": map[string]any{"models": []map[string]string{{"name": "A", "alias": "A"}}}})
		getAfter, afterKind := call("GET", kind, nil)
		check(patchStatus == 200 && getAfter == 200, "kind PATCH/GET failed: "+kind)
		listKey := kind
		if kind == "vertex-api-key" {
			listKey = "vertex-api-key"
		}
		beforeList, ok := beforeKind[listKey].([]any)
		check(ok && len(beforeList) == 1, "kind GET shape: "+kind)
		afterList, ok := afterKind[listKey].([]any)
		check(ok && len(afterList) == 1, "kind GET after shape: "+kind)
		old := beforeList[0].(map[string]any)
		now := afterList[0].(map[string]any)
		changed := !reflect.DeepEqual(old["models"], now["models"])
		_, visible := now["models"]
		delete(old, "models")
		delete(now, "models")
		check(reflect.DeepEqual(old, now), "kind non-model change: "+kind)
		expected := kind != "gemini-api-key" && kind != "interactions-api-key"
		check(changed == expected, "source/runtime models mismatch: "+kind)
		fmt.Printf("%s: GET=%d PATCH=%d models_visible=%t models_changed=%t other_fields_unchanged=true\n", kind, getStatus, patchStatus, visible, changed)
	}
	managed := []string{"openai-compatibility", "claude-api-key", "codex-api-key", "xai-api-key", "vertex-api-key"}
	policies := map[string]any{}
	for _, kind := range managed {
		status, data := call("GET", kind, nil)
		check(status == 200, "pre-sync GET failed")
		for _, v := range data[kind].([]any) {
			entry := v.(map[string]any)
			selector := map[string]any{"match": entry["api-key"], "value": map[string]any{"models": []map[string]string{{"name": "old", "alias": "old"}}}}
			if kind == "openai-compatibility" {
				delete(selector, "match")
				selector["name"] = entry["name"]
			}
			status, _ = call("PATCH", kind, selector)
			check(status == 200, "fixture reset failed")
			policies[entry["prefix"].(string)] = map[string]any{"include": []string{"^A$"}}
		}
	}
	snapshot := map[string]any{}
	for _, kind := range append(append([]string{}, kinds...), "openai-compatibility") {
		status, data := call("GET", kind, nil)
		check(status == 200, "snapshot failed")
		snapshot[kind] = data[kind]
	}
	policy, _ := json.Marshal(map[string]any{"cpa_url": "http://127.0.0.1:18317", "client_version": "compat-probe", "channels": policies})
	check(os.WriteFile("/tmp/rust-policy.json", policy, 0600) == nil, "policy write failed")
	runRust := func(expectedUpdated, expectedUnchanged int) {
		cmd := exec.Command("/rust-sync", "--once", "/tmp/rust-policy.json")
		cmd.Env = append(os.Environ(), "CPA_MANAGEMENT_KEY="+management)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			fmt.Printf("Rust command failed (output bytes=%d, stderr bytes=%d)\n", len(output), stderr.Len())
		}
		check(err == nil, "Rust once failed")
		var summary map[string]int
		check(json.Unmarshal(output, &summary) == nil, "Rust summary invalid")
		fmt.Printf("Rust once: updated=%d unchanged=%d failed=%d\n", summary["updated"], summary["unchanged"], summary["failed"])
		check(summary["updated"] == expectedUpdated && summary["unchanged"] == expectedUnchanged && summary["failed"] == 0, "unexpected Rust counts")
	}
	runRust(6, 0)
	for _, kind := range managed {
		status, data := call("GET", kind, nil)
		check(status == 200, "immediate read-back failed")
		for _, row := range data[kind].([]any) {
			entry := row.(map[string]any)
			check(reflect.DeepEqual(entry["models"], []any{map[string]any{"name": "A", "alias": "A"}}), "immediate models mismatch: "+kind+"/"+entry["prefix"].(string))
		}
	}
	runRust(0, 6)
	status, _ = call("PATCH", "openai-compatibility", map[string]any{
		"name": "enabled-probe", "value": map[string]any{"models": []map[string]string{
			{"name": "A", "alias": "manual-alias"}, {"name": "manual-extra", "alias": "manual-extra"},
		}},
	})
	check(status == 200, "manual model edit fixture failed")
	runRust(1, 5)
	runRust(0, 6)
	for _, kind := range append(append([]string{}, kinds...), "openai-compatibility") {
		status, data := call("GET", kind, nil)
		check(status == 200, "post-sync GET failed")
		old := snapshot[kind].([]any)
		now := data[kind].([]any)
		for i := range old {
			beforeEntry := old[i].(map[string]any)
			afterEntry := now[i].(map[string]any)
			if kind != "gemini-api-key" && kind != "interactions-api-key" {
				check(reflect.DeepEqual(afterEntry["models"], []any{map[string]any{"name": "A", "alias": "A"}}), "wrong final models")
				delete(beforeEntry, "models")
				delete(afterEntry, "models")
			}
			check(reflect.DeepEqual(beforeEntry, afterEntry), "Rust changed unmanaged data or non-model fields")
		}
	}
	check(cpa.Process.Kill() == nil, "stop fixture CPA failed")
	_ = cpa.Wait()
	cpa = exec.Command("/CLIProxyAPI/CLIProxyAPI", "-config", "/tmp/compat-config.yaml")
	cpa.Stdout = io.Discard
	cpa.Stderr = io.Discard
	check(cpa.Start() == nil, "restart fixture CPA failed")
	ready := false
	for i := 0; i < 100; i++ {
		status, _ := call("GET", "openai-compatibility", nil)
		if status == 200 {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	check(ready, "restart readiness failed")
	runRust(0, 6)
	for prefix := range policies {
		policies[prefix] = map[string]any{"exclude": []string{".*"}}
	}
	emptyPolicy, _ := json.Marshal(map[string]any{"cpa_url": "http://127.0.0.1:18317", "client_version": "compat-probe", "channels": policies})
	check(os.WriteFile("/tmp/rust-policy.json", emptyPolicy, 0600) == nil, "empty policy write failed")
	runRust(6, 0)
	runRust(0, 6)
	for _, kind := range append(append([]string{}, kinds...), "openai-compatibility") {
		status, data := call("GET", kind, nil)
		check(status == 200, "empty policy verification GET failed")
		rows := data[kind].([]any)
		for i, row := range rows {
			entry := row.(map[string]any)
			if kind != "gemini-api-key" && kind != "interactions-api-key" {
				models, _ := entry["models"].([]any)
				check(len(models) == 0, "filtered-empty models not visible as zero configured models")
				delete(entry, "models")
			}
			check(reflect.DeepEqual(snapshot[kind].([]any)[i], entry), "filtered-empty altered other fields or unmanaged providers")
		}
	}
	fmt.Println("PASS: real CPA Rust five-kind replacement, second zero writes, non-model/unmanaged preservation and restart persistence")
	fmt.Println("PASS: manual model edits overwritten; filtered-empty configuration verified without changing other fields")
}
