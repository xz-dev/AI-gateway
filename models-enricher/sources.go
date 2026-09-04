package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

type SourceCache struct {
	ttl  time.Duration
	http *http.Client
	mu   sync.Mutex
	at   time.Time
	dev  map[string]sourceHit
	mpK  map[string]sourceHit // modelparams authType=api_key
	mpS  map[string]sourceHit // modelparams authType=subscription
}

type sourceHit struct {
	DisplayName      string
	ContextWindow    int
	MaxTokens        int
	InputModalities  []string
	ReasoningEfforts []string
	DefaultReasoning string
	SupportsReasoning bool
}

func newSourceCache(ttl time.Duration) *SourceCache {
	return &SourceCache{ttl: ttl, http: &http.Client{Timeout: 30 * time.Second}}
}

// LookupChain 按解析出的源链顺序返回各源的首个命中（fillGaps 由调用方按序应用）。
// ids 为同一模型的候选 id（如剥前缀名与原始 id），每个源内按序尝试。
func (s *SourceCache) LookupChain(chain []string, ids ...string) []sourceHit {
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []sourceHit
	for _, src := range chain {
		var table map[string]sourceHit
		switch src {
		case "models_dev":
			table = s.dev
		case "modelparams_api_key":
			table = s.mpK
		case "modelparams_subscription":
			table = s.mpS
		default:
			continue
		}
		for _, id := range ids {
			found := false
			for _, key := range idVariants(id) {
				if h, ok := table[key]; ok {
					out = append(out, h)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	return out
}

func (s *SourceCache) refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dev != nil && time.Since(s.at) < s.ttl {
		return
	}
	dev := map[string]sourceHit{}
	if raw, err := s.get("https://models.dev/api.json"); err == nil {
		indexModelsDev(raw, dev)
	} else {
		slog.Warn("source fetch failed", "url", "models.dev/api.json", "err", err)
	}
	if raw, err := s.get("https://models.dev/models.json"); err == nil {
		indexModelsDevFlat(raw, dev)
	}
	mpK := map[string]sourceHit{}
	mpS := map[string]sourceHit{}
	if raw, err := s.get("https://modelparams.dev/api/v1/models.json"); err == nil {
		indexModelparams(raw, mpK, mpS)
	} else {
		slog.Warn("source fetch failed", "url", "modelparams.dev", "err", err)
	}
	s.dev, s.mpK, s.mpS, s.at = dev, mpK, mpS, time.Now()
	slog.Info("sources refreshed", "models_dev", len(dev), "mp_api_key", len(mpK), "mp_subscription", len(mpS))
}

func (s *SourceCache) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// officialProviderRank：models.dev 同模型常被多家聚合站收录且 limit 字段互有垃圾值，
// 厂商官方源优先；未列出的聚合站按名字字母序兜底。保证 storeHit 首写胜出可复现。
var officialProviderRank = map[string]int{
	"openai": 0, "anthropic": 1, "google": 2, "xai": 3,
	"deepseek": 4, "zhipuai": 5, "zai": 6,
	"moonshotai": 7, "moonshotai-cn": 8,
	"qwen": 9, "alibaba": 10, "minimax": 11, "minimaxai": 12,
	"mistral": 13, "meta": 14,
}

func providerRank(name string) int {
	if r, ok := officialProviderRank[name]; ok {
		return r
	}
	return 1000
}

func indexModelsDev(raw []byte, out map[string]sourceHit) {
	var providers map[string]struct {
		Models map[string]map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &providers) != nil {
		return
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := providerRank(names[i]), providerRank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		for id, m := range providers[name].Models {
			hit := hitFromModelsDev(m)
			storeHit(out, id, hit)
			if name, _ := m["id"].(string); name != "" {
				storeHit(out, name, hit)
			}
		}
	}
}

func indexModelsDevFlat(raw []byte, out map[string]sourceHit) {
	var models map[string]map[string]any
	if json.Unmarshal(raw, &models) != nil {
		return
	}
	for id, m := range models {
		storeHit(out, id, hitFromModelsDev(m))
	}
}

func hitFromModelsDev(m map[string]any) sourceHit {
	h := sourceHit{DisplayName: firstString(m, "name")}
	if limit, _ := m["limit"].(map[string]any); limit != nil {
		h.ContextWindow = toInt(limit["context"])
		h.MaxTokens = toInt(limit["output"])
	}
	if mods, _ := m["modalities"].(map[string]any); mods != nil {
		h.InputModalities = toStringSlice(mods["input"])
	}
	h.SupportsReasoning, _ = m["reasoning"].(bool)
	if opts, ok := m["reasoning_options"].([]any); ok {
		for _, opt := range opts {
			om, _ := opt.(map[string]any)
			if firstString(om, "type") != "effort" {
				continue
			}
			h.ReasoningEfforts = toStringSlice(om["values"])
		}
	}
	return h
}

func indexModelparams(raw []byte, apiKeyOut, subOut map[string]sourceHit) {
	var envelope struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return
	}
	for _, m := range envelope.Models {
		id := firstString(m, "model")
		if id == "" {
			continue
		}
		h := sourceHit{}
		params, _ := m["params"].([]any)
		for _, p := range params {
			pm, _ := p.(map[string]any)
			path := firstString(pm, "path")
			switch path {
			case "max_completion_tokens", "max_tokens", "max_output_tokens":
				if rng, _ := pm["range"].(map[string]any); rng != nil {
					if n := toInt(rng["max"]); n > 0 {
						h.MaxTokens = n
					}
				}
				if h.MaxTokens == 0 {
					h.MaxTokens = toInt(pm["default"])
				}
			case "reasoning_effort":
				h.SupportsReasoning = true
				h.ReasoningEfforts = toStringSlice(pm["values"])
				h.DefaultReasoning = firstString(pm, "default")
			}
		}
		// authType 拆分：api_key / subscription；缺失时两边都索引（宽松）。
		switch strings.ToLower(firstString(m, "authType")) {
		case "api_key":
			storeHit(apiKeyOut, id, h)
			if prov := firstString(m, "provider"); prov != "" {
				storeHit(apiKeyOut, prov+"/"+id, h)
			}
		case "subscription":
			storeHit(subOut, id, h)
			if prov := firstString(m, "provider"); prov != "" {
				storeHit(subOut, prov+"/"+id, h)
			}
		default:
			storeHit(apiKeyOut, id, h)
			storeHit(subOut, id, h)
			if prov := firstString(m, "provider"); prov != "" {
				storeHit(apiKeyOut, prov+"/"+id, h)
				storeHit(subOut, prov+"/"+id, h)
			}
		}
	}
}

func storeHit(out map[string]sourceHit, id string, h sourceHit) {
	for _, key := range idVariants(id) {
		if _, exists := out[key]; !exists {
			out[key] = h
		}
	}
}

func idVariants(id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(id)
	add(strings.ToLower(id))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		add(id[i+1:])
		add(strings.ToLower(id[i+1:]))
	}
	stripped := stripDateSuffix(id)
	add(stripped)
	add(strings.ToLower(stripped))
	return out
}

func stripDateSuffix(id string) string {
	i := strings.LastIndex(id, "-")
	if i < 0 {
		return id
	}
	tail := id[i+1:]
	if len(tail) == 8 {
		for _, r := range tail {
			if !unicode.IsDigit(r) {
				return id
			}
		}
		return id[:i]
	}
	return id
}
