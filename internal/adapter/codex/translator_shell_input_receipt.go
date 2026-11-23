package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
)

// thread/shellCommand has no expectedTurn parameter. Its empty RPC result
// acknowledges the session-level request, not delivery to a particular turn.
// Only the later exact UserShell item can prove the command's actual lifecycle.
func nativeShellInputResponse(pending suppressedResponseContext, requestID string, message map[string]any) Result {
	event := agentproto.Event{Kind: agentproto.EventNativeShellRPCResult, CommandID: pending.CommandID, RequestID: requestID, ThreadID: pending.ThreadID, Status: "acknowledged", Metadata: map[string]any{"shellOperationID": pending.ShellOperationID}, Initiator: pending.Initiator}
	if problem := jsonrpcutil.ExtractErrorMessage(message); problem != "" {
		event.Status = "failed"
		event.ErrorMessage = problem
	} else if _, ok := message["result"].(map[string]any); !ok {
		event.Status = "unknown"
		event.ErrorMessage = "Native shell request response did not confirm receipt."
	}
	return Result{Suppress: true, Events: []agentproto.Event{event}}
}
