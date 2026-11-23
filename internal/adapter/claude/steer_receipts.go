package claude

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (t *Translator) observeAdmittedUserEcho(message map[string]any) (Result, bool) {
	uuid := strings.TrimSpace(xutil.LookupStringFromAny(message["uuid"]))
	command, known := t.nativeUserCommands[uuid]
	if !known || uuid == "" {
		return Result{}, false
	}
	session := strings.TrimSpace(xutil.LookupStringFromAny(message["session_id"]))
	if command.NativeInputAdmission == nil || command.Kind != agentproto.CommandTurnSteer {
		if session != "" && session == t.sessionID {
			for _, turn := range t.pendingTurns {
				if turn.UserCommandUUID == uuid {
					turn.UserCommandUUIDObserved = true
				}
			}
			if t.activeTurn != nil && t.activeTurn.UserCommandUUID == uuid {
				t.activeTurn.UserCommandUUIDObserved = true
			}
		}
		return Result{}, false
	}
	return t.admittedSteerReceipt(uuid, session, command, "user_echo"), true
}

func (t *Translator) admittedSteerReceipt(uuid, session string, command agentproto.Command, origin string) Result {
	target := command.Target
	event := agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: uuid, ThreadID: target.ThreadID, TurnID: target.TurnID, Status: "accepted", Initiator: commandInitiator(command), Metadata: map[string]any{"nativeInputUUID": uuid, "nativeInputAcceptance": "claude." + origin}}
	exact := session != "" && session == target.ThreadID && t.activeTurn != nil && !t.activeTurn.NativeInputClosed && t.activeTurn.ThreadID == target.ThreadID && t.activeTurn.TurnID == target.TurnID
	if !exact {
		event.Kind = agentproto.EventNativeTurnInputRejected
		event.Status = "unknown"
		event.ErrorMessage = "Claude echoed the input UUID without proving the admitted active turn. The original input is retained."
		return Result{Events: []agentproto.Event{event}}
	}
	delete(t.nativeUserCommands, uuid)
	receipt := agentproto.Event{Kind: agentproto.EventNativeInputReceipt, CommandID: command.CommandID, RequestID: uuid, ThreadID: target.ThreadID, TurnID: target.TurnID, Status: "started", Action: "claude." + origin, Initiator: commandInitiator(command), Metadata: map[string]any{"nativeInputUUID": uuid}}
	return Result{Events: []agentproto.Event{receipt, event}}
}

func (t *Translator) observeAdmittedSteerLifecycle(uuid string, command agentproto.Command, message map[string]any) Result {
	state := strings.TrimSpace(xutil.LookupStringFromAny(message["state"]))
	session := strings.TrimSpace(xutil.LookupStringFromAny(message["session_id"]))
	if state == "started" {
		return t.admittedSteerReceipt(uuid, session, command, "command_lifecycle.started")
	}
	if state == "queued" {
		return Result{Events: []agentproto.Event{{Kind: agentproto.EventNativeInputReceipt, CommandID: command.CommandID, RequestID: uuid, ThreadID: command.Target.ThreadID, Status: "queued", Action: "claude.command_lifecycle"}}}
	}
	if state == "cancelled" || state == "completed" {
		// A terminal UUID observation without same-turn acceptance does not prove
		// that the original turn consumed this input, or that replay is safe.
		event := agentproto.Event{Kind: agentproto.EventNativeTurnInputRejected, CommandID: command.CommandID, RequestID: uuid, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Status: "unknown", ErrorMessage: "Claude ended the input command without exact same-turn acceptance; input retained.", Metadata: map[string]any{"nativeInputUUID": uuid}}
		delete(t.nativeUserCommands, uuid)
		return Result{Events: []agentproto.Event{{Kind: agentproto.EventNativeInputReceipt, CommandID: command.CommandID, RequestID: uuid, ThreadID: command.Target.ThreadID, Status: state, Action: "claude.command_lifecycle"}, event}}
	}
	return Result{}
}
