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
	// 渠道链在前，全局链去重追加兜底。
	local := ChannelConfig{globalChain: []string{"models.dev/zai", "models.dev/openai"}, SourcePriority: []string{"models.dev/zai"}}
	if got := sourceChain(local, "m"); len(got) != 2 || got[0] != "models.dev/zai" || got[1] != "models.dev/openai" {
		t.Fatalf("channel chain first, then global fallback, got %v", got)
	}
	// global_fallback: false 只用渠道链；未写渠道链时无来源。
	off := false
	noGlobal := ChannelConfig{globalChain: global, SourcePriority: []string{"models.dev/zai"}, GlobalFallback: &off}
	if got := sourceChain(noGlobal, "m"); len(got) != 1 || got[0] != "models.dev/zai" {
		t.Fatalf("global_fallback false must keep only channel chain, got %v", got)
	}
	if got := sourceChain(ChannelConfig{globalChain: global, GlobalFallback: &off}, "m"); len(got) != 0 {
		t.Fatalf("global_fallback false without channel chain must be empty, got %v", got)
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

// static 声明的裸名也必须保留在 base 中：statics 物化克隆的是 bySlug 的动态数据，
// 若 filter 把 static 裸名移除，克隆为空导致 ctx/display 等动态字段全部丢失。
func TestBareStaticSlugStaysAdmitted(t *testing.T) {
	ids := &catalogIdentities{qualified: map[string]bool{}, unavailable: map[string]bool{}, nativeOnly: map[string]bool{}}
	cfg := &Config{BareModelsTakeover: true}
	base := &Manifest{Models: []map[string]any{{"slug": "glm-5.2"}}}
	out, _ := ids.filter(base, cfg)
	if !out.admitted["glm-5.2"] {
		t.Fatal("static-declared bare slug must stay admitted for dynamic enrichment")
	}
}
