package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestAsyncUserInputCallbackRequiresCurrentDaemon(t *testing.T) {
	startedAt := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})
	for _, lifecycle := range []string{"", "old-daemon", app.daemonLifecycleID} {
		action := app.classifyInboundAction(control.Action{
			Kind:    control.ActionAsyncUserInput,
			Inbound: &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: lifecycle},
		})
		want := control.InboundLifecycleOldCard
		if lifecycle == app.daemonLifecycleID {
			want = control.InboundLifecycleCurrent
		}
		if action.Inbound.LifecycleVerdict != want {
			t.Fatalf("lifecycle=%q verdict=%q want=%q", lifecycle, action.Inbound.LifecycleVerdict, want)
		}
	}
}
