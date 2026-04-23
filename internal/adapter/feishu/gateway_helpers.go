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

func mergeForwardTitle(rawContent string) string {
	return gatewaypkg.MergeForwardTitle(rawContent)
}

func parseFileName(rawContent string) string {
	return gatewaypkg.ParseFileName(rawContent)
}

func gatewayMessageSpeakerLabel(message *gatewayMessage) string {
	if message == nil {
		return ""
	}
	return gatewaypkg.GatewayMessageSpeakerLabel(message.SenderID, message.SenderType)
}

func surfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID string) string {
	return gatewaypkg.SurfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID)
}

func ResolveReceiveTarget(chatID, actorUserID string) (string, string) {
	return gatewaypkg.ResolveReceiveTarget(chatID, actorUserID)
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

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringMapValue(values map[string]interface{}, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch current := value.(type) {
	case string:
		return current
	default:
		return fmt.Sprint(current)
	}
}

func boolMapValue(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	current, _ := value.(bool)
	return current
}

func intMapValue(values map[string]interface{}, key string) int {
	if len(values) == 0 {
		return 0
	}
	value, ok := values[key]
	if !ok || value == nil {
		return 0
	}
	switch current := value.(type) {
	case int:
		return current
	case int8:
		return int(current)
	case int16:
		return int(current)
	case int32:
		return int(current)
	case int64:
		return int(current)
	case uint:
		return int(current)
	case uint8:
		return int(current)
	case uint16:
		return int(current)
	case uint32:
		return int(current)
	case uint64:
		return int(current)
	case float32:
		return int(current)
	case float64:
		return int(current)
	case string:
		current = strings.TrimSpace(current)
		if current == "" {
			return 0
		}
		var parsed int
		_, _ = fmt.Sscanf(current, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}
