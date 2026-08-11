// Package cardkit 提供 Feishu 卡片构建的公共助手：文本元素（plain_text、
// div 文本块、按钮组、文本分节）与卡片数据的深拷贝/取值工具。
//
// 单一事实来源：这些助手此前在 adapter/feishu 主包、cardtransport、
// projector 与 app/daemon 各有一份逐字节相同的复制品（审计 §2.1），
// 已收拢到本包。所有调用方只允许引用 cardkit，禁止在调用点内联。
package cardkit

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// PlainText 构造一个 plain_text 元素，内容会 TrimSpace。
func PlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

// PlainTextBlockElement 构造一个包含 plain_text 的 div 文本块；内容为空时返回 nil。
func PlainTextBlockElement(content string) map[string]any {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "plain_text",
			"content": content,
		},
	}
}

// ButtonGroupElement 把按钮列表排版为单个按钮或 column_set；空列表返回 nil。
func ButtonGroupElement(buttons []map[string]any) map[string]any {
	filtered := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button) == 0 {
			continue
		}
		filtered = append(filtered, CloneMap(button))
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		columns := make([]map[string]any, 0, len(filtered))
		for _, button := range filtered {
			columns = append(columns, map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "top",
				"elements":       []map[string]any{button},
			})
		}
		return map[string]any{
			"tag":                "column_set",
			"flex_mode":          "flow",
			"horizontal_spacing": "small",
			"columns":            columns,
		}
	}
}

// AppendTextSections 把规范化后的文本分节追加到元素列表。
func AppendTextSections(elements []map[string]any, sections []control.FeishuCardTextSection) []map[string]any {
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		if normalized.Label != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + normalized.Label + "**",
			})
		}
		if block := PlainTextBlockElement(strings.Join(normalized.Lines, "\n")); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

// CloneMap 深拷贝卡片 map，空 map 返回 nil。
func CloneMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, raw := range value {
		out[key] = CloneAny(raw)
	}
	return out
}

// CloneAny 深拷贝卡片值（map / []map / []any / 标量）。
func CloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return CloneMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, CloneMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, CloneAny(item))
		}
		return out
	default:
		return typed
	}
}

// StringValue 取字符串值；非字符串返回空串。
func StringValue(raw any) string {
	value, _ := raw.(string)
	return value
}
