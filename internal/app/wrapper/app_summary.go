package wrapper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func summarizeFrames(lines [][]byte) string {
	if len(lines) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, summarizeFrame(line))
	}
	return strings.Join(parts, "; ")
}

func summarizeEventKinds(events []agentproto.Event) string {
	if len(events) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(events))
	for _, event := range events {
		part := string(event.Kind)
		if event.ThreadID != "" {
			part += " thread=" + event.ThreadID
		}
		if event.TurnID != "" {
			part += " turn=" + event.TurnID
		}
		if event.Initiator.Kind != "" {
			part += " initiator=" + string(event.Initiator.Kind)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func summarizeFrame(line []byte) string {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Sprintf("raw=%q", previewRawLine(line))
	}
	parts := []string{}
	if id := lookupStringFromMap(message, "id"); id != "" {
		parts = append(parts, "id="+id)
	}
	if method := lookupStringFromMap(message, "method"); method != "" {
		parts = append(parts, "method="+method)
	}
	if threadID := xutil.FirstNonEmpty(
		lookupNestedString(message, "params", "threadId"),
		lookupNestedString(message, "params", "thread", "id"),
		lookupNestedString(message, "result", "thread", "id"),
	); threadID != "" {
		parts = append(parts, "thread="+threadID)
	}
	if turnID := xutil.FirstNonEmpty(
		lookupNestedString(message, "params", "turnId"),
		lookupNestedString(message, "params", "expectedTurnId"),
		lookupNestedString(message, "params", "turn", "id"),
		lookupNestedString(message, "result", "turn", "id"),
	); turnID != "" {
		parts = append(parts, "turn="+turnID)
	}
	if cwd := lookupNestedString(message, "params", "cwd"); cwd != "" {
		parts = append(parts, "cwd="+cwd)
	}
	if inputs := countJSONArray(message, "params", "input"); inputs > 0 {
		parts = append(parts, fmt.Sprintf("inputs=%d", inputs))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("raw=%q", previewRawLine(line))
	}
	return strings.Join(parts, " ")
}

func lookupStringFromMap(message map[string]any, key string) string {
	value, ok := message[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func lookupNestedString(message map[string]any, path ...string) string {
	var current any = message
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = asMap[key]
		if !ok {
			return ""
		}
	}
	switch value := current.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func countJSONArray(message map[string]any, path ...string) int {
	var current any = message
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current, ok = asMap[key]
		if !ok {
			return 0
		}
	}
	values, ok := current.([]any)
	if !ok {
		return 0
	}
	return len(values)
}

func previewRawLine(line []byte) string {
	text := strings.TrimSpace(string(line))
	if len(text) > 200 {
		return text[:200] + "..."
	}
	return text
}
