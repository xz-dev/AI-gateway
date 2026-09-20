package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// validateConfigVersion：校验输出 schema 版本；变更时 Ansible 端同步升级。
const validateConfigVersion = 1

// validateConfigResult 只携带版本与输入字节摘要：不泄露 YAML 内容，
// 摘要与远端 `sha256sum`/Ansible `stat` checksum 语义一致，供一致性比对。
type validateConfigResult struct {
	Version      int    `json:"version"`
	ConfigDigest string `json:"config_digest"`
}

// runValidateConfig 校验候选配置且仅做纯静态检查：
// 复用与启动相同的 loadConfig 路径（YAML 解析、必填字段、provider 前缀、
// source token、include/exclude 正则编译），不发起任何网络请求、不监听端口。
// path 为 "-" 时从 stdin 读取（Ansible 流式传输候选文件，无需落地临时文件）。
// 错误仅含字段路径等有限信息，绝不回显 YAML 内容或凭证。
func runValidateConfig(path string, stdin io.Reader, stdout, stderr io.Writer) int {
	if path == "" {
		fmt.Fprintln(stderr, "usage: models-enricher validate-config <path|->")
		return 2
	}
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(io.LimitReader(stdin, 16<<20))
		if err != nil {
			fmt.Fprintf(stderr, "read config: %s\n", err.Error())
			return 1
		}
	} else {
		raw, err = os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "read config: %s\n", err.Error())
			return 1
		}
	}
	tmp, err := os.CreateTemp("", "models-enricher-config-*.yaml")
	if err != nil {
		fmt.Fprintf(stderr, "validate config: %s\n", err.Error())
		return 1
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		fmt.Fprintf(stderr, "validate config: %s\n", err.Error())
		return 1
	}
	if err := tmp.Close(); err != nil {
		fmt.Fprintf(stderr, "validate config: %s\n", err.Error())
		return 1
	}
	if _, err := loadConfig(tmpPath); err != nil {
		fmt.Fprintf(stderr, "invalid config: %s\n", err.Error())
		return 1
	}
	out, _ := json.Marshal(validateConfigResult{
		Version:      validateConfigVersion,
		ConfigDigest: fmt.Sprintf("%x", sha256.Sum256(raw)),
	})
	fmt.Fprintf(stdout, "%s\n", out)
	return 0
}
