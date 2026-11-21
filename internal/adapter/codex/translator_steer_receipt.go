package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
)

func nativeSteerResponse(pending suppressedResponseContext, requestID string, message map[string]any) Result {
	event := agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: pending.CommandID, RequestID: requestID, ThreadID: pending.ThreadID, TurnID: pending.ExpectedTurnID, Status: "accepted", Initiator: pending.Initiator}
	if problem := jsonrpcutil.ExtractErrorMessage(message); problem != "" {
		event.Kind = agentproto.EventNativeTurnInputRejected
		event.Status = "failed"
		event.ErrorMessage = problem
	} else if turnID := lookupString(message, "result", "turnId"); turnID == "" || turnID != pending.ExpectedTurnID {
		event.Kind = agentproto.EventNativeTurnInputRejected
		event.Status = "unknown"
		event.ErrorMessage = "Native steer response did not identify the expected turn."
	}
	return Result{Suppress: true, Events: []agentproto.Event{event}}
}
