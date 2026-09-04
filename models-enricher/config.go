package main

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

var defaultSourcePriority = []string{"modelparams_subscription", "modelparams_api_key", "models_dev"}

type Config struct {
	CPABaseURL      string                   `yaml:"cpa_base_url"`
	SourceTTL       time.Duration            `yaml:"source_ttl"`
	ChannelTimeout  time.Duration            `yaml:"channel_timeout"`
	OverallDeadline time.Duration            `yaml:"overall_deadline"`
	SourcePriority  []string                 `yaml:"source_priority"`
	Channels        map[string]ChannelConfig `yaml:"channels"`
	StaticModels    []map[string]any         `yaml:"static_models"`
}

type ModelConfig struct {
	SourcePriority []string       `yaml:"source_priority"`
	Overrides      map[string]any `yaml:"overrides"`
}

type ChannelConfig struct {
	Path           string                    `yaml:"path"`
	Include        []string                  `yaml:"include"`
	Exclude        []string                  `yaml:"exclude"`
	Overrides      map[string]map[string]any `yaml:"overrides"`
	SourcePriority []string                  `yaml:"source_priority"`
	Models         map[string]ModelConfig    `yaml:"models"`
	include        []*regexp.Regexp
	exclude        []*regexp.Regexp
}

// modelOverrides：legacy 扁平 overrides 与 models.<name>.overrides 的并集，
// 后者逐 key 优先（models 块是 legacy 的超集语法）。
func (ch ChannelConfig) modelOverrides(name string) map[string]any {
	legacy := ch.Overrides[name]
	mc, ok := ch.Models[name]
	if !ok {
		return legacy
	}
	if len(legacy) == 0 {
		return mc.Overrides
	}
	out := make(map[string]any, len(legacy)+len(mc.Overrides))
	for k, v := range legacy {
		out[k] = v
	}
	for k, v := range mc.Overrides {
		out[k] = v
	}
	return out
}

// sourceChain：三级整体替换 — 模型 > 渠道 > 全局 > 内置默认。绝不 merge。
func (c *Config) sourceChain(ch ChannelConfig, model string) []string {
	if mc, ok := ch.Models[model]; ok && len(mc.SourcePriority) > 0 {
		return mc.SourcePriority
	}
	if len(ch.SourcePriority) > 0 {
		return ch.SourcePriority
	}
	if c != nil && len(c.SourcePriority) > 0 {
		return c.SourcePriority
	}
	return defaultSourcePriority
}

func loadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		SourceTTL:       10 * time.Minute,
		ChannelTimeout:  8 * time.Second,
		OverallDeadline: 25 * time.Second,
		Channels:        map[string]ChannelConfig{},
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}
	if cfg.CPABaseURL == "" {
		return nil, fmt.Errorf("cpa_base_url is required")
	}
	if cfg.SourceTTL <= 0 {
		cfg.SourceTTL = 10 * time.Minute
	}
	if cfg.ChannelTimeout <= 0 {
		cfg.ChannelTimeout = 8 * time.Second
	}
	if cfg.OverallDeadline <= 0 {
		cfg.OverallDeadline = 25 * time.Second
	}
	for name, ch := range cfg.Channels {
		inc, err := compileRegexes(ch.Include)
		if err != nil {
			return nil, fmt.Errorf("channels.%s.include: %w", name, err)
		}
		exc, err := compileRegexes(ch.Exclude)
		if err != nil {
			return nil, fmt.Errorf("channels.%s.exclude: %w", name, err)
		}
		ch.include = inc
		ch.exclude = exc
		cfg.Channels[name] = ch
	}
	return cfg, nil
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

func (c *Config) channelConfig(name, prefix, typ string) ChannelConfig {
	if c == nil {
		return ChannelConfig{}
	}
	for _, key := range []string{name, prefix, typ} {
		if key == "" {
			continue
		}
		if ch, ok := c.Channels[key]; ok {
			return ch
		}
	}
	return ChannelConfig{}
}
