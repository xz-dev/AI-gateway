package main

import (
	"fmt"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type providerPrefix struct {
	name    string
	targets []string
}

type providerPrefixes []providerPrefix

// 同键原位替换，新键追加；不修改供其他渠道继承的全局切片。
func mergeProviderPrefixes(global, local providerPrefixes) providerPrefixes {
	out := append(providerPrefixes(nil), global...)
	for _, entry := range local {
		found := false
		for i := range out {
			if out[i].name == entry.name {
				out[i] = entry
				found = true
				break
			}
		}
		if !found {
			out = append(out, entry)
		}
	}
	return out
}

func parseProviderPrefixes(node yaml.Node, where string) (providerPrefixes, error) {
	if node.Kind == 0 {
		return nil, nil
	}
	if node.Kind == yaml.AliasNode {
		node = *node.Alias
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s (line %d): must be a dictionary", where, node.Line)
	}
	component := func(n *yaml.Node) (string, error) {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value == "" || strings.Contains(n.Value, "/") || strings.ContainsFunc(n.Value, unicode.IsSpace) {
			return "", fmt.Errorf("%s (line %d): provider must be a non-empty string without slash or whitespace", where, n.Line)
		}
		return strings.ToLower(n.Value), nil
	}
	var out providerPrefixes
	for i := 0; i < len(node.Content); i += 2 {
		name, err := component(node.Content[i])
		if err != nil {
			return nil, err
		}
		value := node.Content[i+1]
		if value.Kind == yaml.AliasNode {
			value = value.Alias
		}
		values := []*yaml.Node{value}
		if value.Kind == yaml.SequenceNode {
			values = value.Content
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("%s.%s (line %d): targets must not be empty", where, name, value.Line)
		}
		entry := providerPrefix{name: name}
		seen := map[string]bool{}
		for _, n := range values {
			target, err := component(n)
			if err != nil {
				return nil, err
			}
			if !seen[target] {
				entry.targets = append(entry.targets, target)
				seen[target] = true
			}
		}
		out = mergeProviderPrefixes(out, providerPrefixes{entry})
	}
	return out, nil
}

// 查询保留来源的 family/provider/authType；只有带前缀名称替换 provider。
type sourceQuery struct {
	token, id string
	explicit  bool
}

func sourceQueries(ch ChannelConfig, name string) []sourceQuery {
	provider, id, qualified := strings.Cut(strings.ToLower(name), "/")
	if !qualified {
		id, provider = provider, ""
	}
	valid := id != "" && (!qualified || provider != "")
	targets := []string{provider}
	for _, entry := range ch.providerPrefixes {
		if qualified && entry.name == provider {
			targets = entry.targets
			break // 不递归映射目标。
		}
	}
	var out []sourceQuery
	seen := map[sourceQuery]bool{}
	add := func(q sourceQuery) {
		if !seen[q] {
			out = append(out, q)
			seen[q] = true
		}
	}
	lookupIDs := ch.modelLookupIDs(name)
	for _, token := range sourceChain(ch, name) {
		if token == "ollama_cloud" {
			add(sourceQuery{token: token, id: name})
			continue
		}
		parts := strings.Split(token, "/")
		for _, target := range targets {
			final := token
			if qualified && provider != "" {
				parts[1] = target
				final = strings.Join(parts, "/")
			}
			if explicit, exists := lookupIDs[final]; exists {
				add(sourceQuery{token: final, id: explicit, explicit: true})
				continue
			}
			if !valid {
				continue
			}
			// 裸名沿用当前链 provider；带前缀名称已在上方完成替换。
			parts[1] = strings.ToLower(parts[1])
			add(sourceQuery{token: strings.Join(parts, "/"), id: id})
		}
	}
	return out
}
