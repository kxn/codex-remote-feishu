package claude

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// RegisterUserCommandUUID is enabled by the managed wrapper only. The UUID is
// written into that exact user frame and correlated by Claude's native echo.
func (t *Translator) RegisterUserCommandUUID(uuid string, command agentproto.Command) {
	if t.nativeUserCommands == nil {
		t.nativeUserCommands = make(map[string]agentproto.Command)
	}
	t.nativeUserCommands[uuid] = command
	for _, turn := range t.pendingTurns {
		if turn.CommandID == command.CommandID {
			turn.UserCommandUUID = uuid
			turn.TurnID = "relay-claude-turn-" + uuid
		}
	}
}

func (t *Translator) observeCommandLifecycle(message map[string]any) Result {
	uuid := strings.TrimSpace(xutil.LookupStringFromAny(message["command_uuid"]))
	command, known := t.nativeUserCommands[uuid]
	if !known {
		return Result{}
	}
	state := strings.TrimSpace(xutil.LookupStringFromAny(message["state"]))
	for _, turn := range t.pendingTurns {
		if turn.CommandID == command.CommandID {
			turn.UserCommandUUIDObserved = true
		}
	}
	if t.activeTurn != nil && t.activeTurn.CommandID == command.CommandID {
		t.activeTurn.UserCommandUUIDObserved = true
	}
	threadID := strings.TrimSpace(xutil.LookupStringFromAny(message["session_id"]))
	if threadID == "" {
		threadID = t.sessionID
	}
	turnID := ""
	var terminal *turnState
	if state == "started" {
		if t.activeTurn != nil {
			turnID = t.activeTurn.TurnID
		} else {
			if command.Kind == agentproto.CommandTurnSteer {
				t.pendingTurns = append(t.pendingTurns, &turnState{CommandID: command.CommandID, Initiator: commandInitiator(command), ThreadID: threadID, TurnID: "relay-claude-turn-" + uuid, UserCommandUUID: uuid, UserCommandUUIDObserved: true})
			}
			// Select only the turn proven by this native UUID, not whichever
			// command happened to be newest when the lifecycle notification arrived.
			for index, turn := range t.pendingTurns {
				if turn.CommandID == command.CommandID {
					t.pendingTurns = append([]*turnState{turn}, append(t.pendingTurns[:index], t.pendingTurns[index+1:]...)...)
					turnID = turn.TurnID
					break
				}
			}
		}
	}
	if state == "cancelled" || state == "completed" {
		for index, turn := range t.pendingTurns {
			if turn.CommandID == command.CommandID {
				terminal = turn
				turnID = turn.TurnID
				t.pendingTurns = append(t.pendingTurns[:index], t.pendingTurns[index+1:]...)
				t.completedTurn = turn
				break
			}
		}
		delete(t.nativeUserCommands, uuid)
	}
	events := []agentproto.Event{{Kind: agentproto.EventNativeInputReceipt, CommandID: command.CommandID, ThreadID: threadID, TurnID: turnID, RequestID: uuid, Status: state, Action: "claude.command_lifecycle"}}
	if terminal != nil {
		status := "completed"
		if state == "cancelled" {
			status = "interrupted"
		}
		events = append(events, agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: command.CommandID, ThreadID: threadID, TurnID: terminal.TurnID, TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime, Status: status, Metadata: map[string]any{"nativeBoundary": "command_lifecycle." + state}})
	}
	return Result{Events: events}
}
