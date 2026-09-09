package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

func main() {
	const key = "synthetic-management-only"
	cfg := `host: "127.0.0.1"
port: 18317
remote-management:
  allow-remote: false
  secret-key: synthetic-management-only
  disable-control-panel: true
  disable-auto-update: true
auth-dir: /tmp/auth
api-keys: [synthetic-client-only]
openai-compatibility:
  - name: enabled
    prefix: enabled
    base-url: https://example.invalid/v1
    api-key-entries: [{api-key: synthetic-enabled}]
    models: [{name: old, alias: old}]
  - name: disabled
    prefix: disabled
    disabled: true
    base-url: https://example.invalid/v1
    api-key-entries: [{api-key: synthetic-disabled}]
    models: [{name: old, alias: old}]
`
	if err := os.WriteFile("/tmp/config.yaml", []byte(cfg), 0600); err != nil {
		panic(err)
	}
	c := exec.Command("/CLIProxyAPI/CLIProxyAPI", "-config", "/tmp/config.yaml")
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	if err := c.Start(); err != nil {
		panic(err)
	}
	defer func() { c.Process.Kill(); c.Wait() }()
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method string, payload any) (int, map[string]any) {
		raw, _ := json.Marshal(payload)
		r, _ := http.NewRequest(method, "http://127.0.0.1:18317/v0/management/openai-compatibility", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		response, err := client.Do(r)
		if err != nil {
			return 0, nil
		}
		defer response.Body.Close()
		var data map[string]any
		json.NewDecoder(response.Body).Decode(&data)
		return response.StatusCode, data
	}
	for i := 0; i < 100; i++ {
		s, _ := call("GET", nil)
		if s == 200 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	failures := 0
	for round := 0; round < 10; round++ {
		old := fmt.Sprintf("old-%d", round)
		wanted := fmt.Sprintf("new-%d", round)
		patch := func(name, model string) {
			s, _ := call("PATCH", map[string]any{"name": name, "value": map[string]any{"models": []map[string]string{{"name": model, "alias": model}}}})
			if s != 200 {
				panic("PATCH failed")
			}
		}
		patch("enabled", old)
		patch("disabled", old)
		time.Sleep(200 * time.Millisecond) // Only settle fixture setup, not application writes.
		var wg sync.WaitGroup
		for _, name := range []string{"enabled", "disabled"} {
			wg.Add(1)
			go func(n string) { defer wg.Done(); patch(n, wanted) }(name)
		}
		wg.Wait()
		s, data := call("GET", nil)
		if s != 200 {
			panic("GET failed")
		}
		for _, row := range data["openai-compatibility"].([]any) {
			entry := row.(map[string]any)
			models := entry["models"].([]any)
			actual := models[0].(map[string]any)["name"]
			if actual != wanted {
				failures++
				fmt.Printf("round=%d channel=%s wanted=%s actual=%v\n", round, entry["name"], wanted, actual)
			}
		}
	}
	fmt.Printf("pure Go concurrent PATCH failures=%d\n", failures)
	if failures > 0 {
		os.Exit(1)
	}
}
