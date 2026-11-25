package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestNativeMissingModelFailureRequiresExactPlanCommand(t *testing.T) {
	for _, tc := range []struct {
		mode           agentproto.PromptExecutionMode
		origin         agentproto.TurnCompletionOrigin
		method, thread string
	}{
		{agentproto.PromptExecutionModeStartNew, agentproto.TurnCompletionOriginThreadStartMissingModel, "thread/start", ""},
		{agentproto.PromptExecutionModeResumeExisting, agentproto.TurnCompletionOriginThreadResumeMissingModel, "thread/resume", "thread"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			c := agentproto.Command{CommandID: "command", Kind: agentproto.CommandPromptSend, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Backend: agentproto.BackendCodex}}, Target: agentproto.Target{ExecutionMode: tc.mode, ThreadID: tc.thread}, Overrides: agentproto.PromptOverrides{PlanMode: "on"}}
			e := agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "command", RequestID: "rpc", ThreadID: tc.thread, Status: "failed", TurnCompletionOrigin: tc.origin, ErrorMessage: "native response omitted model"}
			state, method, ok := nativeEventOutcome(c, e)
			if !ok || state != "failed" || method != tc.method {
				t.Fatalf("outcome=%s method=%s ok=%v", state, method, ok)
			}
			changed := c
			changed.Overrides.Model = "explicit"
			if _, _, ok = nativeEventOutcome(changed, e); ok {
				t.Fatal("explicit model incorrectly rejected")
			}
			changed = c
			changed.Overrides.PlanMode = ""
			if _, _, ok = nativeEventOutcome(changed, e); ok {
				t.Fatal("no plan incorrectly rejected")
			}
			e.RequestID = ""
			if _, _, ok = nativeEventOutcome(c, e); ok {
				t.Fatal("missing request accepted")
			}
		})
	}
}
