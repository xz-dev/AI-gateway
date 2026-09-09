package main

import "testing"

// 全局链兜底：渠道/模型未配置时回落全局；显式空链关闭兜底。
func TestGlobalSourceChainFallback(t *testing.T) {
	global := []string{"models.dev/openai"}
	ch := ChannelConfig{globalChain: global}
	if got := sourceChain(ch, "m"); len(got) != 1 || got[0] != "models.dev/openai" {
		t.Fatalf("global fallback: got %v", got)
	}
	// 渠道显式空链：关闭兜底。
	empty := ChannelConfig{globalChain: global, SourcePriority: []string{}}
	if got := sourceChain(empty, "m"); len(got) != 0 {
		t.Fatalf("explicit empty chain should stay empty, got %v", got)
	}
	// 渠道显式链优先于全局。
	local := ChannelConfig{globalChain: global, SourcePriority: []string{"models.dev/zai"}}
	if got := sourceChain(local, "m"); len(got) != 1 || got[0] != "models.dev/zai" {
		t.Fatalf("channel chain should win, got %v", got)
	}
	// 模型显式链优先于渠道与全局。
	mc := ChannelConfig{
		globalChain:     global,
		SourcePriority:  []string{"models.dev/zai"},
		Models:          map[string]ModelConfig{"m": {SourcePriority: []string{"models.dev/tencent"}}},
	}
	if got := sourceChain(mc, "m"); len(got) != 1 || got[0] != "models.dev/tencent" {
		t.Fatalf("model chain should win, got %v", got)
	}
}

// 裸名 + 全局链生成带 provider 命名空间的查询，ID 为裸名。
func TestBareModelQueriesUseTokenProvider(t *testing.T) {
	ch := ChannelConfig{globalChain: []string{"models.dev/zai", "models.dev/openai"}}
	queries := sourceQueries(ch, "glm-4.6")
	tokens := map[string]string{}
	for _, q := range queries {
		tokens[q.token] = q.id
	}
	if tokens["models.dev/zai"] != "glm-4.6" {
		t.Fatalf("expected bare ID under token provider, got %v", tokens)
	}
}

// 开关关闭时保持现状：裸名在入口过滤被移除。
func TestBareModelsTakeoverOff(t *testing.T) {
	ids := &catalogIdentities{qualified: map[string]bool{}, unavailable: map[string]bool{}, nativeOnly: map[string]bool{}}
	cfg := &Config{}
	base := &Manifest{Models: []map[string]any{{"slug": "glm-4.6"}, {"slug": "zcode/glm-4.6"}}}
	ids.qualified["zcode/glm-4.6"] = true
	out, _ := ids.filter(base, cfg)
	for _, m := range out.Models {
		if asString(m["slug"]) == "glm-4.6" {
			t.Fatal("bare model should be filtered out when takeover is off")
		}
	}
}

// 开关开启时：裸名保留、admitted、且不标记 preserveNative（进入动态补全）。
func TestBareModelsTakeoverOn(t *testing.T) {
	ids := &catalogIdentities{qualified: map[string]bool{}, unavailable: map[string]bool{}, nativeOnly: map[string]bool{}}
	cfg := &Config{BareModelsTakeover: true}
	base := &Manifest{Models: []map[string]any{{"slug": "glm-4.6"}, {"slug": "zcode/glm-4.6"}}}
	ids.qualified["zcode/glm-4.6"] = true
	out, _ := ids.filter(base, cfg)
	found := false
	for _, m := range out.Models {
		if asString(m["slug"]) == "glm-4.6" {
			found = true
		}
	}
	if !found {
		t.Fatal("bare model should be kept when takeover is on")
	}
	if !out.admitted["glm-4.6"] {
		t.Fatal("bare model should be admitted for enrichment")
	}
	if out.preserveNative["glm-4.6"] {
		t.Fatal("bare model should not be preserveNative; it must go through dynamic enrichment")
	}
}
