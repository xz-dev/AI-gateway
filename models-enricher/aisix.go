package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AISIXClient 负责从可选的 AISIX /v1/models 端点读取模型列表。
// 仅读取 models API，不使用 management API，不探测渠道映射。
type AISIXClient struct {
	endpoint string
	token    string
	timeout  time.Duration
	pool     *httpPool
}

func newAISIXClient(endpoint, token string, timeout time.Duration, pool *httpPool) *AISIXClient {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	return &AISIXClient{
		endpoint: endpoint,
		token:    strings.TrimSpace(token),
		timeout:  timeout,
		pool:     pool,
	}
}

// aisixModelsResponse 标准 OpenAI /v1/models 响应结构。
type aisixModelsResponse struct {
	Object string `json:"object"`
	Data   *[]struct {
		ID string `json:"id"`
	} `json:"data"`
}

// validateAISIXModelsResponse 校验 AISIX 响应体：
// 必须为合法 JSON，必须含 data 数组（允许为空），数组内每一项 id 必须非空且非全空白。
// 格式错误或校验失败会使读取层失效缓存，不以陈旧数据掩盖错误。
func validateAISIXModelsResponse(body []byte) error {
	var resp aisixModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("invalid json in aisix models response: %w", err)
	}
	if resp.Data == nil {
		return errors.New("missing data array in aisix models response")
	}
	for _, item := range *resp.Data {
		if strings.TrimSpace(item.ID) == "" {
			return errors.New("empty or blank model id in aisix models response")
		}
	}
	return nil
}

// FetchModelIDs 读取并解析 AISIX 模型列表。
// 使用已有 readCache/httpPool 机制：
//   - 502/503/504 或网络超时允许回退既有有效缓存（stale fallback）
//   - 401/403 认证失败或响应体校验失败直接返回错误并失效缓存，不复活陈旧数据
//   - 有效空列表 {"data":[]} 正常缓存并覆盖旧数据，与故障区分
// 保留所接受 ID 的原始字节（包括空白、大小写与厂商前缀），拒绝全空白 ID。
func (c *AISIXClient) FetchModelIDs(ctx context.Context) ([]string, error) {
	if c == nil || c.endpoint == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("User-Agent", browserUA)

	body, err := c.pool.readJSON(req, 16<<20, true, validateAISIXModelsResponse)
	if err != nil {
		return nil, err
	}
	var resp aisixModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, errors.New("missing data array in aisix models response")
	}
	out := make([]string, 0, len(*resp.Data))
	for _, item := range *resp.Data {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		out = append(out, item.ID) // 保持原始字节，禁止 TrimSpace 改写
	}
	return out, nil
}

// extractCPAIDs 提取 CPA 原始清单的全部模型 ID（在本地身份过滤前）。
// 必须完整包含所有 slug/id，防止已过滤掉的 CPA 模型被误判为 AISIX 增量。
func extractCPAIDs(base *Manifest) map[string]bool {
	if base == nil {
		return nil
	}
	ids := make(map[string]bool, len(base.Models))
	for _, m := range base.Models {
		id := asString(m["slug"])
		if id == "" {
			id = asString(m["id"])
		}
		if id != "" {
			ids[id] = true
		}
	}
	return ids
}

// computeSupplementalIDs 计算 AISIX 独有模型差集：unique(N) - C。
// 比较使用区分大小写的完整 ID，不剥除前缀、不推导别名、不对比 display_name。
// 严格保持首次出现的先后顺序与原始字节。
func computeSupplementalIDs(aisixIDs []string, cpaIDs map[string]bool) []string {
	var out []string
	seen := make(map[string]bool)
	for _, id := range aisixIDs {
		if strings.TrimSpace(id) == "" || cpaIDs[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
