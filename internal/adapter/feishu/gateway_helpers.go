package feishu

import (
	"fmt"
	"strings"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
)

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

func ResolveReceiveTarget(chatID, actorUserID string) (string, string) {
	return gatewaypkg.ResolveReceiveTarget(chatID, actorUserID)
}

func normalizeGatewayID(gatewayID string) string {
	return strings.TrimSpace(gatewayID)
}

func reactionKey(messageID, emojiType string) string {
	return messageID + "|" + emojiType
}

func mimeExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
