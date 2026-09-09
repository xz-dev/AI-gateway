package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestNIMConfiguredFullIDMetadata(t *testing.T) {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// 读取实际NIM配置，只隔离无关渠道和静态模型，不在测试中补写查询规则。
	cfg.Channels = map[string]ChannelConfig{"nim": cfg.Channels["nim"]}
	cfg.StaticModels = nil
	cfg.CustomChannels = nil

	// 2026-09-08 models.dev API保存的NVIDIA字段；source-only是额外的成员准入哨兵。
	// CPA输入只含原始成员和旧字段，不使用已富化目录。
	// 完整来源SHA256：3335e4ab406bcb3fb8519d81ae53c8934c750a3359f75139795049bd7ced0612。
	const api = `{"nvidia":{"models":{
		"deepseek-ai/deepseek-v4-flash-0731":{"id":"deepseek-ai/deepseek-v4-flash-0731","name":"DeepSeek V4 Flash 0731","limit":{"context":1000000,"output":384000},"modalities":{"input":["text"],"output":["text"]},"reasoning_options":[{"type":"effort","values":["none","high","max"]}]},
		"minimaxai/minimax-m3":{"id":"minimaxai/minimax-m3","name":"MiniMax-M3","limit":{"context":1000000,"output":16384},"modalities":{"input":["text","image","video"],"output":["text"]},"reasoning_options":[{"type":"toggle"}]},
		"source-only":{"id":"source-only","limit":{"output":999}}
	}}}`
	var apiCalls, flatCalls, otherCalls atomic.Int64
	sources := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			apiCalls.Add(1)
			fmt.Fprint(w, api)
		case "/flat.json":
			flatCalls.Add(1)
			fmt.Fprint(w, `{}`)
		default:
			otherCalls.Add(1)
			http.Error(w, "unexpected source", http.StatusBadRequest)
		}
	}))
	defer sources.Close()
	oldA, oldF, oldM := modelsDevAPIURL, modelsDevFlatURL, modelparamsURL
	modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = sources.URL+"/api.json", sources.URL+"/flat.json", sources.URL+"/params.json"
	t.Cleanup(func() { modelsDevAPIURL, modelsDevFlatURL, modelparamsURL = oldA, oldF, oldM })

	fake := &fakeCPA{
		native: []byte(`{"models":[
			{"slug":"nim/deepseek-ai/deepseek-v4-flash-0731","id":"nim/deepseek-ai/deepseek-v4-flash-0731","context_window":272000,"input_modalities":["text","image"],"default_reasoning_level":"medium"},
			{"slug":"nim/minimaxai/minimax-m3","id":"nim/minimaxai/minimax-m3","context_window":272000,"input_modalities":["text","image"],"default_reasoning_level":"medium"}
		]}`),
		channelsBody: []byte(`{"openai-compatibility":[{"name":"nim","prefix":"nim","base-url":"https://example.invalid/v1","api-key-entries":[{"auth-index":"test"}],"models":[{"name":"deepseek-ai/deepseek-v4-flash-0731"},{"name":"minimaxai/minimax-m3"}]}]}`),
	}
	cpa := httptest.NewServer(fake.handler())
	defer cpa.Close()
	got := identityCatalog(t, newTestHandler(t, cfg, cpa))
	if len(got) != 2 {
		t.Fatalf("NIM目录成员改变: %v", got)
	}
	for _, tc := range []struct {
		id, name       string
		output         int
		input          []any
		defaultPresent bool
	}{
		{"deepseek-ai/deepseek-v4-flash-0731", "DeepSeek V4 Flash 0731", 384000, []any{"text"}, false},
		{"minimaxai/minimax-m3", "MiniMax-M3", 16384, []any{"text", "image", "video"}, true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			slug := "nim/" + tc.id
			row := got[slug]
			if row == nil || row["id"] != slug {
				t.Fatalf("公开模型身份改变: %v", row)
			}
			if toInt(row["max_output_tokens"]) != tc.output {
				t.Errorf("完整NVIDIA模型ID未补全输出上限: got %v, want %d", row["max_output_tokens"], tc.output)
			}
			if row["display_name"] != tc.name || toInt(row["context_window"]) != 1000000 || !reflect.DeepEqual(row["input_modalities"], tc.input) || !reflect.DeepEqual(row["output_modalities"], []any{"text"}) {
				t.Errorf("NVIDIA声明字段未进入目录: %v", row)
			}
			for _, field := range []string{"max_input_tokens", "max_tokens", "max_completion_tokens"} {
				if value, exists := row[field]; exists {
					t.Errorf("不得推导未声明字段 %s=%v", field, value)
				}
			}
			value, present := row["default_reasoning_level"]
			if present != tc.defaultPresent || (present && value != "medium") {
				t.Errorf("只省略明确冲突的默认值，不猜测替代值: %v (present=%v)", value, present)
			}
		})
	}
	if levels := got["nim/deepseek-ai/deepseek-v4-flash-0731"]["supported_reasoning_levels"]; !reflect.DeepEqual(levels, []any{map[string]any{"effort": "none"}, map[string]any{"effort": "high"}, map[string]any{"effort": "max"}}) {
		t.Errorf("来源推理列表未保留: %v", levels)
	}
	if apiCalls.Load() != 1 || flatCalls.Load() != 1 || otherCalls.Load() != 0 {
		t.Errorf("查询映射扩大来源HTTP预算: %d/%d/%d", apiCalls.Load(), flatCalls.Load(), otherCalls.Load())
	}
}
