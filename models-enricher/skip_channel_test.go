package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestUnconfiguredChannelPreservesNativeWithoutInventory(t *testing.T) {
	cfg := &Config{OverallDeadline: time.Second}
	// nil客户端证明未配置渠道不发起请求，而不是依赖上游失败来降级。
	got := fetchChannelModels(context.Background(), nil, cfg, []Channel{{Type: "claude-api-key", Prefix: "unconfigured"}}, "test", testLog())
	if len(got) != 1 || !got[0].FetchSkipped || got[0].Failed {
		t.Fatal("unconfigured channel must retain native data without a failed step")
	}
	base := &Manifest{Models: []map[string]any{{"slug": "unconfigured/existing", "null": nil, "enabled": false, "n": json.Number("9007199254740993")}, {"slug": "oauth/native"}}}
	out := mergeManifest(base, got, cfg, emptySourceTables(), nil)
	if !reflect.DeepEqual(out.Models, base.Models) {
		t.Fatalf("unconfigured channel or native sibling changed: %#v", out.Models)
	}
}
