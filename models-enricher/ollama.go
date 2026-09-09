package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// ollama_cloud 经 CPA 的凭据边界查询公开模型详情，同渠道同 lookup id 只请求一次。
func fetchOllamaForChannel(ctx context.Context, cpa *CPAClient, cfg *Config, ch Channel, models []ParsedModel, log *slog.Logger) (map[string]sourceHit, error) {
	out := map[string]sourceHit{}
	chCfg := cfg.Channels[ch.Prefix]
	if chCfg.OllamaNativeBase == "" || !channelUsesOllama(chCfg) {
		return out, nil
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
		name := model.ID // modelsForEnrichment 已移除CPA前缀，保留原始ID内部命名空间。
		if !allowModel(name, chCfg) || !chainHas(sourceChain(chCfg, name), "ollama_cloud") {
			continue
		}
		id := name
		if explicit := chCfg.modelLookupIDs(name)["ollama_cloud"]; explicit != "" {
			id = explicit
		}
		byLookup[id] = append(byLookup[id], name)
	}
	var mu sync.Mutex
	var failed bool
	var wg sync.WaitGroup
	for id, names := range byLookup {
		wg.Add(1)
		go func(id string, names []string) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"model": id})
			response, _, err := cpa.APICall(ctx, ch, "POST", url, auth, body)
			var hit sourceHit
			if err == nil {
				hit, err = parseOllamaMetadata(response)
			}
			if err != nil {
				mu.Lock()
				failed = true
				mu.Unlock()
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
	if failed {
		return nil, errors.New("ollama channel metadata step failed")
	}
	return out, nil
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
	return hit, nil
}
