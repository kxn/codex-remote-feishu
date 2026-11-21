package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// renderSavedArtifact converts saved source structures into human-readable
// material. It never fetches the moving source or substitutes raw JSON for text.
func renderSavedArtifact(content work.Content) (string, error) {
	if err := content.Validate(); err != nil {
		return "", err
	}
	if strings.HasPrefix(content.MediaType, "text/") {
		return string(content.Data), nil
	}
	if !strings.Contains(content.MediaType, "json") {
		return "", fmt.Errorf("此版本是二进制附件，需要通过真实文件下载入口查看")
	}
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(content.Data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return "", err
	}
	if blocks := raw["blocks"]; blocks != nil {
		var assets []workservice.SavedDocumentAsset
		_ = json.Unmarshal(raw["assets"], &assets)
		return renderSavedDocument(blocks, assets...)
	}
	if values := raw["values"]; values != nil {
		var sheet feishu.ProjectSheetValues
		d := json.NewDecoder(bytes.NewReader(values))
		d.UseNumber()
		if err := d.Decode(&sheet); err != nil {
			return "", err
		}
		text := renderSavedSheet("数值", sheet)
		if formulas := raw["formulas"]; formulas != nil {
			var formula feishu.ProjectSheetValues
			d := json.NewDecoder(bytes.NewReader(formulas))
			d.UseNumber()
			if err := d.Decode(&formula); err != nil {
				return "", err
			}
			text += "\n\n" + renderSavedSheet("公式（与数值分次读取）", formula)
		}
		return text, nil
	}
	if records := raw["records"]; records != nil {
		var rows []struct {
			Fields map[string]any `json:"fields"`
		}
		d := json.NewDecoder(bytes.NewReader(records))
		d.UseNumber()
		if err := d.Decode(&rows); err != nil {
			return "", err
		}
		var out strings.Builder
		for index, row := range rows {
			fmt.Fprintf(&out, "第%d条记录\n", index+1)
			fields := []string{}
			for field := range row.Fields {
				fields = append(fields, field)
			}
			sort.Strings(fields)
			for _, field := range fields {
				out.WriteString(field + "：" + savedCell(row.Fields[field]) + "\n")
			}
			out.WriteString("\n")
		}
		if len(rows) == 0 {
			out.WriteString("本次保存的表格区域没有记录。")
		}
		return out.String(), nil
	}
	return "", fmt.Errorf("这个保存格式尚无可读呈现，原始快照仍完整保留；未将原始JSON当正文发送")
}
func savedCell(value any) string {
	switch v := value.(type) {
	case nil:
		return "（空）"
	case string:
		return v
	case json.Number:
		return string(v)
	case float64:
		return fmt.Sprint(v)
	case bool:
		if v {
			return "是"
		}
		return "否"
	case []any:
		parts := []string{}
		for _, item := range v {
			parts = append(parts, savedCell(item))
		}
		return strings.Join(parts, "、")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			if link, ok := v["link"].(string); ok && link != "" {
				return text + "（" + link + "）"
			}
			return text
		}
		if text, ok := v["name"].(string); ok {
			return text
		}
		if text, ok := v["value"]; ok {
			return savedCell(text)
		}
		return "（复杂单元格，原格式已保存）"
	default:
		return "（未展开的单元格内容）"
	}
}
func renderSavedSheet(label string, sheet feishu.ProjectSheetValues) string {
	area := sheet.Range
	if area == "" {
		area = sheet.RequestedRange
	}
	lines := []string{label + " · 区域：" + area}
	for index, row := range sheet.Values {
		cells := []string{}
		for _, value := range row {
			cells = append(cells, savedCell(value))
		}
		lines = append(lines, fmt.Sprintf("区域第%d行：%s", index+1, strings.Join(cells, " | ")))
	}
	if len(sheet.Values) == 0 {
		lines = append(lines, "本次保存的区域为空。")
	}
	return strings.Join(lines, "\n")
}
func renderSavedDocument(data json.RawMessage, assets ...workservice.SavedDocumentAsset) (string, error) {
	savedAssets := map[string]bool{}
	for _, asset := range assets {
		savedAssets[asset.Token] = true
	}
	var blocks []map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&blocks); err != nil {
		return "", err
	}
	byID := map[string]map[string]any{}
	for _, block := range blocks {
		if id, ok := block["block_id"].(string); ok {
			byID[id] = block
		}
	}
	seen := map[string]bool{}
	var render func(map[string]any) string
	render = func(block map[string]any) string {
		id, _ := block["block_id"].(string)
		if seen[id] {
			return ""
		}
		seen[id] = true
		if table, ok := block["table"].(map[string]any); ok {
			columns := 0
			if prop, ok := table["property"].(map[string]any); ok {
				if n, ok := prop["column_size"].(json.Number); ok {
					columns, _ = strconvInt(n)
				}
			}
			values := []string{}
			if cells, ok := table["cells"].([]any); ok {
				for _, cell := range cells {
					cellID, _ := cell.(string)
					values = append(values, strings.ReplaceAll(render(byID[cellID]), "\n", " / "))
				}
			}
			var rows []string
			if columns < 1 {
				columns = len(values)
			}
			if columns > 0 {
				for i := 0; i < len(values); i += columns {
					end := i + columns
					if end > len(values) {
						end = len(values)
					}
					rows = append(rows, strings.Join(values[i:end], " | "))
				}
			}
			return strings.Join(rows, "\n")
		}
		lines := []string{}
		for _, key := range []string{"text", "heading1", "heading2", "heading3", "heading4", "heading5", "heading6", "heading7", "heading8", "heading9", "bullet", "ordered", "code", "quote", "todo"} {
			if node, ok := block[key].(map[string]any); ok {
				var text strings.Builder
				if elements, ok := node["elements"].([]any); ok {
					for _, element := range elements {
						if fields, ok := element.(map[string]any); ok {
							if run, ok := fields["text_run"].(map[string]any); ok {
								value, _ := run["content"].(string)
								text.WriteString(value)
							} else if equation, ok := fields["equation"].(map[string]any); ok {
								value, _ := equation["content"].(string)
								text.WriteString(value)
							} else {
								text.WriteString("[内嵌引用]")
							}
						}
					}
				}
				lines = append(lines, text.String())
			}
		}
		if image, ok := block["image"].(map[string]any); ok {
			token, _ := image["token"].(string)
			if savedAssets[token] {
				lines = append(lines, "[图片已独立保存，可用下方按钮查看]")
			} else {
				lines = append(lines, "[图片未能独立保存，快照仅保留原始引用]")
			}
		}
		if file, ok := block["file"].(map[string]any); ok {
			token, _ := file["token"].(string)
			name, _ := file["name"].(string)
			if savedAssets[token] {
				lines = append(lines, "[附件已独立保存："+name+"，可用下方按钮下载]")
			} else {
				lines = append(lines, "[附件未能独立保存："+name+"]")
			}
		}
		if children, ok := block["children"].([]any); ok {
			for _, child := range children {
				if childID, ok := child.(string); ok {
					if node := byID[childID]; node != nil {
						lines = append(lines, render(node))
					}
				}
			}
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{}
	for _, block := range blocks {
		if _, ok := block["page"]; ok {
			lines = append(lines, render(block))
		}
	}
	for _, block := range blocks {
		if text := render(block); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n"), nil
}
func strconvInt(number json.Number) (int, error) { n, err := number.Int64(); return int(n), err }
