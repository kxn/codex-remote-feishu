package privatechat

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestCodexGoalAndThreadReadOutcomeRequiresExactResponse(t *testing.T) {
	for _, tc := range []struct {
		kind   agentproto.CommandKind
		event  agentproto.EventKind
		method string
	}{
		{agentproto.CommandThreadGoalGet, agentproto.EventThreadGoalCommandResult, "thread/goal/get"},
		{agentproto.CommandThreadGoalSet, agentproto.EventThreadGoalCommandResult, "thread/goal/set"},
		{agentproto.CommandThreadGoalClear, agentproto.EventThreadGoalCommandResult, "thread/goal/clear"},
		{agentproto.CommandThreadRead, agentproto.EventThreadRuntimeStatusUpdated, "thread/read"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			command := agentproto.Command{CommandID: "command", Kind: tc.kind, Target: agentproto.Target{ThreadID: "thread"}, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Backend: agentproto.BackendCodex}}}
			event := agentproto.Event{Kind: tc.event, CommandID: "command", RequestID: "rpc", ThreadID: "thread", Status: "accepted"}
			if state, method, ok := nativeEventOutcome(command, event); !ok || state != "accepted" || method != tc.method {
				t.Fatalf("outcome: %q %q %v", state, method, ok)
			}
			for name, mutate := range map[string]func(*agentproto.Event){"other command": func(e *agentproto.Event) { e.CommandID = "other" }, "other thread": func(e *agentproto.Event) { e.ThreadID = "other" }, "no rpc": func(e *agentproto.Event) { e.RequestID = "" }, "other kind": func(e *agentproto.Event) { e.Kind = agentproto.EventTurnStarted }} {
				x := event
				mutate(&x)
				if _, _, ok := nativeEventOutcome(command, x); ok {
					t.Fatalf("%s accepted", name)
				}
			}
			if tc.kind == agentproto.CommandThreadRead {
				event.ErrorMessage = "rejected"
			} else {
				event.Status = "failed"
			}
			if state, method, ok := nativeEventOutcome(command, event); !ok || state != "failed" || method != tc.method {
				t.Fatalf("failure: %q %q %v", state, method, ok)
			}
		})
	}
}
