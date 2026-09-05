package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// 源 URL 提为变量：测试可指向 httptest 伪服务。
var (
	modelsDevAPIURL  = "https://models.dev/api.json"
	modelsDevFlatURL = "https://models.dev/models.json"
	modelparamsURL   = "https://modelparams.dev/api/v1/models.json"
)

type sourceHit struct {
	DisplayName       string
	ContextWindow     int
	MaxInputTokens    int
	MaxTokens         int
	InputModalities   []string
	ReasoningEfforts  []string
	DefaultReasoning  string
	SupportsReasoning bool
}

// SourceTables：每请求重建一次的源索引，无跨请求缓存。
// 所有表按 provider 命名空间隔离：provider -> 精确模型 id -> hit。
// 无 rank、无跨 provider 首写胜出、无 ID 变体。
type SourceTables struct {
	dev map[string]map[string]sourceHit // models.dev
	mpK map[string]map[string]sourceHit // modelparams authType=api_key
	mpS map[string]map[string]sourceHit // modelparams authType=subscription
}

func emptySourceTables() *SourceTables {
	return &SourceTables{
		dev: map[string]map[string]sourceHit{},
		mpK: map[string]map[string]sourceHit{},
		mpS: map[string]map[string]sourceHit{},
	}
}

// sourceTable 把 provider-qualified token 解析到对应命名空间表。
// ollama_cloud 不是 bulk 源，由调用方单独处理，这里返回 nil。
func (t *SourceTables) sourceTable(token string) map[string]sourceHit {
	if prov, ok := strings.CutPrefix(token, "models.dev/"); ok {
		return t.dev[prov]
	}
	if rest, ok := strings.CutPrefix(token, "modelparams.dev/"); ok {
		if prov, ok := strings.CutSuffix(rest, "/api_key"); ok {
			return t.mpK[prov]
		}
		if prov, ok := strings.CutSuffix(rest, "/subscription"); ok {
			return t.mpS[prov]
		}
	}
	return nil
}

// lookupOne：单源精确查找。id 为 lookup_ids[token]（若配置）否则 canonical ID；
// 不做小写/日期/provider/tag 任何变体。
func (t *SourceTables) lookupOne(token, id string) (sourceHit, bool) {
	table := t.sourceTable(token)
	if table == nil {
		return sourceHit{}, false
	}
	h, ok := table[id]
	return h, ok
}

// fetchSources 拉取 bulk 源并建索引。单源失败 fail-open（WARN + 空表），
// 所有请求共享 httpPool 并发上限。
func fetchSources(ctx context.Context, pool *httpPool, log *slog.Logger) *SourceTables {
	t := emptySourceTables()
	var wg sync.WaitGroup
	var devAPI, devFlat map[string]map[string]sourceHit
	var mpK, mpS map[string]map[string]sourceHit

	wg.Add(3)
	go func() {
		defer wg.Done()
		raw, err := sourceGet(ctx, pool, modelsDevAPIURL)
		if err != nil {
			log.Warn("source fetch failed", "url", modelsDevAPIURL, "err", err)
			return
		}
		devAPI = indexModelsDev(raw)
	}()
	go func() {
		defer wg.Done()
		raw, err := sourceGet(ctx, pool, modelsDevFlatURL)
		if err != nil {
			log.Warn("source fetch failed", "url", modelsDevFlatURL, "err", err)
			return
		}
		devFlat = indexModelsDevFlat(raw)
	}()
	go func() {
		defer wg.Done()
		raw, err := sourceGet(ctx, pool, modelparamsURL)
		if err != nil {
			log.Warn("source fetch failed", "url", modelparamsURL, "err", err)
			return
		}
		mpK, mpS = indexModelparams(raw, log)
	}()
	wg.Wait()

	// 两个 models.dev 端点属于同一个 source token：api.json 更完整，
	// models.json 只允许补齐 api.json 没有的字段，顺序固定而非按完成先后。
	mergeSourceMaps(t.dev, devAPI)
	mergeSourceMaps(t.dev, devFlat)
	t.mpK, t.mpS = mpK, mpS
	return t
}

func mergeSourceMaps(dst, src map[string]map[string]sourceHit) {
	for provider, models := range src {
		if dst[provider] == nil {
			dst[provider] = map[string]sourceHit{}
		}
		for id, hit := range models {
			if current, ok := dst[provider][id]; ok {
				mergeSourceHit(&current, hit)
				dst[provider][id] = current
			} else {
				dst[provider][id] = hit
			}
		}
	}
}

func mergeSourceHit(dst *sourceHit, src sourceHit) {
	if dst.DisplayName == "" {
		dst.DisplayName = src.DisplayName
	}
	if dst.ContextWindow == 0 {
		dst.ContextWindow = src.ContextWindow
	}
	if dst.MaxInputTokens == 0 {
		dst.MaxInputTokens = src.MaxInputTokens
	}
	if dst.MaxTokens == 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if len(dst.InputModalities) == 0 {
		dst.InputModalities = src.InputModalities
	}
	if len(dst.ReasoningEfforts) == 0 {
		dst.ReasoningEfforts = src.ReasoningEfforts
	}
	if dst.DefaultReasoning == "" {
		dst.DefaultReasoning = src.DefaultReasoning
	}
	if !dst.SupportsReasoning {
		dst.SupportsReasoning = src.SupportsReasoning
	}
}

func sourceGet(ctx context.Context, pool *httpPool, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := pool.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// indexModelsDev：api.json 天然按 provider 分命名空间；
// m["id"] 别名仅在同命名空间内登记。
func indexModelsDev(raw []byte) map[string]map[string]sourceHit {
	var providers map[string]struct {
		Models map[string]map[string]any `json:"models"`
	}
	out := map[string]map[string]sourceHit{}
	if json.Unmarshal(raw, &providers) != nil {
		return out
	}
	for name, p := range providers {
		table := map[string]sourceHit{}
		for id, m := range p.Models {
			hit := hitFromModelsDev(m)
			table[id] = hit
			if alt, _ := m["id"].(string); alt != "" {
				table[alt] = hit
			}
		}
		out[name] = table
	}
	return out
}

// indexModelsDevFlat：models.json 平铺键为 "<provider>/<model>"，
// 按首段归命名空间；无 "/" 的键无法定命名空间，跳过（不猜）。
func indexModelsDevFlat(raw []byte) map[string]map[string]sourceHit {
	var models map[string]map[string]any
	out := map[string]map[string]sourceHit{}
	if json.Unmarshal(raw, &models) != nil {
		return out
	}
	for key, m := range models {
		i := strings.Index(key, "/")
		if i <= 0 || i == len(key)-1 {
			continue
		}
		prov, id := key[:i], key[i+1:]
		if out[prov] == nil {
			out[prov] = map[string]sourceHit{}
		}
		out[prov][id] = hitFromModelsDev(m)
	}
	return out
}

func hitFromModelsDev(m map[string]any) sourceHit {
	h := sourceHit{DisplayName: firstString(m, "name")}
	if limit, _ := m["limit"].(map[string]any); limit != nil {
		h.ContextWindow = toInt(limit["context"])
		h.MaxInputTokens = toInt(limit["input"])
		h.MaxTokens = toInt(limit["output"])
	}
	if mods, _ := m["modalities"].(map[string]any); mods != nil {
		h.InputModalities = toStringSlice(mods["input"])
	}
	h.SupportsReasoning, _ = m["reasoning"].(bool)
	if opts, ok := m["reasoning_options"].([]any); ok {
		for _, opt := range opts {
			om, _ := opt.(map[string]any)
			if firstString(om, "type") != "effort" {
				continue
			}
			h.ReasoningEfforts = toStringSlice(om["values"])
		}
	}
	return h
}

// indexModelparams：provider 必填（缺失则跳过并计数 WARN——无命名空间不入索引，
// 不做裸 ID 猜测）；authType 拆分 api_key/subscription，缺失时两边都索引。
func indexModelparams(raw []byte, log *slog.Logger) (apiKeyOut, subOut map[string]map[string]sourceHit) {
	apiKeyOut = map[string]map[string]sourceHit{}
	subOut = map[string]map[string]sourceHit{}
	var envelope struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return
	}
	skipped := 0
	for _, m := range envelope.Models {
		id := firstString(m, "model")
		prov := firstString(m, "provider")
		if id == "" {
			continue
		}
		if prov == "" {
			skipped++
			continue
		}
		h := sourceHit{}
		params, _ := m["params"].([]any)
		for _, p := range params {
			pm, _ := p.(map[string]any)
			switch firstString(pm, "path") {
			case "max_completion_tokens", "max_tokens", "max_output_tokens":
				if rng, _ := pm["range"].(map[string]any); rng != nil {
					if n := toInt(rng["max"]); n > 0 {
						h.MaxTokens = n
					}
				}
				if h.MaxTokens == 0 {
					h.MaxTokens = toInt(pm["default"])
				}
			case "reasoning_effort":
				h.SupportsReasoning = true
				h.ReasoningEfforts = toStringSlice(pm["values"])
				h.DefaultReasoning = firstString(pm, "default")
			}
		}
		put := func(table map[string]map[string]sourceHit) {
			if table[prov] == nil {
				table[prov] = map[string]sourceHit{}
			}
			table[prov][id] = h
		}
		switch strings.ToLower(firstString(m, "authType")) {
		case "api_key":
			put(apiKeyOut)
		case "subscription":
			put(subOut)
		default:
			put(apiKeyOut)
			put(subOut)
		}
	}
	if skipped > 0 {
		log.Warn("modelparams entries skipped: missing provider namespace", "count", skipped)
	}
	return apiKeyOut, subOut
}
