package main

import (
	"fmt"
	"strings"
)

// configError：运行时配置/身份校验失败。code 供客户端机器识别，
// problems 列出全部问题（不含凭证）。
type configError struct {
	code     string
	problems []string
}

func (e *configError) Error() string {
	return strings.Join(e.problems, "; ")
}

// validateRuntime 在每请求 native manifest 成功、Discover 完成后执行。
// 身份规则：prefix 是渠道的唯一配置身份（CPA 仅 openai-compatibility 有 name 字段；
// 且无前缀渠道的模型在本管线会被全部过滤，空前缀 = 不可用配置）。
//   - 发现的 key channel：prefix 非空且唯一；
//   - 每个发现渠道必须有 exact channels.<prefix> 配置与非空 channel 链；
//   - 链含 ollama_cloud 的渠道必须显式配置 ollama_native_base_url；
//   - custom channel key 不得与发现渠道 prefix 或 native manifest 的
//     provider prefix（slug 首段）冲突。
func validateRuntime(cfg *Config, channels []Channel, base *Manifest) error {
	var conflicts, missing []string

	seenPrefixes := map[string]string{}
	for _, ch := range channels {
		if ch.Prefix == "" {
			conflicts = append(conflicts, fmt.Sprintf("channel (type=%s name=%q) has empty prefix", ch.Type, ch.Name))
			continue
		}
		if prev, dup := seenPrefixes[ch.Prefix]; dup {
			conflicts = append(conflicts, fmt.Sprintf("duplicate channel prefix %q (types %q and %q)", ch.Prefix, prev, ch.Type))
			continue
		}
		seenPrefixes[ch.Prefix] = ch.Type

		chCfg, ok := cfg.Channels[ch.Prefix]
		if !ok {
			missing = append(missing, fmt.Sprintf("channel prefix %q has no channels.<prefix> configuration", ch.Prefix))
			continue
		}
		if len(chCfg.SourcePriority) == 0 {
			missing = append(missing, fmt.Sprintf("channel prefix %q has empty source_priority", ch.Prefix))
		}
		if channelUsesOllama(chCfg) && chCfg.OllamaNativeBase == "" {
			missing = append(missing, fmt.Sprintf("channel prefix %q uses ollama_cloud but ollama_native_base_url is not configured", ch.Prefix))
		}
	}

	if len(conflicts) == 0 && base != nil {
		nativePrefixes := map[string]struct{}{}
		for _, m := range base.Models {
			slug := asString(m["slug"])
			if i := strings.Index(slug, "/"); i > 0 {
				nativePrefixes[slug[:i]] = struct{}{}
			}
		}
		for key := range cfg.CustomChannels {
			if _, hit := seenPrefixes[key]; hit {
				conflicts = append(conflicts, fmt.Sprintf("custom channel %q conflicts with CPA channel prefix", key))
			}
			if _, hit := nativePrefixes[key]; hit {
				conflicts = append(conflicts, fmt.Sprintf("custom channel %q conflicts with native manifest provider prefix", key))
			}
		}
	}

	if len(conflicts) > 0 {
		return &configError{code: "catalog_configuration_conflict", problems: conflicts}
	}
	if len(missing) > 0 {
		return &configError{code: "catalog_configuration_missing", problems: missing}
	}
	return nil
}
