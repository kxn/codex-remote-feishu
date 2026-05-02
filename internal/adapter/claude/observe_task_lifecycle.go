package claude

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) currentParentToolUseID() string {
	if t.currentMessage == nil {
		return ""
	}
	return strings.TrimSpace(t.currentMessage.ParentToolUseID)
}

func (t *Translator) syncObservedMessageParent(message map[string]any) {
	parentToolUseID := strings.TrimSpace(lookupStringFromAny(message["parent_tool_use_id"]))
	if t.currentMessage == nil {
		t.currentMessage = &messageState{
			ParentToolUseID: parentToolUseID,
			Blocks:          map[int]*blockState{},
		}
		return
	}
	t.currentMessage.ParentToolUseID = parentToolUseID
}

func (t *Translator) hiddenClaudeToolLifecycleEvent(tool *toolState, itemKind string, isError bool, metadata map[string]any) (agentproto.Event, bool) {
	if t.activeTurn == nil || tool == nil {
		return agentproto.Event{}, false
	}
	switch strings.TrimSpace(tool.Name) {
	case "TodoWrite":
		snapshot := buildClaudeTodoPlanSnapshot(tool.Input)
		if snapshot == nil {
			return agentproto.Event{}, false
		}
		return agentproto.Event{
			Kind:         agentproto.EventTurnPlanUpdated,
			CommandID:    t.activeTurn.CommandID,
			ThreadID:     t.activeTurn.ThreadID,
			TurnID:       t.activeTurn.TurnID,
			PlanSnapshot: snapshot,
		}, true
	case "TaskOutput":
		parent := t.delegatedTaskParent(tool)
		if parent == nil {
			return agentproto.Event{}, false
		}
		delta := strings.TrimSpace(firstNonEmptyString(
			stringifyTextContent(metadata["text"]),
			stringifyTextContent(metadata["toolUseResult"]),
		))
		if delta == "" {
			delta = strings.TrimSpace(firstNonEmptyString(
				lookupStringFromAny(metadata["text"]),
				lookupStringFromAny(metadata["toolUseResult"]),
			))
		}
		if delta == "" {
			return agentproto.Event{}, false
		}
		return agentproto.Event{
			Kind:      agentproto.EventItemDelta,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    parent.ItemID,
			ItemKind:  "delegated_task",
			Delta:     delta,
			Metadata:  claudeToolMetadata(parent.Name, parent.Input),
		}, true
	case "TaskStop":
		parent := t.delegatedTaskParent(tool)
		if parent == nil {
			return agentproto.Event{}, false
		}
		eventMetadata := claudeToolMetadata(parent.Name, parent.Input)
		if text := strings.TrimSpace(lookupStringFromAny(metadata["text"])); text != "" {
			eventMetadata["text"] = text
		}
		if errorMessage := strings.TrimSpace(firstNonEmptyString(
			lookupStringFromAny(metadata["error"]),
			lookupStringFromAny(metadata["errorMessage"]),
			lookupStringFromAny(metadata["message"]),
			lookupStringFromAny(metadata["text"]),
		)); errorMessage != "" && isError {
			eventMetadata["errorMessage"] = errorMessage
		}
		parent.Completed = true
		return agentproto.Event{
			Kind:      agentproto.EventItemCompleted,
			CommandID: t.activeTurn.CommandID,
			ThreadID:  t.activeTurn.ThreadID,
			TurnID:    t.activeTurn.TurnID,
			ItemID:    parent.ItemID,
			ItemKind:  "delegated_task",
			Status: map[bool]string{
				true:  "failed",
				false: "completed",
			}[isError],
			Metadata: eventMetadata,
		}, true
	default:
		_ = itemKind
		return agentproto.Event{}, false
	}
}

func (t *Translator) delegatedTaskParent(tool *toolState) *toolState {
	if tool == nil {
		return nil
	}
	parentToolUseID := strings.TrimSpace(tool.ParentToolUseID)
	if parentToolUseID == "" {
		return nil
	}
	parent := t.toolStates[parentToolUseID]
	if parent == nil || strings.TrimSpace(parent.Name) != "Task" {
		return nil
	}
	return parent
}
