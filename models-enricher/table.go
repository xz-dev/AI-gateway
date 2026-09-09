package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"
)

// 保持表格的渠道自身目录请求版本不变；CPA原生目录独立固定为1。
const tableInventoryClientVersion = "v0.65.0"

//go:embed models-table.html
var tableHTML string

var modelsTable = template.Must(template.New("models-table").Parse(tableHTML))

// 裸模型（无供应商前缀）在前，组内按slug字母序。
func slugLess(a, b string) bool {
	aBare, bBare := !strings.Contains(a, "/"), !strings.Contains(b, "/")
	if aBare != bBare {
		return aBare
	}
	return a < b
}

var tableColumns = []struct{ Label, Key string }{
	{"slug", "slug"},
	{"display_name", "display_name"},
	{"input_modalities", "input_modalities"},
	{"output_modalities", "output_modalities"},
	{"context_window", "context_window"},
	{"max_input_tokens", "max_input_tokens"},
	{"max_tokens(abandon)", "max_tokens"},
	{"max_completion_tokens", "max_completion_tokens"},
	{"max_output_tokens", "max_output_tokens"},
	{"reasoning_levels", "supported_reasoning_levels"},
	{"default_reasoning", "default_reasoning_level"},
}

type tableCell struct {
	Text string
	Null bool
}

// 输入是管线已编码的JSON，数字通过decodeJSON保留为json.Number。
func modelTableCell(model map[string]any, key string) tableCell {
	v, exists := model[key]
	if !exists {
		return tableCell{"未知", true}
	}
	if v == nil {
		return tableCell{"null", true}
	}
	if s, ok := v.(string); ok && s != "" {
		return tableCell{Text: s}
	}
	var text bytes.Buffer
	encoder := json.NewEncoder(&text)
	encoder.SetEscapeHTML(false) // html/template在HTML边界转义，不改变JSON文本中的字符。
	_ = encoder.Encode(v)
	return tableCell{Text: strings.TrimSuffix(text.String(), "\n")}
}

// 不缓存最终HTML，不重新请求JSON端点；复用当前单飞构建的结果。
func writeModelsTable(w http.ResponseWriter, status int, body []byte) {
	data := struct {
		Columns     []struct{ Label, Key string }
		Rows        [][]tableCell
		Meta, Error string
	}{Columns: tableColumns}
	if status == http.StatusOK {
		var manifest Manifest
		if err := decodeJSON(body, &manifest); err != nil {
			status, body = errorJSON(http.StatusInternalServerError, "manifest_decode_failed", err.Error())
		} else {
			sort.SliceStable(manifest.Models, func(i, j int) bool {
				return slugLess(asString(manifest.Models[i]["slug"]), asString(manifest.Models[j]["slug"]))
			})
			for _, model := range manifest.Models {
				row := make([]tableCell, len(tableColumns))
				for i, col := range tableColumns {
					row[i] = modelTableCell(model, col.Key)
				}
				data.Rows = append(data.Rows, row)
			}
			data.Meta = fmt.Sprintf("%d models · client_version=%s · %s", len(data.Rows), cpaCatalogClientVersion, time.Now().UTC().Format(time.RFC3339))
		}
	}
	if status != http.StatusOK {
		data.Meta = fmt.Sprintf("HTTP %d", status)
		data.Error = string(body)
	}
	var html bytes.Buffer
	if err := modelsTable.Execute(&html, data); err != nil {
		http.Error(w, "table rendering failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(html.Bytes())
}
