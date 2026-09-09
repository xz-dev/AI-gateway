package main

import (
	"log/slog"
	"regexp"
	"strings"
)

type Manifest struct {
	Models []map[string]any `json:"models"`
	// 单次构建的内部状态，不序列化；故障或OAuth透传记录保持CPA全部字段和值。
	preserveNative map[string]bool
	admitted       map[string]bool
}

// mergeManifest：公开记录逐层组合，路由身份独立于可覆盖的元数据。
// 同步器接管的五类只使用 CPA 当前可见成员，上游目录不再决定集合。
func syncManagedKind(kind string) bool {
	switch kind {
	case "openai-compatibility", "claude-api-key", "codex-api-key", "xai-api-key", "vertex-api-key":
		return true
	}
	return false
}

func modelsForEnrichment(base *Manifest, pack channelModels) []ParsedModel {
	if !syncManagedKind(pack.Channel.Type) && !pack.FetchSkipped {
		if base == nil || base.admitted == nil {
			return pack.Models
		}
		var members []ParsedModel
		for _, model := range pack.Models {
			slug := pack.Channel.Prefix + "/" + model.ID
			if base.admitted[slug] && !base.preserveNative[slug] {
				members = append(members, model)
			}
		}
		return members
	}
	lookup := make(map[string]ParsedModel, len(pack.Models))
	for _, model := range pack.Models {
		lookup[model.ID] = model
	}
	var members []ParsedModel
	if base != nil && pack.Channel.Prefix != "" {
		prefix := pack.Channel.Prefix + "/"
		for _, model := range base.Models {
			slug := asString(model["slug"])
			if !base.preserveNative[slug] && strings.HasPrefix(slug, prefix) {
				name := stripExactPrefix(slug, pack.Channel.Prefix)
				members = append(members, ParsedModel{ID: name, Metadata: lookup[name].Metadata})
			}
		}
	}
	return members
}

// 仅移除明确冲突的默认值；人工覆盖和无法判定的数据保持原义。
func omitConflictingReasoningDefault(entry, overrides map[string]any) {
	if _, explicit := overrides["default_reasoning_level"]; explicit {
		return
	}
	level, ok := entry["default_reasoning_level"].(string)
	if !ok || level == "" {
		return
	}
	levels, ok := entry["supported_reasoning_levels"].([]any)
	if !ok {
		return
	}
	for _, raw := range levels {
		option, ok := raw.(map[string]any)
		if !ok {
			return
		}
		effort, ok := option["effort"].(string)
		if !ok || effort == "" || effort == level {
			return
		}
	}
	delete(entry, "default_reasoning_level")
}

func mergeManifest(base *Manifest, fetched []channelModels, cfg *Config, tables *SourceTables, ollama map[string]map[string]sourceHit) *Manifest {
	bySlug := map[string]map[string]any{}
	baselinePrefixes := map[string]bool{}
	for _, pack := range fetched {
		baselineOnly := pack.FetchSkipped
		for _, model := range modelsForEnrichment(base, pack) {
			if len(sourceChain(cfg.Channels[pack.Channel.Prefix], model.ID)) > 0 {
				baselineOnly = false
				break
			}
		}
		if pack.Failed || baselineOnly {
			baselinePrefixes[pack.Channel.Prefix] = true
		}
	}
	baselineSlug := func(slug string) bool {
		prefix, _, _ := strings.Cut(slug, "/")
		return baselinePrefixes[prefix] || (base != nil && base.preserveNative[slug])
	}
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
			if baselineSlug(slug) {
				if _, exists := bySlug[slug]; !exists {
					put(slug, cloneJSONValue(model).(map[string]any))
				}
				continue
			}
			entry := map[string]any{}
			overlayMetadata(entry, model)
			// 重复 native slug 保持前项优先。
			overlayMetadata(entry, bySlug[slug])
			put(slug, entry)
		}
	}
	enrichSources := func(entry map[string]any, chCfg ChannelConfig, name, prefix string) {
		queries := sourceQueries(chCfg, name)
		for i := len(queries) - 1; i >= 0; i-- {
			q := queries[i]
			if q.token == "ollama_cloud" {
				overlayMetadata(entry, ollama[prefix][q.id])
				continue
			}
			if hit, ok := tables.lookupQuery(q); ok {
				overlayMetadata(entry, hit)
			}
		}
	}
	type reference struct {
		slug      string
		overrides map[string]any
	}
	references := map[string]reference{}
	publicOverrides := map[string]map[string]any{}
	for _, pack := range fetched {
		if baselinePrefixes[pack.Channel.Prefix] {
			continue
		}
		prefix := pack.Channel.Prefix
		chCfg := cfg.Channels[prefix]
		for _, model := range modelsForEnrichment(base, pack) {
			name := model.ID // 已是上游原名，不可再次剥除同名厂商命名空间。
			if !syncManagedKind(pack.Channel.Type) && !allowModel(name, chCfg) {
				continue
			}
			slug := name
			if prefix != "" {
				slug = prefix + "/" + name
			}
			if baselineSlug(slug) {
				continue
			}
			entry := map[string]any{}
			overlayMetadata(entry, bySlug[slug])
			overlayMetadata(entry, model.Metadata)
			enrichSources(entry, chCfg, name, prefix)
			overrides := chCfg.modelOverrides(name)
			overlayMetadata(entry, overrides)
			publicOverrides[slug] = overrides
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
			overlayMetadata(entry, source)
		} else {
			slog.Warn("metadata reference missing", "slug", slug, "source", ref.slug)
		}
		overlayMetadata(entry, ref.overrides)
		put(slug, entry)
	}

	// 引用和当前对象的人工覆盖完成后再判断，避免过早丢失可用默认值。
	for slug, overrides := range publicOverrides {
		omitConflictingReasoningDefault(bySlug[slug], overrides)
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
			overrides := chCfg.modelOverrides(name)
			overlayMetadata(entry, overrides)
			omitConflictingReasoningDefault(entry, overrides)
			customPool[prefix+"/"+name] = entry
		}
	}
	// 按配置顺序物化 static；只能引用公开池、隐藏池或已经完成的 static。
	for _, model := range cfg.StaticModels {
		slug := asString(model["slug"])
		if slug == "" || baselineSlug(slug) {
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
			overlayMetadata(entry, source)
		}
		overrides, _ := model["overrides"].(map[string]any)
		overlayMetadata(entry, overrides)
		omitConflictingReasoningDefault(entry, overrides)
		put(slug, entry)
	}
	models := make([]map[string]any, 0, len(order))
	for _, slug := range order {
		entry := bySlug[slug]
		if !baselineSlug(slug) {
			entry["slug"] = slug
			if _, exists := entry["id"]; exists {
				entry["id"] = slug
			}
		}
		models = append(models, entry)
	}
	return &Manifest{Models: models}
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

// stripExactPrefix 只用于已确认的CPA路由slug；不得传入上游原始模型ID。
func stripExactPrefix(id, prefix string) string {
	id = strings.TrimSpace(id)
	if prefix != "" && strings.HasPrefix(id, prefix+"/") {
		return strings.TrimPrefix(id, prefix+"/")
	}
	return id
}
