package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

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
	for _, src := range channelSources {
		list, err := c.listKind(ctx, src)
		if err != nil {
			c.log.Warn("channel list failed", "kind", src.typ, "err", err.Error())
			continue
		}
		out = append(out, list...)
	}
	return out, nil
}

func (c *CPAClient) NativeManifest(ctx context.Context, clientVersion string) (*Manifest, error) {
	u := c.base + "/v1/models?client_version=" + url.QueryEscape(clientVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.clientKey)
	resp, err := c.pool.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("native manifest status %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
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
	resp, err := c.pool.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("api-call http %d", resp.StatusCode)
	}
	var parsed apiCallResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, err
	}
	if parsed.StatusCode < 200 || parsed.StatusCode >= 300 {
		return nil, parsed.StatusCode, fmt.Errorf("upstream status %d", parsed.StatusCode)
	}
	// body 可能是 string（原样文本）或直接 JSON 对象，两种都接受
	var s string
	if err := json.Unmarshal(parsed.Body, &s); err == nil {
		return []byte(s), parsed.StatusCode, nil
	}
	return parsed.Body, parsed.StatusCode, nil
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

func (c *CPAClient) listKind(ctx context.Context, src channelSource) ([]Channel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+src.path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	resp, err := c.pool.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
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
	for _, e := range entries {
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
			continue
		}
		if typ == "openai-compatibility" && auth == "" {
			if keys, ok := e["api-key-entries"].([]any); !ok || len(keys) == 0 {
				continue
			}
		}
		out = append(out, Channel{
			Type:      typ,
			Name:      name,
			Prefix:    strings.Trim(prefix, "/"),
			BaseURL:   base,
			AuthIndex: auth,
		})
	}
	return out, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
