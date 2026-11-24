package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestShellOutcomeRequiresExactNativeRPC(t *testing.T) {
	owner := agentproto.NativeOwner{Backend: agentproto.BackendCodex}
	command := agentproto.Command{CommandID: "shell", Kind: agentproto.CommandThreadShellCommand, NativeBinding: &agentproto.NativeTurnBinding{Owner: owner}, Target: agentproto.Target{ThreadID: "session", TurnID: "old-turn"}, ShellCommand: agentproto.ShellCommand{OperationID: "op"}}
	event := agentproto.Event{Kind: agentproto.EventNativeShellRPCResult, CommandID: "shell", RequestID: "rpc", ThreadID: "session", Status: "acknowledged", Metadata: map[string]any{"shellOperationID": "op"}}
	for _, tc := range []struct {
		name   string
		mutate func(*agentproto.Event)
		want   string
	}{{"accepted", func(*agentproto.Event) {}, "accepted"}, {"failed", func(e *agentproto.Event) { e.Status = "failed" }, "failed"}, {"unknown", func(e *agentproto.Event) { e.Status = "unknown" }, ""}, {"wrong command", func(e *agentproto.Event) { e.CommandID = "wrong" }, ""}, {"wrong session", func(e *agentproto.Event) { e.ThreadID = "wrong" }, ""}, {"wrong operation", func(e *agentproto.Event) { e.Metadata = map[string]any{"shellOperationID": "wrong"} }, ""}, {"no request", func(e *agentproto.Event) { e.RequestID = "" }, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			e := event
			tc.mutate(&e)
			outcome, method, ok := nativeEventOutcome(command, e)
			if outcome != tc.want || ok != (tc.want != "") || ok && method != "thread/shellCommand" {
				t.Fatalf("outcome=%q method=%q definitive=%v", outcome, method, ok)
			}
		})
	}
}

func TestReviewAndCompactOutcomeRequiresExactNativeResponse(t *testing.T) {
	for _, tc := range []struct {
		kind           agentproto.CommandKind
		action, method string
	}{{agentproto.CommandReviewStart, "review.start", "review/start"}, {agentproto.CommandThreadCompactStart, "thread.compact.start", "thread/compact/start"}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			c := agentproto.Command{CommandID: "command", Kind: tc.kind, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Backend: agentproto.BackendCodex}}, Target: agentproto.Target{ThreadID: "thread"}}
			e := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: "command", RequestID: "rpc", ThreadID: "thread", Action: tc.action, Status: "accepted"}
			state, method, ok := nativeEventOutcome(c, e)
			if !ok || state != "accepted" || method != tc.method {
				t.Fatalf("valid outcome %s %s %v", state, method, ok)
			}
			e.ThreadID = "other"
			if _, _, ok = nativeEventOutcome(c, e); ok {
				t.Fatal("wrong thread accepted")
			}
		})
	}
}
