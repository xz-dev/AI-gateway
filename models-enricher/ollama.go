package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// ollama_cloud 经 CPA 的凭据边界查询公开模型详情，同渠道同 lookup id 只请求一次。
func fetchOllamaForChannel(ctx context.Context, cpa *CPAClient, cfg *Config, ch Channel, models []ParsedModel, log *slog.Logger) map[string]sourceHit {
	out := map[string]sourceHit{}
	chCfg := cfg.Channels[ch.Prefix]
	if chCfg.OllamaNativeBase == "" || !channelUsesOllama(chCfg) {
		return out
	}
	auth := map[string]string{"Content-Type": "application/json"}
	if ad, err := adapterFor(ch.Type); err == nil {
		for k, v := range ad.auth {
			auth[k] = v
		}
	}
	url := joinURL(chCfg.OllamaNativeBase, "/api/show")
	byLookup := map[string][]string{}
	for _, model := range models {
		name := stripExactPrefix(model.ID, ch.Prefix)
		if !chainHas(sourceChain(chCfg, name), "ollama_cloud") {
			continue
		}
		id := name
		if explicit := chCfg.modelLookupIDs(name)["ollama_cloud"]; explicit != "" {
			id = explicit
		}
		byLookup[id] = append(byLookup[id], name)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for id, names := range byLookup {
		wg.Add(1)
		go func(id string, names []string) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"model": id})
			response, _, err := cpa.APICall(ctx, ch, "POST", url, auth, body)
			if err != nil {
				log.Warn("ollama /api/show miss", "channel", ch.Name, "model", id, "err", err.Error())
				return
			}
			hit, err := parseOllamaMetadata(response)
			if err != nil {
				log.Warn("ollama /api/show miss", "channel", ch.Name, "model", id, "err", err.Error())
				return
			}
			if len(hit) != 0 {
				mu.Lock()
				for _, name := range names {
					out[name] = hit
				}
				mu.Unlock()
			}
		}(id, names)
	}
	wg.Wait()
	return out
}

// 只解释 Ollama 明示的 context_length；保留 model_info 与其余公开详情。
func parseOllamaMetadata(body []byte) (sourceHit, error) {
	var record map[string]any
	if err := decodeJSON(body, &record); err != nil {
		return nil, err
	}
	hit := cloneMap(record)
	info, _ := record["model_info"].(map[string]any)
	keys := make([]string, 0, len(info))
	for key := range info {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasSuffix(key, ".context_length") {
			mapDeclaredField(hit, "context_window", info[key])
		}
	}
	syncOutputAliases(hit)
	return hit, nil
}
