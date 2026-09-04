package main

import (
	"encoding/json"
	"net/url"
	"strings"
)

type ParsedModel struct {
	ID               string
	DisplayName      string
	ContextWindow    int
	MaxContextWindow int
	MaxTokens        int
	InputModalities  []string
	ReasoningEfforts []string
	DefaultReasoning string
	SupportsReasoning bool
}

type adapter struct {
	auth    map[string]string
	path    func(clientVersion string) string
	parse   func([]byte) ([]ParsedModel, error)
}

func adapterFor(typ string) adapter {
	switch typ {
	case "claude-api-key":
		return adapter{
			auth: map[string]string{
				"x-api-key":         "$TOKEN$",
				"anthropic-version": "2023-06-01",
			},
			path:  func(string) string { return "/v1/models" },
			parse: parseClaude,
		}
	case "gemini-api-key", "interactions-api-key":
		return adapter{
			auth:  map[string]string{"x-goog-api-key": "$TOKEN$"},
			path:  func(string) string { return "/v1beta/models" },
			parse: parseGemini,
		}
	case "codex-api-key":
		return adapter{
			auth: map[string]string{"Authorization": "Bearer $TOKEN$"},
			path: func(v string) string {
				return "/v1/models?client_version=" + url.QueryEscape(v)
			},
			parse: parseOpenAI,
		}
	default:
		return adapter{
			auth: map[string]string{"Authorization": "Bearer $TOKEN$"},
			path: func(v string) string {
				if typ == "openai-compatibility" && v != "" {
					return "/v1/models?client_version=" + url.QueryEscape(v)
				}
				return "/v1/models"
			},
			parse: parseOpenAI,
		}
	}
}

func parseOpenAI(body []byte) ([]ParsedModel, error) {
	var envelope struct {
		Data    []map[string]any `json:"data"`
		Models  []map[string]any `json:"models"`
		Object  string           `json:"object"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	rows := envelope.Data
	if len(rows) == 0 {
		rows = envelope.Models
	}
	out := make([]ParsedModel, 0, len(rows))
	for _, row := range rows {
		id := firstString(row, "id", "slug", "name")
		if id == "" {
			continue
		}
		m := ParsedModel{ID: id, DisplayName: firstString(row, "display_name", "name")}
		if n := intFrom(row, "context_window", "context_length", "max_context_window"); n > 0 {
			m.ContextWindow = n
			m.MaxContextWindow = n
		}
		if n := nestedInt(row, "top_provider", "max_completion_tokens"); n > 0 {
			m.MaxTokens = n
		} else if n := intFrom(row, "max_tokens", "max_output_tokens"); n > 0 {
			m.MaxTokens = n
		}
		if mods := nestedStringSlice(row, "architecture", "input_modalities"); len(mods) > 0 {
			m.InputModalities = mods
		}
		if params, ok := row["supported_parameters"].([]any); ok {
			for _, p := range params {
				if s, _ := p.(string); s == "reasoning" || s == "include_reasoning" {
					m.SupportsReasoning = true
				}
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func parseClaude(body []byte) ([]ParsedModel, error) {
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	out := make([]ParsedModel, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		id := firstString(row, "id")
		if id == "" {
			continue
		}
		out = append(out, ParsedModel{
			ID:          id,
			DisplayName: firstString(row, "display_name", "name"),
		})
	}
	return out, nil
}

func parseGemini(body []byte) ([]ParsedModel, error) {
	var envelope struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	out := make([]ParsedModel, 0, len(envelope.Models))
	for _, row := range envelope.Models {
		name := firstString(row, "name", "id")
		name = strings.TrimPrefix(name, "models/")
		if name == "" {
			continue
		}
		m := ParsedModel{
			ID:          name,
			DisplayName: firstString(row, "displayName", "display_name"),
		}
		if n := intFrom(row, "inputTokenLimit"); n > 0 {
			m.ContextWindow = n
			m.MaxContextWindow = n
		}
		if n := intFrom(row, "outputTokenLimit"); n > 0 {
			m.MaxTokens = n
		}
		out = append(out, m)
	}
	return out, nil
}

func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 渠道 base-url 常已含版本前缀（如 https://openrouter.ai/api/v1），
	// 与默认 path（/v1/models...）重复时去重，避免 /api/v1/v1/models
	for _, seg := range []string{"/v1beta", "/v1"} {
		if strings.HasSuffix(base, seg) && strings.HasPrefix(path, seg+"/") {
			path = strings.TrimPrefix(path, seg)
			break
		}
	}
	return base + path
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func intFrom(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if n := toInt(m[k]); n > 0 {
			return n
		}
	}
	return 0
}

func nestedInt(m map[string]any, a, b string) int {
	inner, _ := m[a].(map[string]any)
	if inner == nil {
		return 0
	}
	return toInt(inner[b])
}

func nestedStringSlice(m map[string]any, a, b string) []string {
	inner, _ := m[a].(map[string]any)
	if inner == nil {
		return nil
	}
	return toStringSlice(inner[b])
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
