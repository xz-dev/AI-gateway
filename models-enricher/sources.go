package main

import (
	"context"
	"log/slog"
	"net/http"
	"reflect"
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

// provider / id 由外层索引持有，值仅为公开模型记录。
type sourceHit = map[string]any

// SourceTables：每请求重建源索引；原始成功响应由读取层缓存。
// 所有表按 provider 命名空间隔离：provider -> 精确模型 id -> hit。
// 无 rank、无跨 provider 首写胜出；原始ID保留，仅查询时允许唯一大小写匹配。
type SourceTables struct {
	dev     map[string]map[string]sourceHit // 显式 ID 保留原始索引。
	devAuto map[string]map[string]sourceHit // 单次 bulk 构建的完整查询身份，已协调 API/flat。
	mpK     map[string]map[string]sourceHit // modelparams authType=api_key
	mpS     map[string]map[string]sourceHit // modelparams authType=subscription
	failed  map[string]bool                 // 共享HTTP来源失败，仅使依赖它的渠道降级。
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
func (t *SourceTables) sourceNamespace(token string) (map[string]map[string]sourceHit, string) {
	if prov, ok := strings.CutPrefix(token, "models.dev/"); ok {
		return t.dev, prov
	}
	if rest, ok := strings.CutPrefix(token, "modelparams.dev/"); ok {
		if prov, ok := strings.CutSuffix(rest, "/api_key"); ok {
			return t.mpK, prov
		}
		if prov, ok := strings.CutSuffix(rest, "/subscription"); ok {
			return t.mpS, prov
		}
	}
	return nil, ""
}

func (t *SourceTables) sourceTable(token string) map[string]sourceHit {
	tables, provider := t.sourceNamespace(token)
	return tables[provider]
}

// lookupOne：单源精确优先，再忽略大小写匹配唯一ID；多个候选时不猜测。
// 显式 ID 不做默认拆分或来源格式推导。
func (t *SourceTables) lookupOne(token, id string) (sourceHit, bool) {
	hit, count := lookupSourceID(t.sourceTable(token), id)
	return hit, count == 1
}

// count=2 表示歧义，不能当成零候选后再选另一个 provider。
func lookupSourceID(table map[string]sourceHit, id string) (sourceHit, int) {
	if hit, exists := table[id]; exists {
		if hit == nil {
			return nil, 2
		}
		return hit, 1
	}
	var found sourceHit
	count := 0
	// ponytail: 非精确查询扫描当前表；目录变大再建立折叠索引。
	for candidate, hit := range table {
		if strings.EqualFold(candidate, id) {
			if count != 0 || hit == nil {
				return nil, 2
			}
			found, count = hit, 1
		}
	}
	return found, count
}

func (t *SourceTables) lookupQuery(q sourceQuery) (sourceHit, bool) {
	if q.explicit {
		return t.lookupOne(q.token, q.id)
	}
	tables, provider := t.sourceNamespace(q.token)
	dev := strings.HasPrefix(q.token, "models.dev/")
	if dev {
		tables = t.devAuto
		if tables == nil {
			// 单端点直接构造的源表；双端点 fetch 路径已经预先协调。
			tables = normalizeModelsDev(t.dev, false)
		}
	}
	var found sourceHit
	count := 0
	for candidate, models := range tables {
		if provider != "" && !strings.EqualFold(candidate, provider) {
			continue
		}
		id := q.id
		if dev {
			id = strings.ToLower(candidate) + "/" + id
		}
		hit, n := lookupSourceID(models, id)
		count += n
		if count > 1 {
			return nil, false
		}
		if n == 1 {
			found = hit
		}
	}
	return found, count == 1
}

// requiredSources 仅启用实际成员与显式custom模型的数据链，不为未配置来源发请求。
func requiredSources(cfg *Config, base *Manifest, fetched []channelModels) map[string]bool {
	needed := map[string]bool{}
	add := func(ch ChannelConfig, name string) {
		if !allowModel(name, ch) {
			return
		}
		for _, token := range sourceChain(ch, name) {
			provider, _, _ := strings.Cut(token, "/")
			needed[provider] = true
		}
	}
	for _, pack := range fetched {
		if pack.Failed {
			continue
		}
		for _, model := range modelsForEnrichment(base, pack) {
			add(cfg.Channels[pack.Channel.Prefix], stripExactPrefix(model.ID, pack.Channel.Prefix))
		}
	}
	for _, ch := range cfg.CustomChannels {
		for name := range ch.Models {
			add(ch, name)
		}
	}
	// 裸模型接管：裸名需要的全局链来源也计入拉取集合。
	if cfg.BareModelsTakeover && base != nil && len(cfg.GlobalSourcePriority) > 0 {
		bare := ChannelConfig{globalChain: cfg.GlobalSourcePriority}
		for _, model := range base.Models {
			if slug := asString(model["slug"]); !strings.Contains(slug, "/") && base.admitted[slug] && !base.preserveNative[slug] {
				add(bare, slug)
			}
		}
	}
	return needed
}

// fetchSources 只拉取已启用的bulk来源；共享失败留给依赖渠道处理。
// 所有请求共享 httpPool 并发上限。
func fetchSources(ctx context.Context, pool *httpPool, needed map[string]bool, log *slog.Logger) *SourceTables {
	t := emptySourceTables()
	var wg sync.WaitGroup
	var devAPI, devFlat map[string]map[string]sourceHit
	var mpK, mpS map[string]map[string]sourceHit
	var apiErr, flatErr, paramsErr error

	wg.Add(3)
	go func() {
		defer wg.Done()
		if !needed["models.dev"] {
			return
		}
		raw, err := sourceGet(ctx, pool, modelsDevAPIURL)
		apiErr = err
		if err != nil {
			log.Warn("source fetch failed", "url", modelsDevAPIURL, "err", err)
			return
		}
		devAPI = indexModelsDev(raw)
	}()
	go func() {
		defer wg.Done()
		if !needed["models.dev"] {
			return
		}
		raw, err := sourceGet(ctx, pool, modelsDevFlatURL)
		flatErr = err
		if err != nil {
			log.Warn("source fetch failed", "url", modelsDevFlatURL, "err", err)
			return
		}
		devFlat = indexModelsDevFlat(raw)
	}()
	go func() {
		defer wg.Done()
		if !needed["modelparams.dev"] {
			return
		}
		raw, err := sourceGet(ctx, pool, modelparamsURL)
		paramsErr = err
		if err != nil {
			log.Warn("source fetch failed", "url", modelparamsURL, "err", err)
			return
		}
		mpK, mpS = indexModelparams(raw, log)
	}()
	wg.Wait()

	// 两个 models.dev 端点属于同一个 source token：api.json 更完整，
	// models.json 只允许补齐 api.json 没有的字段，顺序固定而非按完成先后。
	t.setModelsDev(devAPI, devFlat)
	t.mpK, t.mpS = mpK, mpS
	t.failed = map[string]bool{"models.dev": apiErr != nil || flatErr != nil, "modelparams.dev": paramsErr != nil}
	return t
}

// 只检查该渠道实际成员使用的数据链；查不到某个模型ID是未知字段，不是网络失败。
func (t *SourceTables) channelFailed(ch ChannelConfig, prefix string, models []ParsedModel) bool {
	for _, model := range models {
		name := stripExactPrefix(model.ID, prefix)
		if !allowModel(name, ch) {
			continue
		}
		for _, token := range sourceChain(ch, name) {
			provider, _, _ := strings.Cut(token, "/")
			if t.failed[provider] {
				return true
			}
		}
	}
	return false
}

// 两端点先适配完整身份，再合并；原始索引供不改写的显式 ID 查询使用。
func (t *SourceTables) setModelsDev(api, flat map[string]map[string]sourceHit) {
	t.dev = map[string]map[string]sourceHit{}
	mergeSourceMaps(t.dev, api)
	mergeSourceMaps(t.dev, flat)
	t.devAuto = normalizeModelsDev(api, false)
	mergeSourceMaps(t.devAuto, normalizeModelsDev(flat, true))
}

func normalizeModelsDev(src map[string]map[string]sourceHit, flat bool) map[string]map[string]sourceHit {
	out := map[string]map[string]sourceHit{}
	exact := map[string]map[string]bool{}
	for provider, models := range src {
		provider = strings.ToLower(provider)
		if out[provider] == nil {
			out[provider] = map[string]sourceHit{}
			exact[provider] = map[string]bool{}
		}
		for id, hit := range models {
			// flat 已剥去其完整键的首段；API ID 则可能本来就是完整键。
			if first, rest, ok := strings.Cut(id, "/"); !flat && ok && strings.EqualFold(first, provider) {
				id = rest
			}
			full := provider + "/" + id
			key := strings.ToLower(full)
			isExact := full == key
			previous, exists := out[provider][key]
			if !exists || isExact && !exact[provider][key] {
				out[provider][key], exact[provider][key] = hit, isExact
			} else if isExact == exact[provider][key] && reflect.ValueOf(previous).UnsafePointer() != reflect.ValueOf(hit).UnsafePointer() {
				out[provider][key] = nil // 同层无法唯一协调；不同 API 别名指向同一记录则不重复计数。
			}
		}
	}
	return out
}

func mergeSourceMaps(dst, src map[string]map[string]sourceHit) {
	for provider, models := range src {
		if dst[provider] == nil {
			dst[provider] = map[string]sourceHit{}
		}
		for id, hit := range models {
			high, exists := dst[provider][id]
			if exists && high == nil {
				continue // 高优先级身份歧义不能由低优先级记录裁决。
			}
			if hit == nil {
				if !exists {
					dst[provider][id] = nil
				}
				continue
			}
			merged := map[string]any{}
			overlayMetadata(merged, hit)
			overlayMetadata(merged, dst[provider][id])
			dst[provider][id] = merged
		}
	}
}

func sourceGet(ctx context.Context, pool *httpPool, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	return pool.readJSON(req, 16<<20, true, nil)
}

// indexModelsDev：api.json 天然按 provider 分命名空间；
// m["id"] 别名仅在同命名空间内登记。
func indexModelsDev(raw []byte) map[string]map[string]sourceHit {
	var providers map[string]struct {
		Models map[string]map[string]any `json:"models"`
	}
	out := map[string]map[string]sourceHit{}
	if decodeJSON(raw, &providers) != nil {
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
	if decodeJSON(raw, &models) != nil {
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
	h := cloneMap(m)
	mapDeclaredField(h, "display_name", m["name"])
	mapDeclaredField(h, "context_window", declaredNested(m, "limit", "context"))
	mapDeclaredField(h, "max_input_tokens", declaredNested(m, "limit", "input"))
	mapDeclaredField(h, "max_output_tokens", declaredNested(m, "limit", "output"))
	mapDeclaredField(h, "input_modalities", declaredNested(m, "modalities", "input"))
	mapDeclaredField(h, "output_modalities", declaredNested(m, "modalities", "output"))
	if opts, ok := m["reasoning_options"].([]any); ok {
		for _, opt := range opts {
			option, _ := opt.(map[string]any)
			if firstString(option, "type") == "effort" {
				if values, ok := option["values"].([]any); ok {
					mapDeclaredField(h, "supported_reasoning_levels", effortsToLevels(values))
				}
			}
		}
	}
	return h
}

// 显式 effort 列表的结构转换，不从 reasoning boolean 推导等级。
func effortsToLevels(values []any) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = map[string]any{"effort": cloneJSONValue(value)}
	}
	return out
}

// indexModelparams：空 provider 仅参加无 provider 条件的查询；
// authType 拆分 api_key/subscription，缺失时沿用两边索引。
func indexModelparams(raw []byte, log *slog.Logger) (apiKeyOut, subOut map[string]map[string]sourceHit) {
	apiKeyOut = map[string]map[string]sourceHit{}
	subOut = map[string]map[string]sourceHit{}
	var envelope struct {
		Models []map[string]any `json:"models"`
	}
	if decodeJSON(raw, &envelope) != nil {
		return
	}
	for _, m := range envelope.Models {
		id := firstString(m, "model")
		prov := firstString(m, "provider")
		if id == "" {
			continue
		}
		h := cloneMap(m)
		params, _ := m["params"].([]any)
		for _, p := range params {
			pm, _ := p.(map[string]any)
			path := firstString(pm, "path")
			switch path {
			case "max_completion_tokens", "max_tokens", "max_output_tokens":
				mapDeclaredField(h, path, declaredNested(pm, "range", "max"))
			case "reasoning_effort":
				if values, ok := pm["values"].([]any); ok {
					mapDeclaredField(h, "supported_reasoning_levels", effortsToLevels(values))
				}
				mapDeclaredField(h, "default_reasoning_level", pm["default"])
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
	return apiKeyOut, subOut
}
