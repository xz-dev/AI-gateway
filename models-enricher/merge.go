package main

import (
	"log/slog"
	"regexp"
	"strings"
)

type Manifest struct {
	Models []map[string]any `json:"models"`
}

// mergeManifest：公开记录逐层组合，路由身份独立于可覆盖的元数据。
func mergeManifest(base *Manifest, fetched []channelModels, cfg *Config, tables *SourceTables, ollama map[string]map[string]sourceHit) *Manifest {
	bySlug := map[string]map[string]any{}
	order := []string{}
	put := func(slug string, entry map[string]any) {
		if _, exists := bySlug[slug]; !exists {
			order = append(order, slug)
		}
		bySlug[slug] = entry
	}
	if base != nil {
		for _, model := range base.Models {
			slug := asString(model["slug"])
			if !strings.Contains(slug, "/") {
				continue
			}
			entry := map[string]any{}
			applyModelLayer(entry, model)
			// 重复 native slug 保持前项优先。
			applyModelLayer(entry, bySlug[slug])
			put(slug, entry)
		}
	}
	enrichSources := func(entry map[string]any, chCfg ChannelConfig, name, prefix string) {
		chain := sourceChain(chCfg, name)
		lookupIDs := chCfg.modelLookupIDs(name)
		for i := len(chain) - 1; i >= 0; i-- {
			token := chain[i]
			if token == "ollama_cloud" {
				applyModelLayer(entry, ollama[prefix][name])
				continue
			}
			id := name
			if explicit := lookupIDs[token]; explicit != "" {
				id = explicit
			}
			if hit, ok := tables.lookupOne(token, id); ok {
				applyModelLayer(entry, hit)
			}
		}
	}
	type reference struct {
		slug      string
		overrides map[string]any
	}
	references := map[string]reference{}
	for _, pack := range fetched {
		prefix := pack.Channel.Prefix
		chCfg := cfg.Channels[prefix]
		for _, model := range pack.Models {
			name := stripExactPrefix(model.ID, prefix)
			if !allowModel(name, chCfg) {
				continue
			}
			slug := name
			if prefix != "" {
				slug = prefix + "/" + name
			}
			entry := map[string]any{}
			applyModelLayer(entry, bySlug[slug])
			applyModelLayer(entry, model.Metadata)
			enrichSources(entry, chCfg, name, prefix)
			overrides := chCfg.modelOverrides(name)
			applyModelLayer(entry, overrides)
			if ref := chCfg.modelMetadataFrom(name); ref != "" {
				references[slug] = reference{slug: ref, overrides: overrides}
			}
			put(slug, entry)
		}
	}
	// 快照只借用已经完成的对象；引用写入新对象，不修改快照中的任何节点。
	snapshot := make(map[string]map[string]any, len(bySlug))
	for slug, entry := range bySlug {
		snapshot[slug] = entry
	}
	for slug, ref := range references {
		entry := cloneMap(snapshot[slug])
		if source, exists := snapshot[ref.slug]; exists {
			applyModelLayer(entry, source)
		} else {
			slog.Warn("metadata reference missing", "slug", slug, "source", ref.slug)
		}
		applyModelLayer(entry, ref.overrides)
		put(slug, entry)
	}

	// custom 只作为内部继承池，不进入公开清单。
	customPool := map[string]map[string]any{}
	for prefix, chCfg := range cfg.CustomChannels {
		for name := range chCfg.Models {
			if !allowModel(name, chCfg) {
				continue
			}
			entry := map[string]any{}
			enrichSources(entry, chCfg, name, "")
			applyModelLayer(entry, chCfg.modelOverrides(name))
			customPool[prefix+"/"+name] = entry
		}
	}
	// 按配置顺序物化 static；只能引用公开池、隐藏池或已经完成的 static。
	for _, model := range cfg.StaticModels {
		slug := asString(model["slug"])
		if slug == "" {
			continue
		}
		entry := cloneMap(bySlug[slug])
		refs := inheritList(model["inherit"])
		for i := len(refs) - 1; i >= 0; i-- {
			source, exists := bySlug[refs[i]]
			if !exists {
				source, exists = customPool[refs[i]]
			}
			if !exists {
				slog.Warn("static inherit target missing", "slug", slug, "target", refs[i])
				continue
			}
			applyModelLayer(entry, source)
		}
		overrides, _ := model["overrides"].(map[string]any)
		applyModelLayer(entry, overrides)
		put(slug, entry)
	}
	models := make([]map[string]any, 0, len(order))
	for _, slug := range order {
		entry := bySlug[slug]
		entry["slug"] = slug
		if _, exists := entry["id"]; exists {
			entry["id"] = slug
		}
		models = append(models, entry)
	}
	return &Manifest{Models: models}
}

// 模型层边界只额外对齐明确的输出上限别名，不解释任意嵌套对象。
func applyModelLayer(dst, src map[string]any) {
	overlayMetadata(dst, src)
	alignOutputAliases(dst, src)
}

func syncOutputAliases(m map[string]any) { alignOutputAliases(m, m) }

func alignOutputAliases(dst, src map[string]any) {
	value := src["max_output_tokens"]
	if value == nil {
		value = src["max_tokens"]
	}
	if value != nil {
		dst["max_output_tokens"] = cloneJSONValue(value)
		dst["max_tokens"] = cloneJSONValue(value)
	}
}

// inheritList 接受单个 slug 或列表，归一为列表。
func inheritList(value any) []string {
	switch value := value.(type) {
	case string:
		if value != "" {
			return []string{value}
		}
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func allowModel(name string, cfg ChannelConfig) bool {
	if len(cfg.include) > 0 && !anyMatch(cfg.include, name) {
		return false
	}
	return !anyMatch(cfg.exclude, name)
}

func anyMatch(patterns []*regexp.Regexp, name string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(name) {
			return true
		}
	}
	return false
}

// stripExactPrefix 仅剥除精确匹配的渠道前缀，不猜测 provider、末段或 tag。
func stripExactPrefix(id, prefix string) string {
	id = strings.TrimSpace(id)
	if prefix != "" && strings.HasPrefix(id, prefix+"/") {
		return strings.TrimPrefix(id, prefix+"/")
	}
	return id
}
