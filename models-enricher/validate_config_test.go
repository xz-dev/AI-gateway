package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validateTestConfig = "cpa_base_url: http://cpa\nchannels:\n  demo: {fetch_models: false, source_priority: [models.dev/openai]}\n"

func runValidate(t *testing.T, arg, input string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	rc := runValidateConfig(arg, strings.NewReader(input), &stdout, &stderr)
	return rc, stdout.String(), stderr.String()
}

func TestValidateConfigStdinValid(t *testing.T) {
	rc, stdout, stderr := runValidate(t, "-", validateTestConfig)
	if rc != 0 || stderr != "" {
		t.Fatalf("valid stdin config rejected: rc=%d stderr=%q", rc, stderr)
	}
	var got validateConfigResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not JSON: %q", stdout)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(validateTestConfig)))
	if got.Version != validateConfigVersion || got.ConfigDigest != want {
		t.Fatalf("unexpected result: %#v want digest %s", got, want)
	}
}

func TestValidateConfigFileValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(validateTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runValidate(t, path, "")
	if rc != 0 {
		t.Fatalf("valid file config rejected: rc=%d", rc)
	}
	var got validateConfigResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output is not JSON: %q", stdout)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(validateTestConfig)))
	if got.ConfigDigest != want {
		t.Fatalf("digest mismatch: %q != %q", got.ConfigDigest, want)
	}
}

func TestValidateConfigDeterministic(t *testing.T) {
	_, first, _ := runValidate(t, "-", validateTestConfig)
	_, second, _ := runValidate(t, "-", validateTestConfig)
	if first != second {
		t.Fatalf("same bytes must produce same result: %q != %q", first, second)
	}
}

func TestValidateConfigMalformedYAML(t *testing.T) {
	rc, stdout, stderr := runValidate(t, "-", "cpa_base_url: [unclosed")
	if rc != 1 || stdout != "" {
		t.Fatalf("malformed YAML must fail without stdout: rc=%d stdout=%q", rc, stdout)
	}
	if !strings.Contains(stderr, "invalid config") {
		t.Fatalf("expected invalid config error, got %q", stderr)
	}
}

func TestValidateConfigMissingRequiredField(t *testing.T) {
	rc, _, stderr := runValidate(t, "-", "channels: {}\n")
	if rc != 1 || !strings.Contains(stderr, "cpa_base_url is required") {
		t.Fatalf("missing cpa_base_url must fail with field error: rc=%d stderr=%q", rc, stderr)
	}
}

func TestValidateConfigInvalidSourceToken(t *testing.T) {
	rc, _, stderr := runValidate(t, "-", "cpa_base_url: http://cpa\nsource_priority: [bogus.dev/x]\n")
	if rc != 1 || !strings.Contains(stderr, "invalid global source token") {
		t.Fatalf("invalid source token must fail: rc=%d stderr=%q", rc, stderr)
	}
}

func TestValidateConfigInvalidPrefixMap(t *testing.T) {
	rc, _, stderr := runValidate(t, "-", "cpa_base_url: http://cpa\nprovider_prefix_map: not-a-map\n")
	if rc != 1 || stderr == "" {
		t.Fatalf("invalid provider_prefix_map must fail: rc=%d stderr=%q", rc, stderr)
	}
}

func TestValidateConfigInvalidRegex(t *testing.T) {
	rc, _, stderr := runValidate(t, "-", "cpa_base_url: http://cpa\nchannels:\n  p: {exclude: [\"[\"]}\n")
	if rc != 1 || !strings.Contains(stderr, "exclude") {
		t.Fatalf("invalid exclude regex must fail: rc=%d stderr=%q", rc, stderr)
	}
}

func TestValidateConfigMissingFile(t *testing.T) {
	rc, stdout, stderr := runValidate(t, filepath.Join(t.TempDir(), "nope.yaml"), "")
	if rc != 1 || stdout != "" || !strings.Contains(stderr, "read config") {
		t.Fatalf("missing file must fail with read error: rc=%d stderr=%q", rc, stderr)
	}
}

func TestValidateConfigUsageError(t *testing.T) {
	rc, stdout, _ := runValidate(t, "", "")
	if rc != 2 || stdout != "" {
		t.Fatalf("empty path must be usage error rc=2: rc=%d stdout=%q", rc, stdout)
	}
}

func TestValidateConfigNoSecretLeakage(t *testing.T) {
	t.Setenv("CPA_MANAGEMENT_KEY", "mgmt-secret-value")
	t.Setenv("CPA_API_KEY", "client-secret-value")
	t.Setenv("AISIX_TOKEN", "aisix-secret-value")
	// 含敏感外观内容的候选：校验仅回字段路径级错误，不回显行内秘密。
	candidate := "cpa_base_url: http://cpa\nchannels:\n  p: {include: [\"*secret-bad-regex*(\"]}\n"
	rc, stdout, stderr := runValidate(t, "-", candidate)
	if rc != 1 {
		t.Fatalf("invalid config must fail: rc=%d", rc)
	}
	for _, leak := range []string{"mgmt-secret-value", "client-secret-value", "aisix-secret-value", "*secret-bad-regex*"} {
		if strings.Contains(stdout, leak) || strings.Contains(stderr, leak) {
			t.Fatalf("output leaks %q: stdout=%q stderr=%q", leak, stdout, stderr)
		}
	}
}

func TestValidateConfigNoEnvironmentMutation(t *testing.T) {
	// AISIX_TOKEN 环境变量不应影响候选校验结果（同字节输入 -> 同结果）。
	t.Setenv("AISIX_TOKEN", "ignored-secret")
	_, first, _ := runValidate(t, "-", validateTestConfig)
	os.Unsetenv("AISIX_TOKEN")
	_, second, _ := runValidate(t, "-", validateTestConfig)
	if first != second {
		t.Fatalf("environment must not change validation output: %q != %q", first, second)
	}
}
