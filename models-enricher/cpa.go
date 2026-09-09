package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// 富数据目录固定请求版本，避免调用方版本触发CPA的旧客户端兼容裁剪。
const cpaCatalogClientVersion = "1"

type CPAClient struct {
	base      string
	mgmtKey   string
	clientKey string
	pool      *httpPool
	log       *slog.Logger
}

type Channel struct {
	Type      string
	Name      string
	Prefix    string
	BaseURL   string
	AuthIndex string
	// 管理配置声明的有效原名：alias 非空时使用 alias，否则使用 name。
	Models []string
}

type apiCallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       json.RawMessage     `json:"body"`
}

func newCPAClient(base, mgmtKey, clientKey string, pool *httpPool, log *slog.Logger) *CPAClient {
	return &CPAClient{
		base:      strings.TrimRight(base, "/"),
		mgmtKey:   mgmtKey,
		clientKey: clientKey,
		pool:      pool,
		log:       log,
	}
}

func (c *CPAClient) Discover(ctx context.Context) ([]Channel, error) {
	var out []Channel
	var failures []error
	for _, src := range channelSources {
		list, err := c.listKind(ctx, src)
		out = append(out, list...)
		if err != nil {
			c.log.Warn("channel list failed", "kind", src.typ, "err", err.Error())
			failures = append(failures, err)
			continue
		}
	}
	return out, errors.Join(failures...)
}

func (c *CPAClient) NativeManifest(ctx context.Context) (*Manifest, error) {
	u := c.base + "/v1/models?client_version=" + cpaCatalogClientVersion
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.clientKey)
	body, err := c.pool.readJSON(req, 32<<20, true, func(body []byte) error {
		var manifest Manifest
		return decodeJSON(body, &manifest)
	})
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := decodeJSON(body, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// APICall 经 CPA /v0/management/api-call 转发渠道请求；凭证不出 CPA（$TOKEN$ 由 CPA
// 依 auth_index 替换）。data 为非空时作为上游请求 body 原样携带（CPA api-call 的
// data 字段），调用方需自设 header Content-Type。
func (c *CPAClient) APICall(ctx context.Context, ch Channel, method, absURL string, header map[string]string, data []byte) ([]byte, int, error) {
	payload := map[string]any{
		"method": method,
		"url":    absURL,
		"header": header,
	}
	if len(data) > 0 {
		payload["data"] = string(data)
	}
	if ch.AuthIndex != "" {
		payload["auth_index"] = ch.AuthIndex
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v0/management/api-call", bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	req.Header.Set("Content-Type", "application/json")
	// 外层POST只是CPA转发封装；只缓存内层GET或Ollama的只读/api/show。
	endpoint, _ := url.Parse(absURL)
	cacheable := method == http.MethodGet || (method == http.MethodPost && endpoint != nil && endpoint.Path == "/api/show")
	body, err := c.pool.readJSON(req, 32<<20, cacheable, func(body []byte) error {
		_, _, err := parseAPIRead(body)
		return err
	})
	if err != nil {
		var status readStatusError
		if errors.As(err, &status) {
			return nil, status.status, err
		}
		return nil, 0, err
	}
	return parseAPIRead(body)
}

func parseAPIRead(body []byte) ([]byte, int, error) {
	var parsed apiCallResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, err
	}
	if parsed.StatusCode < 200 || parsed.StatusCode >= 300 {
		return nil, parsed.StatusCode, readStatusError{parsed.StatusCode}
	}
	inner := []byte(parsed.Body)
	var text string
	if json.Unmarshal(parsed.Body, &text) == nil {
		inner = []byte(text)
	}
	if !json.Valid(inner) {
		return nil, parsed.StatusCode, fmt.Errorf("invalid api-call body JSON")
	}
	return inner, parsed.StatusCode, nil
}

type channelSource struct {
	typ     string
	path    string
	wrapper string
}

var channelSources = []channelSource{
	{typ: "openai-compatibility", path: "/v0/management/openai-compatibility", wrapper: "openai-compatibility"},
	{typ: "claude-api-key", path: "/v0/management/claude-api-key", wrapper: "claude-api-key"},
	{typ: "gemini-api-key", path: "/v0/management/gemini-api-key", wrapper: "gemini-api-key"},
	{typ: "codex-api-key", path: "/v0/management/codex-api-key", wrapper: "codex-api-key"},
	{typ: "xai-api-key", path: "/v0/management/xai-api-key", wrapper: "xai-api-key"},
	{typ: "vertex-api-key", path: "/v0/management/vertex-api-key", wrapper: "vertex-api-key"},
	{typ: "interactions-api-key", path: "/v0/management/interactions-api-key", wrapper: "interactions-api-key"},
}

var errIncompleteDiscovery = errors.New("channel identity discovery incomplete")

func (c *CPAClient) listKind(ctx context.Context, src channelSource) ([]Channel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+src.path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	body, err := c.pool.readJSON(req, 8<<20, true, func(body []byte) error {
		_, err := parseChannels(src.typ, src.wrapper, body)
		if errors.Is(err, errIncompleteDiscovery) {
			return nil // 响应可用但覆盖不全；返回时仍携带不完整信号。
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return parseChannels(src.typ, src.wrapper, body)
}

func parseChannels(typ, wrapper string, body []byte) ([]Channel, error) {
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	raw, ok := wrapped[wrapper]
	if !ok {
		return nil, fmt.Errorf("missing %s", wrapper)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(entries))
	var coverageErr error
	for _, e := range entries {
		if e == nil {
			return nil, fmt.Errorf("invalid %s entry", wrapper)
		}
		if asBool(e["disabled"]) {
			continue
		}
		// name 仅作展示/日志元数据；配置身份统一为 prefix（见 validateRuntime）。
		name := asString(e["name"])
		prefix := asString(e["prefix"])
		base := asString(e["base-url"])
		if base == "" {
			base = asString(e["base_url"])
		}
		auth := asString(e["auth-index"])
		if auth == "" {
			auth = asString(e["auth_index"])
		}
		if keys, ok := e["api-key-entries"].([]any); ok && len(keys) > 0 {
			if first, ok := keys[0].(map[string]any); ok {
				if v := asString(first["auth-index"]); v != "" {
					auth = v
				} else if v := asString(first["auth_index"]); v != "" {
					auth = v
				}
			}
		} else if typ != "openai-compatibility" && auth == "" {
			coverageErr = errIncompleteDiscovery
			continue
		}
		if typ == "openai-compatibility" && auth == "" {
			if keys, ok := e["api-key-entries"].([]any); !ok || len(keys) == 0 {
				coverageErr = errIncompleteDiscovery
				continue
			}
		}
		var models []string
		if value := e["models"]; value != nil {
			list, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("invalid %s models", wrapper)
			}
			for _, value := range list {
				model, ok := value.(map[string]any)
				if !ok || asString(model["name"]) == "" {
					return nil, fmt.Errorf("invalid %s model name", wrapper)
				}
				if alias := model["alias"]; alias != nil {
					if _, ok := alias.(string); !ok {
						return nil, fmt.Errorf("invalid %s model alias", wrapper)
					}
				}
				id := asString(model["alias"])
				if id == "" {
					id = asString(model["name"])
				}
				models = append(models, id)
			}
		}
		out = append(out, Channel{
			Type:      typ,
			Name:      name,
			Prefix:    strings.Trim(prefix, "/"),
			BaseURL:   base,
			AuthIndex: auth,
			Models:    models,
		})
	}
	return out, coverageErr
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
