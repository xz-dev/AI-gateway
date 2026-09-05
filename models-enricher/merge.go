package main

import (
	"log/slog"
	"regexp"
	"slices"
	"strings"
)

type Manifest struct {
	Models []map[string]any `json:"models"`
}

type ReasoningLevel struct {
	Effort string `json:"effort"`
}

// mergeManifest：native（权威基线，含 OAuth）+ 渠道抓取 + sources/ollama 富化
// + custom 隐藏池 + static 组合。输入均假定已过 validateRuntime。
func mergeManifest(base *Manifest, fetched []channelModels, cfg *Config, tables *SourceTables, ollama map[string]map[string]sourceHit) *Manifest {
	bySlug := map[string]map[string]any{}
	order := []string{}
	put := func(m map[string]any) {
		slug := asString(m["slug"])
		if slug == "" {
			return
		}
		if _, ok := bySlug[slug]; !ok {
			order = append(order, slug)
			bySlug[slug] = m
			return
		}
		fillGaps(bySlug[slug], m)
	}
	if base != nil {
		for _, m := range base.Models {
			slug := asString(m["slug"])
			// CPA native manifest 的裸名模型不进合并池。
			if !strings.Contains(slug, "/") {
				continue
			}
			c := cloneMap(m)
			stripCodexTemplateJunk(c)
			put(c)
		}
	}
	metadataRefs := map[string]string{}
	clearCapabilityFields := func(entry map[string]any) {
		for _, key := range []string{"context_window", "max_input_tokens", "max_tokens", "input_modalities", "supported_reasoning_levels", "default_reasoning_level"} {
			delete(entry, key)
		}
	}
	copyMetadata := func(dst, src map[string]any) {
		if value, ok := src["display_name"]; ok {
			dst["display_name"] = value
		}
		for _, key := range []string{"context_window", "max_input_tokens", "max_tokens", "input_modalities", "supported_reasoning_levels", "default_reasoning_level"} {
			if value, ok := src[key]; ok {
				dst[key] = value
			} else {
				delete(dst, key)
			}
		}
	}
	// enrichModel：渠道/源/overrides 三段式富化，渠道与 custom 共用。
	enrichModel := func(entry map[string]any, chCfg ChannelConfig, name, channelName string) {
		chain := sourceChain(chCfg, name)
		if len(chain) > 0 {
			clearCapabilityFields(entry)
		}
		lookupIDs := chCfg.modelLookupIDs(name)
		for _, token := range chain {
			if token == "ollama_cloud" {
				if hits, ok := ollama[channelName]; ok {
					if h, hit := hits[name]; hit {
						applySource(entry, h)
					}
				}
				continue
			}
			id := name
			if v := lookupIDs[token]; v != "" {
				id = v
			}
			if h, ok := tables.lookupOne(token, id); ok {
				applySource(entry, h)
			}
		}
		if ov := chCfg.modelOverrides(name); ov != nil {
			for k, v := range ov {
				entry[k] = v
			}
		}
	}
	for _, pack := range fetched {
		chCfg := cfg.Channels[pack.Channel.Prefix] // validateRuntime 已保证存在
		prefix := pack.Channel.Prefix
		for _, pm := range pack.Models {
			name := stripExactPrefix(pm.ID, prefix)
			if !allowModel(name, chCfg) {
				continue
			}
			slug := name
			if prefix != "" {
				slug = prefix + "/" + name
			}
			entry := map[string]any{"slug": slug}
			applyParsed(entry, pm)
			enrichModel(entry, chCfg, name, pack.Channel.Prefix)
			if ref := chCfg.modelMetadataFrom(name); ref != "" {
				metadataRefs[slug] = ref
			}
			if existing, ok := bySlug[slug]; ok {
				if len(sourceChain(chCfg, name)) > 0 {
					clearCapabilityFields(existing)
				}
				fillGaps(existing, entry)
				if ov := chCfg.modelOverrides(name); ov != nil {
					for k, v := range ov {
						existing[k] = v
					}
				}
				continue
			}
			put(entry)
		}
	}
	for slug, ref := range metadataRefs {
		dst, dstOK := bySlug[slug]
		src, srcOK := bySlug[ref]
		if !dstOK || !srcOK {
			slog.Warn("metadata reference missing", "slug", slug, "source", ref)
			if dstOK {
				clearCapabilityFields(dst)
			}
			continue
		}
		copyMetadata(dst, src)
	}
	// custom_channels：内部隐藏 metadata pool。只进 customPool，不进公开输出；
	// 供 static_models 以普通 pool slug 继承。
	customPool := map[string]map[string]any{}
	for cname, chCfg := range cfg.CustomChannels {
		for modelID := range chCfg.Models {
			if !allowModel(modelID, chCfg) {
				continue
			}
			entry := map[string]any{}
			enrichModel(entry, chCfg, modelID, "")
			customPool[cname+"/"+modelID] = entry
		}
	}
	// static_models：合并池内原位物化（输出构建之前）。按 YAML 顺序处理，
	// 每个算完即入池，后续 static 可 inherit 它（顺序解析天然防环）。
	// inherit 目标解析顺序：公开池 → custom 隐藏池；缺失=跳过+WARN；
	// overrides 永远最后盖顶。id@ 已在 loadConfig 拒绝。
	// 命中已有 slug（如 native 裸条目）时原位替换：每个 slug 输出一行。
	for _, sm := range cfg.StaticModels {
		slug := asString(sm["slug"])
		if slug == "" {
			continue
		}
		entry := map[string]any{}
		for _, ref := range inheritList(sm["inherit"]) {
			src, ok := bySlug[ref]
			if !ok {
				src, ok = customPool[ref]
			}
			if !ok {
				slog.Warn("static inherit target missing", "slug", slug, "target", ref)
				continue
			}
			// 与源链一致的首命中逐字段胜出：列表越靠前优先级越高，
			// 后续 ref 仅填补空缺。列表序应排为 [custom 池, 公开池]。
			for k, v := range src {
				if k == "slug" {
					continue
				}
				if missing(entry, k) {
					entry[k] = v
				}
			}
		}
		if ov, ok := sm["overrides"].(map[string]any); ok {
			for k, v := range ov {
				entry[k] = v
			}
		}
		entry["slug"] = slug
		// 与已有 slug 冲突时：static 字段优先，池内已有字段仅填补空缺。
		if existing, ok := bySlug[slug]; ok {
			fillGaps(entry, existing)
		}
		ensureDefaults(entry)
		bySlug[slug] = entry
		if !slices.Contains(order, slug) {
			order = append(order, slug)
		}
	}
	models := make([]map[string]any, 0, len(order))
	for _, slug := range order {
		m := bySlug[slug]
		ensureDefaults(m)
		models = append(models, m)
	}
	return &Manifest{Models: models}
}

// codexCatalogSlugs 是 CPA codex 目录真实型号（按 slug 末段匹配）。
// 这些型号的模板字段即真值，豁免 stripCodexTemplateJunk。
var codexCatalogSlugs = map[string]struct{}{
	"gpt-5.6-sol": {}, "gpt-5.6-terra": {}, "gpt-5.6-luna": {},
	"gpt-5.5": {}, "gpt-5.4": {}, "gpt-5.4-mini": {},
	"gpt-5.3-codex-spark": {}, "codex-auto-review": {},
}

// codexTemplateCtx 是 CPA rich 端点默认模板（gpt-5.5 系）的 context_window。
// CPA 的 buildCodexClientModels 对所有不在 codex 目录的模型克隆该模板，
// 使 glm/kimi 等模型携带捏造的 272000（及模板 max_tokens/reasoning levels）。
const codexTemplateCtx = 272000

// stripCodexTemplateJunk 剥除模板克隆捏造的展示元数据字段。
// 判据：非 codex 目录型号且 context_window 恰为模板值。
// 剥后字段由 sources（fetched 路径 fillGaps）与 statics 重建真值；
// 客户端契约字段（shell_type/service_tiers 等）保留不动。
func stripCodexTemplateJunk(m map[string]any) {
	slug := asString(m["slug"])
	last := slug
	if i := strings.LastIndex(last, "/"); i >= 0 {
		last = last[i+1:]
	}
	if _, ok := codexCatalogSlugs[last]; ok {
		return
	}
	if toInt(m["context_window"]) != codexTemplateCtx {
		return
	}
	delete(m, "context_window")
	delete(m, "max_input_tokens")
	delete(m, "max_tokens")
	delete(m, "supported_reasoning_levels")
	delete(m, "default_reasoning_level")
}

// stringList 归一 YAML 列表字段为 []string。
func stringList(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := asString(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// inheritList 接受单个 slug 或 slug 列表，归一为列表。
func inheritList(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := asString(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func applyParsed(dst map[string]any, pm ParsedModel) {
	if pm.DisplayName != "" {
		dst["display_name"] = pm.DisplayName
	}
	if pm.ContextWindow > 0 {
		dst["context_window"] = pm.ContextWindow
	}
	if pm.MaxInputTokens > 0 {
		dst["max_input_tokens"] = pm.MaxInputTokens
	}
	if pm.MaxTokens > 0 {
		dst["max_tokens"] = pm.MaxTokens
	}
	if len(pm.InputModalities) > 0 {
		dst["input_modalities"] = pm.InputModalities
	}
	if len(pm.ReasoningEfforts) > 0 {
		dst["supported_reasoning_levels"] = effortsToLevels(pm.ReasoningEfforts)
	}
	if pm.DefaultReasoning != "" {
		dst["default_reasoning_level"] = pm.DefaultReasoning
	}
}

// applySource 使用 source chain 作为 capability 真值；渠道返回的 capability
// 可能是错误模板值，因此 source 命中时覆盖已有字段，未命中则保持原值。
func applySource(dst map[string]any, h sourceHit) {
	if h.DisplayName != "" && missing(dst, "display_name") {
		dst["display_name"] = h.DisplayName
	}
	if h.ContextWindow > 0 && missing(dst, "context_window") {
		dst["context_window"] = h.ContextWindow
	}
	if h.MaxInputTokens > 0 && missing(dst, "max_input_tokens") {
		dst["max_input_tokens"] = h.MaxInputTokens
	}
	if h.MaxTokens > 0 && missing(dst, "max_tokens") {
		dst["max_tokens"] = h.MaxTokens
	}
	if len(h.InputModalities) > 0 && missing(dst, "input_modalities") {
		dst["input_modalities"] = h.InputModalities
	}
	if len(h.ReasoningEfforts) > 0 && missing(dst, "supported_reasoning_levels") {
		dst["supported_reasoning_levels"] = effortsToLevels(h.ReasoningEfforts)
	}
	if h.DefaultReasoning != "" && missing(dst, "default_reasoning_level") {
		dst["default_reasoning_level"] = h.DefaultReasoning
	}
}

func fillGaps(dst, src map[string]any) {
	for k, v := range src {
		if k == "slug" {
			continue
		}
		if missing(dst, k) {
			dst[k] = v
		}
	}
}

func ensureDefaults(m map[string]any) {
	if missing(m, "display_name") {
		slug := asString(m["slug"])
		if i := strings.LastIndex(slug, "/"); i >= 0 {
			m["display_name"] = slug[i+1:]
		} else {
			m["display_name"] = slug
		}
	}
	if missing(m, "input_modalities") {
		m["input_modalities"] = []string{"text"}
	}
	if missing(m, "supported_reasoning_levels") {
		m["supported_reasoning_levels"] = []ReasoningLevel{{Effort: "none"}}
	}
	if missing(m, "default_reasoning_level") {
		m["default_reasoning_level"] = "none"
	}
	// 双发 max_output_tokens：Sub2API 等下游只认该字段（codex 风格为 max_tokens）。
	if !missing(m, "max_tokens") && missing(m, "max_output_tokens") {
		m["max_output_tokens"] = m["max_tokens"]
	}
}

func effortsToLevels(efforts []string) []ReasoningLevel {
	out := make([]ReasoningLevel, 0, len(efforts))
	for _, e := range efforts {
		out = append(out, ReasoningLevel{Effort: e})
	}
	return out
}

func allowModel(name string, cfg ChannelConfig) bool {
	if len(cfg.include) > 0 && !anyMatch(cfg.include, name) {
		return false
	}
	if anyMatch(cfg.exclude, name) {
		return false
	}
	return true
}

func anyMatch(res []*regexp.Regexp, name string) bool {
	for _, re := range res {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// stripExactPrefix：仅在精确命中 "<prefix>/" 前缀时剥除；不做末段猜测。
func stripExactPrefix(id, prefix string) string {
	id = strings.TrimSpace(id)
	if prefix != "" && strings.HasPrefix(id, prefix+"/") {
		return strings.TrimPrefix(id, prefix+"/")
	}
	return id
}

func missing(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	case []ReasoningLevel:
		return len(t) == 0
	default:
		return false
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
