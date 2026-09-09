package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 源 token 只允许 provider-qualified 形式：
//
//	models.dev/<provider-id>
//	modelparams.dev/<provider-id>/api_key
//	modelparams.dev/<provider-id>/subscription
//	ollama_cloud（仅真实 CPA 渠道；custom channel 禁用）
//
// 无全局默认链，无 provider rank，无裸 ID 猜测。
func validSourceToken(token string) bool {
	if token == "ollama_cloud" {
		return true
	}
	if rest, ok := strings.CutPrefix(token, "models.dev/"); ok {
		return rest != "" && !strings.Contains(rest, "/")
	}
	if rest, ok := strings.CutPrefix(token, "modelparams.dev/"); ok {
		for _, suffix := range []string{"/api_key", "/subscription"} {
			if p, ok := strings.CutSuffix(rest, suffix); ok {
				return p != "" && !strings.Contains(p, "/")
			}
		}
	}
	return false
}

type Config struct {
	CPABaseURL        string                   `yaml:"cpa_base_url"`
	HTTPConcurrency   int                      `yaml:"http_concurrency"`
	ChannelTimeout    time.Duration            `yaml:"channel_timeout"`
	OverallDeadline   time.Duration            `yaml:"overall_deadline"`
	Channels          map[string]ChannelConfig `yaml:"channels"`
	CustomChannels    map[string]ChannelConfig `yaml:"custom_channels"`
	StaticModels      []map[string]any         `yaml:"static_models"`
	ProviderPrefixMap yaml.Node                `yaml:"provider_prefix_map"`

	// BareModelsTakeover 开启后，CPA manifest 中无前缀的裸模型（主人手设）
	// 不再被入口过滤，改用全局链动态补全；显式声明（static/custom）仍然覆盖动态结果。
	BareModelsTakeover bool `yaml:"bare_models_takeover"`
	// GlobalSourcePriority 是全局兜底链：模型级 > 渠道级 > 全局。
	GlobalSourcePriority []string `yaml:"source_priority"`

	providerPrefixes providerPrefixes
}

type ModelConfig struct {
	SourcePriority    []string          `yaml:"source_priority"`
	LookupIDs         map[string]string `yaml:"lookup_ids"`
	MetadataFrom      string            `yaml:"metadata_from"`
	Overrides         map[string]any    `yaml:"overrides"`
	ProviderPrefixMap yaml.Node         `yaml:"provider_prefix_map"` // 仅用于拒绝模型级映射。
}

type ChannelConfig struct {
	FetchModels       *bool                     `yaml:"fetch_models"` // nil 默认为 true。
	Path              string                    `yaml:"path"`
	Include           []string                  `yaml:"include"`
	Exclude           []string                  `yaml:"exclude"`
	Overrides         map[string]map[string]any `yaml:"overrides"`
	SourcePriority    []string                  `yaml:"source_priority"`
	OllamaNativeBase  string                    `yaml:"ollama_native_base_url"`
	Models            map[string]ModelConfig    `yaml:"models"`
	ProviderPrefixMap yaml.Node                 `yaml:"provider_prefix_map"`

	// globalChain 是 loadConfig 注入的全局兜底链（yaml:"-"）；仅当模型级与渠道级均未
	// 显式配置时生效。显式 source_priority: [] 关闭继承。
	globalChain []string `yaml:"-"`

	providerPrefixes providerPrefixes
	include          []*regexp.Regexp
	exclude          []*regexp.Regexp
}

func (ch ChannelConfig) fetchModelsEnabled() bool {
	return ch.FetchModels == nil || *ch.FetchModels
}

// modelOverrides：模型配置在 legacy 平铺配置之上叠层，null 不清除下层。
func (ch ChannelConfig) modelOverrides(name string) map[string]any {
	out := map[string]any{}
	overlayMetadata(out, ch.Overrides[name])
	overlayMetadata(out, ch.Models[name].Overrides)
	return out
}

// modelLookupIDs：某模型的 source-token→查询ID 映射（无配置时各源用 canonical ID）。
func (ch ChannelConfig) modelLookupIDs(name string) map[string]string {
	if mc, ok := ch.Models[name]; ok {
		return mc.LookupIDs
	}
	return nil
}

// modelMetadataFrom 返回显式跨渠道 metadata 引用；引用完整输出 slug，不猜测。
func (ch ChannelConfig) modelMetadataFrom(name string) string {
	if mc, ok := ch.Models[name]; ok {
		return strings.TrimSpace(mc.MetadataFrom)
	}
	return ""
}

// sourceChain：三级整体替换 — 模型 > 渠道 > 全局（globalChain，loadConfig 注入）。
// 空链表示不启用外部来源；显式模型/渠道空链（`[]`）可以关闭全局兜底。
func sourceChain(ch ChannelConfig, model string) []string {
	if mc, ok := ch.Models[model]; ok && mc.SourcePriority != nil {
		return mc.SourcePriority
	}
	if ch.SourcePriority != nil {
		return ch.SourcePriority
	}
	return ch.globalChain
}

func chainHas(chain []string, token string) bool {
	for _, t := range chain {
		if t == token {
			return true
		}
	}
	return false
}

// channelUsesOllama：渠道链或任一模型链含 ollama_cloud。
func channelUsesOllama(ch ChannelConfig) bool {
	if chainHas(ch.SourcePriority, "ollama_cloud") {
		return true
	}
	for _, mc := range ch.Models {
		if chainHas(mc.SourcePriority, "ollama_cloud") {
			return true
		}
	}
	return false
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		HTTPConcurrency: 8,
		ChannelTimeout:  8 * time.Second,
		OverallDeadline: 25 * time.Second,
		Channels:        map[string]ChannelConfig{},
		CustomChannels:  map[string]ChannelConfig{},
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}
	cfg.providerPrefixes, err = parseProviderPrefixes(cfg.ProviderPrefixMap, "provider_prefix_map")
	if err != nil {
		return nil, err
	}
	if cfg.CPABaseURL == "" {
		return nil, fmt.Errorf("cpa_base_url is required")
	}
	if cfg.HTTPConcurrency <= 0 {
		cfg.HTTPConcurrency = 8
	}
	if cfg.ChannelTimeout <= 0 {
		cfg.ChannelTimeout = 8 * time.Second
	}
	if cfg.OverallDeadline <= 0 {
		cfg.OverallDeadline = 25 * time.Second
	}
	// 全局链 token 校验与渠道链同规则；ollama_cloud 只能在具体渠道声明。
	for _, token := range cfg.GlobalSourcePriority {
		if !validSourceToken(token) || token == "ollama_cloud" {
			return nil, fmt.Errorf("source_priority: invalid global source token %q", token)
		}
	}
	for key := range cfg.Channels {
		if _, dup := cfg.CustomChannels[key]; dup {
			return nil, fmt.Errorf("channels.%s: also defined as custom channel", key)
		}
	}
	for name, ch := range cfg.Channels {
		ch, err := finishChannelConfig("channels."+name, ch, false)
		if err != nil {
			return nil, err
		}
		ch.providerPrefixes = mergeProviderPrefixes(cfg.providerPrefixes, ch.providerPrefixes)
		ch.globalChain = cfg.GlobalSourcePriority
		cfg.Channels[name] = ch
	}
	for name, ch := range cfg.CustomChannels {
		if name == "" {
			return nil, fmt.Errorf("custom_channels: empty channel key")
		}
		if len(ch.SourcePriority) == 0 {
			// 虚空创造池：无来源合法，但每个成员必须内联声明完整元数据。
			for modelName, model := range ch.Models {
				if len(model.Overrides) == 0 {
					return nil, fmt.Errorf("custom_channels.%s.%s: source-free pool members must declare inline overrides", name, modelName)
				}
			}
		}
		ch, err := finishChannelConfig("custom_channels."+name, ch, true)
		if err != nil {
			return nil, err
		}
		ch.providerPrefixes = mergeProviderPrefixes(cfg.providerPrefixes, ch.providerPrefixes)
		ch.globalChain = cfg.GlobalSourcePriority
		cfg.CustomChannels[name] = ch
	}
	for i, sm := range cfg.StaticModels {
		if _, ok := sm["provider_prefix_map"]; ok {
			return nil, fmt.Errorf("static_models[%d]: provider_prefix_map is only allowed globally or on channels", i)
		}
		if _, ok := sm["source_priority"]; ok {
			return nil, fmt.Errorf("static_models[%d] (%v): source_priority is not allowed; use custom_channels", i, sm["slug"])
		}
		if _, ok := sm["lookup_ids"]; ok {
			return nil, fmt.Errorf("static_models[%d] (%v): lookup_ids is not allowed; use custom_channels", i, sm["slug"])
		}
		for _, ref := range inheritList(sm["inherit"]) {
			if strings.HasPrefix(ref, "id@") {
				return nil, fmt.Errorf("static_models[%d] (%v): id@ inherit is removed; use custom_channels pool slug", i, sm["slug"])
			}
		}
	}
	return cfg, nil
}

// finishChannelConfig：编译正则并校验所有链 token。custom=true 时禁止 ollama_cloud
// 与 ollama_native_base_url（ollama_cloud 必须有真实 CPA 渠道作凭证边界）。
func finishChannelConfig(where string, ch ChannelConfig, custom bool) (ChannelConfig, error) {
	prefixes, err := parseProviderPrefixes(ch.ProviderPrefixMap, where+".provider_prefix_map")
	if err != nil {
		return ch, err
	}
	ch.providerPrefixes = prefixes
	inc, err := compileRegexes(ch.Include)
	if err != nil {
		return ch, fmt.Errorf("%s.include: %w", where, err)
	}
	exc, err := compileRegexes(ch.Exclude)
	if err != nil {
		return ch, fmt.Errorf("%s.exclude: %w", where, err)
	}
	ch.include = inc
	ch.exclude = exc
	if custom && ch.OllamaNativeBase != "" {
		return ch, fmt.Errorf("%s: ollama_native_base_url is meaningless on a custom channel", where)
	}
	checkChain := func(chain []string, at string) error {
		for _, token := range chain {
			if !validSourceToken(token) {
				return fmt.Errorf("%s: invalid source token %q", at, token)
			}
			if custom && token == "ollama_cloud" {
				return fmt.Errorf("%s: ollama_cloud requires a real CPA channel", at)
			}
		}
		return nil
	}
	if err := checkChain(ch.SourcePriority, where+".source_priority"); err != nil {
		return ch, err
	}
	for model, mc := range ch.Models {
		if mc.ProviderPrefixMap.Kind != 0 {
			return ch, fmt.Errorf("%s.models.%s.provider_prefix_map: model-level mapping is not allowed", where, model)
		}
		if err := checkChain(mc.SourcePriority, where+".models."+model+".source_priority"); err != nil {
			return ch, err
		}
		if ref := strings.TrimSpace(mc.MetadataFrom); ref != "" {
			parts := strings.Split(ref, "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return ch, fmt.Errorf("%s.models.%s.metadata_from: must be a full channel/model slug", where, model)
			}
		}
		for token := range mc.LookupIDs {
			if !validSourceToken(token) {
				return ch, fmt.Errorf("%s.models.%s.lookup_ids: invalid source token %q", where, model, token)
			}
			if custom && token == "ollama_cloud" {
				return ch, fmt.Errorf("%s.models.%s.lookup_ids: ollama_cloud requires a real CPA channel", where, model)
			}
		}
	}
	return ch, nil
}

func compileRegexes(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}
