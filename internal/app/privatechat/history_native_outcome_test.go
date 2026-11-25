package privatechat

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestCodexHistoryFailureRequiresExactResponse(t *testing.T) {
	c := agentproto.Command{CommandID: "history", Kind: agentproto.CommandThreadHistoryRead, Target: agentproto.Target{ThreadID: "thread"}, NativeBinding: &agentproto.NativeTurnBinding{Owner: agentproto.NativeOwner{Backend: agentproto.BackendCodex}}}
	e := agentproto.Event{Kind: agentproto.EventThreadHistoryRead, CommandID: "history", RequestID: "rpc", ThreadID: "thread", Status: "failed", ErrorMessage: "not found"}
	if state, method, ok := nativeEventOutcome(c, e); !ok || state != "failed" || method != "thread/read" {
		t.Fatalf("failure=%s %s %v", state, method, ok)
	}
	for _, mutate := range []func(*agentproto.Event){func(x *agentproto.Event) { x.ThreadID = "other" }, func(x *agentproto.Event) { x.RequestID = "" }, func(x *agentproto.Event) { x.ErrorMessage = "" }, func(x *agentproto.Event) { x.CommandID = "other" }} {
		copy := e
		mutate(&copy)
		if _, _, ok := nativeEventOutcome(c, copy); ok {
			t.Fatalf("foreign failure accepted: %+v", copy)
		}
	}
}
