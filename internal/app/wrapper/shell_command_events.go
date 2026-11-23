package wrapper

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"strings"
)

func (r *shellCommandRuntime) observeEvents(events []agentproto.Event) []agentproto.Event {
	if r == nil || len(events) == 0 {
		return events
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	output := make([]agentproto.Event, 0, len(events))
	for index, event := range events {
		if !isUserShellEvent(event) {
			output = append(output, event)
			continue
		}
		command, _ := event.Metadata["command"].(string)
		var matched *shellCommandPayload
		for _, payloads := range r.payloads {
			for _, payload := range payloads {
				if payload == nil || payload.commandID == "" {
					continue
				}
				sameItem := payload.itemID != "" && payload.itemID == event.ItemID && payload.actualThreadID == event.ThreadID && payload.actualTurnID == event.TurnID
				firstItem := payload.itemID == "" && event.ThreadID == payload.threadID && event.ItemID != "" && matchesPrivateShellCommand(command, payload.nativeCommand)
				if sameItem || firstItem {
					matched = payload
					break
				}
			}
			if matched != nil {
				break
			}
		}
		if matched != nil {
			matched.itemID = event.ItemID
			matched.actualThreadID = event.ThreadID
			matched.actualTurnID = event.TurnID
			event.CommandID = matched.commandID
			if matched.operationID != "" {
				// A real UserShell item belongs to its own actual native turn. Do not
				// manufacture a model input receipt or assign it to the expected turn.
				switch event.Kind {
				case agentproto.EventItemStarted:
					event.Action = "started"
				case agentproto.EventItemCompleted:
					event.Action = "completed"
				default:
					continue
				}
				event.Kind = agentproto.EventNativeShellItemObserved
				if event.Metadata == nil {
					event.Metadata = map[string]any{}
				}
				event.Metadata["shellOperationID"] = matched.operationID
				event.Execution = agentproto.CloneExecutionBinding(matched.execution)
			}
			events[index] = event
			output = append(output, event)
			if event.Kind == agentproto.EventItemCompleted || event.Kind == agentproto.EventNativeShellItemObserved && event.Action == "completed" {
				r.removePayloadLocked(matched.path)
			}
			continue
		}
		// Compatibility cleanup for the old unstamped shell helper. It has no typed
		// operation or authority and can never annotate a managed command's item.
		payloads := r.payloads[shellPayloadKey(event.ThreadID, event.TurnID)]
		for _, payload := range payloads {
			if payload == nil || payload.commandID != "" {
				continue
			}
			if event.Kind == agentproto.EventItemStarted && payload.itemID == "" {
				payload.itemID = strings.TrimSpace(event.ItemID)
				break
			}
			if event.Kind == agentproto.EventItemCompleted && (payload.itemID == "" || payload.itemID == event.ItemID) {
				r.removePayloadLocked(payload.path)
				break
			}
		}
		output = append(output, event)
	}
	return output
}
