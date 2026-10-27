// Synthetic CPA for resource measurements; run in a separate isolated container.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

func main() {
	const count = 10000
	records := make([]map[string]string, count)
	for i := range records {
		records[i] = map[string]string{"id": fmt.Sprintf("model-%05d", i), "padding": strings.Repeat("x", 3000)}
	}
	raw, _ := json.Marshal(map[string]any{"data": records})
	inventory, _ := json.Marshal(map[string]any{"status_code": 200, "body": string(raw)})
	fmt.Printf("inventory records=%d encoded_bytes=%d channels=2\n", count, len(inventory))
	var lock sync.Mutex
	models := map[string]any{"p0": []map[string]string{{"name": "old", "alias": "old"}}, "p1": []map[string]string{{"name": "old", "alias": "old"}}}
	http.HandleFunc("/v0/management/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-management" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		kind := strings.TrimPrefix(r.URL.Path, "/v0/management/")
		if r.Method == "POST" && kind == "api-call" {
			w.Write(inventory)
			return
		}
		lock.Lock()
		defer lock.Unlock()
		if r.Method == "PATCH" && kind == "openai-compatibility" {
			var patch struct {
				Name  string `json:"name"`
				Value struct {
					Models any `json:"models"`
				} `json:"value"`
			}
			if json.NewDecoder(r.Body).Decode(&patch) != nil {
				w.WriteHeader(400)
				return
			}
			if _, ok := models[patch.Name]; !ok {
				w.WriteHeader(404)
				return
			}
			models[patch.Name] = patch.Value.Models
			fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		rows := []any{}
		if kind == "openai-compatibility" {
			for _, name := range []string{"p0", "p1"} {
				rows = append(rows, map[string]any{"name": name, "prefix": name, "base-url": "https://upstream.invalid/v1", "api-key-entries": []map[string]string{{"auth-index": name}}, "models": models[name]})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{kind: rows})
	})
	if err := http.ListenAndServe(":8317", nil); err != nil {
		panic(err)
	}
}
