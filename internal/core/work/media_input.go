package work

import (
	"encoding/json"
	"strings"
)

// StandaloneMedia preserves the private-chat contract: a standalone image or
// file waits for a subsequent human instruction. Download annotations and
// quoted content are source material, never a substitute for that instruction.
func StandaloneMedia(envelope RawIngressEnvelope) bool {
	switch envelope.MessageType {
	case "image", "file", "audio", "media":
		return true
	case "post":
		var value any
		if json.Unmarshal([]byte(envelope.RawContent), &value) != nil {
			return true
		}
		return !postHasInstruction(value)
	default:
		return false
	}
}
func postHasInstruction(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if title, ok := v["title"].(string); ok && strings.TrimSpace(title) != "" {
			return true
		}
		if tag, _ := v["tag"].(string); tag != "" && tag != "img" {
			for _, key := range []string{"text", "href", "user_name", "user_id", "emoji_type"} {
				if text, ok := v[key].(string); ok && strings.TrimSpace(text) != "" {
					return true
				}
			}
		}
		for _, key := range []string{"content", "zh_cn", "en_us", "ja_jp"} {
			if postHasInstruction(v[key]) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if postHasInstruction(child) {
				return true
			}
		}
	}
	return false
}

// MediaBatchHasInstruction is rechecked by durable admission, so a planner
// cannot turn staged private media into an unsolicited model request.
func MediaBatchHasInstruction(inputs []Inbox) bool {
	for _, input := range inputs {
		if !input.AwaitingText {
			continue
		}
		found := false
		for _, other := range inputs {
			if !other.AwaitingText && other.ControlOfInputID == "" && other.SessionGoalIntentID == "" && other.AutomationEpisodeID == "" && other.MessageID != "" && other.ScopeID == input.ScopeID && other.RecipientContextID == input.RecipientContextID && !other.AvailableAt.Before(input.AvailableAt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
