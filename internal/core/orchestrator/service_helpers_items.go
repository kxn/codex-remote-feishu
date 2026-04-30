package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (b *itemBuffer) replaceText(text string) {
	if text == "" {
		b.textChunks = nil
		b.textValue = ""
		return
	}
	b.textChunks = []string{text}
	b.textValue = text
}

func (b *itemBuffer) appendText(text string) {
	if text == "" {
		return
	}
	b.textChunks = append(b.textChunks, text)
	if len(b.textChunks) == 1 {
		b.textValue = b.textChunks[0]
		return
	}
	b.textValue = ""
}

func (b *itemBuffer) text() string {
	if b == nil {
		return ""
	}
	if b.textValue != "" {
		return b.textValue
	}
	if len(b.textChunks) == 0 {
		return ""
	}
	if len(b.textChunks) == 1 {
		b.textValue = b.textChunks[0]
		return b.textValue
	}
	b.textValue = strings.Join(b.textChunks, "")
	b.textChunks = []string{b.textValue}
	return b.textValue
}

func turnRenderKey(instanceID, threadID, turnID string) string {
	return instanceID + "\x00" + threadID + "\x00" + turnID
}

func threadStateForInstance(inst *state.InstanceRecord) string {
	if !inst.Online {
		return "offline"
	}
	if inst.ActiveTurnID != "" {
		return "running"
	}
	return "idle"
}

func itemBufferKey(instanceID, threadID, turnID, itemID string) string {
	return strings.Join([]string{instanceID, threadID, turnID, itemID}, "::")
}

func (s *Service) ensureItemBuffer(instanceID, threadID, turnID, itemID, itemKind string) *itemBuffer {
	key := itemBufferKey(instanceID, threadID, turnID, itemID)
	if existing := s.itemBuffers[key]; existing != nil {
		if existing.ItemKind == "" {
			existing.ItemKind = itemKind
		}
		return existing
	}
	buf := &itemBuffer{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
		ItemID:     itemID,
		ItemKind:   itemKind,
	}
	s.itemBuffers[key] = buf
	return buf
}

func deleteMatchingItemBuffers(buffers map[string]*itemBuffer, instanceID, threadID, turnID string) {
	for key, buf := range buffers {
		if buf == nil {
			continue
		}
		if buf.InstanceID != instanceID {
			continue
		}
		if threadID != "" && buf.ThreadID != threadID {
			continue
		}
		if turnID != "" && buf.TurnID != turnID {
			continue
		}
		delete(buffers, key)
	}
}

func tracksTextItem(itemKind string) bool {
	switch itemKind {
	case "agent_message", "plan", "reasoning", "reasoning_summary", "reasoning_content", "command_execution_output":
		return true
	default:
		return false
	}
}

func rendersTextItem(itemKind string) bool {
	switch itemKind {
	case "agent_message":
		return true
	default:
		return false
	}
}

func isImageGenerationItem(itemKind string) bool {
	return strings.TrimSpace(itemKind) == "image_generation"
}

func isContextCompactionItem(itemKind string) bool {
	return strings.TrimSpace(itemKind) == "context_compaction"
}

func isDynamicToolCallItem(itemKind string) bool {
	return strings.TrimSpace(itemKind) == "dynamic_tool_call"
}

func suppressesCompletedTextRender(itemKind string, metadata map[string]any) bool {
	switch strings.TrimSpace(itemKind) {
	case "web_search", "command_execution", "process_plan", "delegated_task":
		return true
	}
	if len(metadata) == 0 {
		return false
	}
	value, ok := metadata["suppressFinalText"].(bool)
	return ok && value
}
