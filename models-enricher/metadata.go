package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decodeJSON 保留未知数字的精度，并与 Unmarshal 一样拒绝尾随 JSON。
func decodeJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected trailing JSON value")
	}
	return nil
}

// overlayMetadata 从低到高叠层；null 不提供更新，数组作为完整值拷贝。
func overlayMetadata(dst, src map[string]any) {
	for key, value := range src {
		if value == nil {
			continue
		}
		if object, ok := value.(map[string]any); ok {
			current, _ := dst[key].(map[string]any)
			if current == nil {
				current = map[string]any{}
			}
			overlayMetadata(current, object)
			dst[key] = current
		} else {
			dst[key] = cloneJSONValue(value)
		}
	}
}

func cloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = cloneJSONValue(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	default:
		return value
	}
}

func cloneMap(src map[string]any) map[string]any {
	dst := map[string]any{}
	overlayMetadata(dst, src)
	return dst
}
