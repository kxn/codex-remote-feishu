package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseMergeForwardContent(rawContent string) (string, error) {
	rawContent = strings.TrimSpace(rawContent)
	if rawContent == "" {
		return "", fmt.Errorf("empty merge_forward content")
	}
	if !looksLikeJSONObject(rawContent) {
		return rawContent, nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(rawContent), &decoded); err != nil {
		return "", err
	}
	lines := make([]string, 0, 8)
	seen := map[string]struct{}{}
	appendLine := func(text string) {
		text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		lines = append(lines, text)
	}
	collectMergeForwardLines(decoded, appendLine)
	if len(lines) == 0 {
		return "", fmt.Errorf("empty merge_forward content")
	}
	const maxLines = 24
	if len(lines) > maxLines {
		remaining := len(lines) - maxLines
		lines = append(lines[:maxLines], fmt.Sprintf("...（其余 %d 条省略）", remaining))
	}
	return strings.Join(lines, "\n"), nil
}

func gatewayMessageSpeakerLabel(message *gatewayMessage) string {
	if message == nil {
		return ""
	}
	senderID := strings.TrimSpace(message.SenderID)
	senderType := strings.ToLower(strings.TrimSpace(message.SenderType))
	switch senderType {
	case "user":
		if senderID == "" {
			return "用户"
		}
		return "用户(" + senderID + ")"
	case "app":
		if senderID == "" {
			return "应用"
		}
		return "应用(" + senderID + ")"
	case "anonymous":
		if senderID == "" {
			return "匿名"
		}
		return "匿名(" + senderID + ")"
	case "unknown":
		if senderID == "" {
			return "未知发送者"
		}
		return "未知发送者(" + senderID + ")"
	default:
		if senderType != "" && senderID != "" {
			return senderType + "(" + senderID + ")"
		}
		if senderID != "" {
			return "发送者(" + senderID + ")"
		}
		if senderType != "" {
			return senderType
		}
		return ""
	}
}

func mergeForwardTitle(rawContent string) string {
	rawContent = strings.TrimSpace(rawContent)
	if rawContent == "" || strings.EqualFold(rawContent, "Merged and Forwarded Message") {
		return ""
	}
	if !looksLikeJSONObject(rawContent) {
		return rawContent
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawContent), &payload); err != nil {
		return ""
	}
	return firstJSONString(payload, "title", "topic", "chat_name", "chat_title")
}

func collectMergeForwardLines(value any, appendLine func(string)) {
	switch current := value.(type) {
	case map[string]any:
		title := firstJSONString(current, "title", "topic", "chat_name", "chat_title")
		speaker := firstJSONString(current, "sender_name", "user_name", "name", "from_name", "sender")
		text := firstJSONString(current, "text", "message", "summary", "description", "desc")
		if text == "" {
			content := strings.TrimSpace(firstJSONString(current, "content"))
			if content != "" && !looksLikeJSONObject(content) {
				text = content
			}
		}
		if title != "" {
			appendLine(title)
		}
		if text != "" {
			if speaker != "" && !strings.EqualFold(speaker, text) {
				appendLine(speaker + ": " + text)
			} else {
				appendLine(text)
			}
		} else if len(linesFromMessageIDs(current)) > 0 {
			for _, line := range linesFromMessageIDs(current) {
				appendLine(line)
			}
		}
		for _, key := range []string{"items", "messages", "message_list", "children", "content"} {
			child, ok := current[key]
			if !ok {
				continue
			}
			collectMergeForwardLines(child, appendLine)
		}
		for key, child := range current {
			switch key {
			case "title", "topic", "chat_name", "chat_title",
				"sender_name", "user_name", "name", "from_name", "sender",
				"text", "message", "summary", "description", "desc",
				"content", "items", "messages", "message_list", "children",
				"message_id_list":
				continue
			}
			collectMergeForwardLines(child, appendLine)
		}
	case []any:
		for _, item := range current {
			collectMergeForwardLines(item, appendLine)
		}
	case string:
		text := strings.TrimSpace(current)
		if text == "" {
			return
		}
		if looksLikeJSONObject(text) {
			var nested any
			if err := json.Unmarshal([]byte(text), &nested); err == nil {
				collectMergeForwardLines(nested, appendLine)
				return
			}
		}
		appendLine(text)
	}
}

func linesFromMessageIDs(payload map[string]any) []string {
	raw, ok := payload["message_id_list"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("包含 %d 条转发消息", len(items))}
}

func looksLikeJSONObject(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		return true
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return true
	}
	return false
}

func firstJSONString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case string:
			if text := strings.TrimSpace(current); text != "" {
				return text
			}
		}
	}
	return ""
}

func parseFileName(rawContent string) string {
	var payload struct {
		FileName string `json:"file_name"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawContent), &payload); err != nil {
		return ""
	}
	if name := strings.TrimSpace(payload.FileName); name != "" {
		return name
	}
	return strings.TrimSpace(payload.Name)
}
