package acp

import (
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func newHistoryHydration(command agentproto.Command, cwd string) *historyHydrationState {
	return &historyHydrationState{
		CommandID: command.CommandID,
		ThreadID:  strings.TrimSpace(command.Target.ThreadID),
		CWD:       strings.TrimSpace(cwd),
		current:   -1,
		itemByKey: map[string]historyItemRef{},
	}
}

func (h *historyHydrationState) observeUpdate(update map[string]any) {
	switch strings.TrimSpace(xutil.LookupStringFromAny(update["sessionUpdate"])) {
	case "user_message_chunk":
		h.observeHistoryText(update, "user_message")
	case "agent_message_chunk":
		h.observeHistoryText(update, "agent_message")
	case "agent_thought_chunk":
		h.observeHistoryText(update, "reasoning_summary")
	case "tool_call":
		h.observeHistoryToolCall(update)
	case "tool_call_update":
		h.observeHistoryToolCallUpdate(update)
	}
}

func (h *historyHydrationState) observeHistoryText(update map[string]any, kind string) {
	text := historyContentText(update["content"])
	if text == "" {
		return
	}
	messageID := xutil.FirstNonEmpty(xutil.LookupStringFromAny(update["messageId"]), kind)
	turnIndex := h.current
	if kind == "user_message" {
		key := "user\x00" + messageID
		if ref, ok := h.itemByKey[key]; ok {
			appendHistoryText(&h.Turns[ref.Turn].Items[ref.Item], text)
			h.current = ref.Turn
			return
		}
		turnIndex = h.addTurn(messageID)
	} else if turnIndex < 0 {
		turnIndex = h.addTurn(messageID)
	}
	key := kind + "\x00" + messageID
	ref := h.ensureHistoryItem(turnIndex, key, agentproto.ThreadHistoryItemRecord{
		ItemID: "opencode-history-" + kind + "-" + sanitizeID(messageID),
		Kind:   kind,
		Status: "completed",
	})
	appendHistoryText(&h.Turns[ref.Turn].Items[ref.Item], text)
}

func (h *historyHydrationState) observeHistoryToolCall(update map[string]any) {
	toolID := xutil.LookupStringFromAny(update["toolCallId"])
	if toolID == "" {
		return
	}
	turnIndex := h.current
	if turnIndex < 0 {
		turnIndex = h.addTurn(toolID)
	}
	ref := h.ensureHistoryItem(turnIndex, "tool\x00"+toolID, agentproto.ThreadHistoryItemRecord{
		ItemID: "opencode-history-tool-" + sanitizeID(toolID),
		Kind:   toolItemKind(update),
		Status: xutil.FirstNonEmpty(xutil.LookupStringFromAny(update["status"]), "pending"),
	})
	item := &h.Turns[ref.Turn].Items[ref.Item]
	if kind := toolItemKind(update); kind != "" {
		item.Kind = kind
	}
	if status := strings.TrimSpace(xutil.LookupStringFromAny(update["status"])); status != "" {
		item.Status = status
	}
	h.mergeHistoryMetadata(item, map[string]any{
		"toolCallId": toolID,
		"title":      xutil.LookupStringFromAny(update["title"]),
		"kind":       xutil.LookupStringFromAny(update["kind"]),
		"rawInput":   xutil.CloneJSONValue(update["rawInput"]),
	})
	if item.Kind == "command_execution" {
		if rawInput, _ := update["rawInput"].(map[string]any); rawInput != nil {
			item.Command = xutil.LookupStringFromAny(rawInput["cmd"])
			item.CWD = xutil.LookupStringFromAny(rawInput["cwd"])
		}
	}
}

func (h *historyHydrationState) observeHistoryToolCallUpdate(update map[string]any) {
	toolID := xutil.LookupStringFromAny(update["toolCallId"])
	if toolID == "" {
		return
	}
	turnIndex := h.current
	if turnIndex < 0 {
		turnIndex = h.addTurn(toolID)
	}
	ref := h.ensureHistoryItem(turnIndex, "tool\x00"+toolID, agentproto.ThreadHistoryItemRecord{
		ItemID: "opencode-history-tool-" + sanitizeID(toolID),
		Kind:   toolItemKind(update),
		Status: xutil.FirstNonEmpty(xutil.LookupStringFromAny(update["status"]), "in_progress"),
	})
	item := &h.Turns[ref.Turn].Items[ref.Item]
	if kind := toolItemKind(update); kind != "" && kind != "tool_call" {
		item.Kind = kind
	}
	if status := strings.TrimSpace(xutil.LookupStringFromAny(update["status"])); status != "" {
		item.Status = status
	}
	if text := historyContentText(update["content"]); text != "" {
		appendHistoryText(item, text)
	}
	h.mergeHistoryMetadata(item, map[string]any{
		"toolCallId": toolID,
		"title":      xutil.LookupStringFromAny(update["title"]),
		"rawOutput":  xutil.CloneJSONValue(update["rawOutput"]),
	})
	if item.Kind == "command_execution" {
		if rawOutput, _ := update["rawOutput"].(map[string]any); rawOutput != nil {
			if _, ok := rawOutput["exitCode"]; ok {
				exitCode := xutil.LookupIntFromAny(rawOutput["exitCode"])
				item.ExitCode = &exitCode
			}
		}
	}
}

func (h *historyHydrationState) addTurn(seed string) int {
	turnID := "opencode-history-turn-" + sanitizeID(seed)
	h.Turns = append(h.Turns, agentproto.ThreadHistoryTurnRecord{
		TurnID: turnID,
		Status: "completed",
	})
	h.current = len(h.Turns) - 1
	return h.current
}

func (h *historyHydrationState) ensureHistoryItem(turnIndex int, key string, item agentproto.ThreadHistoryItemRecord) historyItemRef {
	if ref, ok := h.itemByKey[key]; ok {
		return ref
	}
	if item.ItemID == "" {
		item.ItemID = "opencode-history-item-" + strconv.Itoa(len(h.itemByKey)+1)
	}
	ref := historyItemRef{Turn: turnIndex, Item: len(h.Turns[turnIndex].Items)}
	h.Turns[turnIndex].Items = append(h.Turns[turnIndex].Items, item)
	h.itemByKey[key] = ref
	return ref
}

func (h *historyHydrationState) mergeHistoryMetadata(item *agentproto.ThreadHistoryItemRecord, values map[string]any) {
	for key, value := range values {
		if value == nil {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata[key] = value
	}
}

func appendHistoryText(item *agentproto.ThreadHistoryItemRecord, text string) {
	if text == "" {
		return
	}
	item.Text += text
}

func historyContentText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := historyContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		contentType := strings.TrimSpace(xutil.LookupStringFromAny(typed["type"]))
		switch contentType {
		case "text":
			return xutil.LookupStringFromAny(typed["text"])
		case "content":
			return historyContentText(typed["content"])
		case "resource_link":
			return xutil.FirstNonEmpty(xutil.LookupStringFromAny(typed["name"]), xutil.LookupStringFromAny(typed["uri"]))
		case "resource":
			if resource, _ := typed["resource"].(map[string]any); resource != nil {
				return xutil.FirstNonEmpty(xutil.LookupStringFromAny(resource["text"]), xutil.LookupStringFromAny(resource["uri"]))
			}
		default:
			if text := xutil.LookupStringFromAny(typed["text"]); text != "" {
				return text
			}
		}
	}
	return ""
}
