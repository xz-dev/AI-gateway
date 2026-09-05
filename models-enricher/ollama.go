package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
)

// ollama_cloud：渠道原生按需源。对链中含 ollama_cloud 的每个模型，
// 经 CPA api-call 向该渠道显式配置的 Ollama 原生 endpoint 发 POST /api/show，
// body {"model": <lookup id>}。扫描响应 model_info 中 .context_length 后缀键
// 填 context_window；其他字段留空交给链上下一源。
// 404/报错/缺字段 = 无命中（fail-open + WARN），绝不中断合并。

// fetchOllamaForChannel 返回 canonical 模型名 -> hit。并发经 cpa.pool 统一限流；
// 同渠道同 lookup id 去重。
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

	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[string]struct{}{}
	for _, pm := range models {
		name := stripExactPrefix(pm.ID, ch.Prefix)
		chain := sourceChain(chCfg, name)
		if !chainHas(chain, "ollama_cloud") {
			continue
		}
		id := name
		if v := chCfg.modelLookupIDs(name)["ollama_cloud"]; v != "" {
			id = v
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		wg.Add(1)
		go func(name, id string) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"model": id})
			resp, _, err := cpa.APICall(ctx, ch, "POST", url, auth, body)
			if err != nil {
				log.Warn("ollama /api/show miss", "channel", ch.Name, "model", id, "err", err.Error())
				return
			}
			if cw := parseOllamaContextLength(resp); cw > 0 {
				mu.Lock()
				out[name] = sourceHit{ContextWindow: cw}
				mu.Unlock()
			}
		}(name, id)
	}
	wg.Wait()
	return out
}

// parseOllamaContextLength 扫描 model_info 中所有以 .context_length 结尾的键。
func parseOllamaContextLength(body []byte) int {
	var envelope struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return 0
	}
	for k, v := range envelope.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if n := toInt(v); n > 0 {
				return n
			}
		}
	}
	return 0
}
