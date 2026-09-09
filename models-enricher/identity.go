package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// 身份证据只决定如何筛选当前公开成员，管理列表从不直接添加公开记录。
type catalogIdentities struct {
	qualified, unavailable, nativeOnly map[string]bool
	incomplete                         bool
}

func (c *CPAClient) catalogIdentities(ctx context.Context, channels []Channel, discoveryErr error) *catalogIdentities {
	ids := &catalogIdentities{
		qualified: map[string]bool{}, unavailable: map[string]bool{}, nativeOnly: map[string]bool{},
		incomplete: discoveryErr != nil,
	}
	for _, ch := range channels {
		for _, id := range ch.Models {
			if ch.Prefix != "" {
				ids.qualified[ch.Prefix+"/"+id] = true
			}
		}
	}
	c.oauthIdentities(ctx, ids)
	return ids
}

type identityAuth struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Disabled bool   `json:"disabled"`
}

type identityModel struct {
	ID string `json:"id"`
}

type identityAlias struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

func (c *CPAClient) oauthIdentities(ctx context.Context, ids *catalogIdentities) {
	var files []identityAuth
	if err := c.identityRead(ctx, "/v0/management/auth-files", "files", &files); err != nil {
		ids.incomplete = true
		c.log.Warn("catalog identity read failed", "step", "auth list")
		return
	}
	if len(files) == 0 {
		return
	}
	var aliases map[string][]identityAlias
	aliasErr := c.identityRead(ctx, "/v0/management/oauth-model-alias", "oauth-model-alias", &aliases)
	for _, file := range files {
		if file.Disabled {
			continue
		}
		var registered, definitions []identityModel
		if err := c.identityRead(ctx, "/v0/management/auth-files/models?name="+url.QueryEscape(file.Name), "models", &registered); err != nil {
			// 缺少账号注册集，无法排除与其他渠道原名的重叠。
			ids.incomplete = true
			c.log.Warn("catalog identity read failed", "step", "auth models")
			continue
		}
		var definitionErr error
		if aliasErr == nil {
			definitionErr = c.identityRead(ctx, "/v0/management/model-definitions/"+url.PathEscape(file.Provider), "models", &definitions)
		}
		originals := map[string]bool{}
		for _, model := range definitions {
			originals[model.ID] = true
		}
		aliasIDs := map[string]bool{}
		for _, alias := range aliases[file.Provider] {
			if originals[alias.Name] {
				aliasIDs[alias.Alias] = true
			}
		}
		if aliasErr != nil || definitionErr != nil {
			for _, model := range registered {
				ids.unavailable[model.ID] = true
			}
			c.log.Warn("catalog identity read failed", "step", "OAuth definitions or aliases")
			continue
		}
		for _, model := range registered {
			// 第一步去原名/裸别名，包含原始ID中的厂商命名空间。
			if originals[model.ID] || aliasIDs[model.ID] {
				continue
			}
			// 第二步：注册ID去路由前缀后必须命中本provider的原名或别名。
			// force-model-prefix下CPA只注册带前缀ID，不能再要求裸名同时注册。
			// 只记录能否对应，不推导或持久化OAuth前缀，也不修改公开ID。
			for i, ch := range model.ID {
				if ch != '/' || i == 0 {
					continue
				}
				name := model.ID[i+1:]
				if originals[name] || aliasIDs[name] {
					if !ids.qualified[model.ID] {
						ids.nativeOnly[model.ID] = true // OAuth仍只透传CPA字段。
					}
					ids.qualified[model.ID] = true
					break
				}
			}
		}
	}
}

func (ids *catalogIdentities) filter(base *Manifest, cfg *Config) (*Manifest, int) {
	out := &Manifest{Models: make([]map[string]any, 0, len(base.Models)), preserveNative: map[string]bool{}, admitted: map[string]bool{}}
	for id := range ids.qualified {
		out.admitted[id] = true
	}
	static := map[string]bool{}
	for _, model := range cfg.StaticModels {
		static[asString(model["slug"])] = true
	}
	fallbackCount := 0
	for _, model := range base.Models {
		id := asString(model["slug"])
		switch {
		case ids.qualified[id]: // 同名也有已声明路由时，保留该路由。
			out.preserveNative[id] = ids.nativeOnly[id] && !static[id]
		case ids.incomplete || ids.unavailable[id]:
			fallbackCount++
			// 读取故障不是无匹配：只保留已有CPA记录，不新增未知候选。
			out.preserveNative[id] = !static[id]
			out.admitted[id] = true
		default:
			continue // 管理数据成功读取但无精确对应，移除。
		}
		out.Models = append(out.Models, model)
	}
	return out, fallbackCount
}

// 使用既有有界读取缓存；验证包装字段，不能将坏响应缓存成空身份集。
func (c *CPAClient) identityRead(ctx context.Context, path, field string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	decode := func(body []byte) error {
		var wrapped map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrapped); err != nil {
			return err
		}
		raw, ok := wrapped[field]
		if !ok || (bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && field != "oauth-model-alias") {
			return errors.New("missing identity response field")
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
		// 只接受所需身份字段，不让缺字段的200替代此前有效缓存。
		switch value := out.(type) {
		case *[]identityAuth:
			for _, file := range *value {
				if file.Name == "" || file.Provider == "" {
					return errors.New("invalid auth identity")
				}
			}
		case *[]identityModel:
			for _, model := range *value {
				if model.ID == "" {
					return errors.New("invalid model identity")
				}
			}
		case *map[string][]identityAlias:
			for _, entries := range *value {
				for _, alias := range entries {
					if alias.Name == "" || alias.Alias == "" {
						return errors.New("invalid alias identity")
					}
				}
			}
		}
		return nil
	}
	body, err := c.pool.readJSON(req, 8<<20, true, decode)
	if err != nil {
		return err
	}
	return decode(body)
}
