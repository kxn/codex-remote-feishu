package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// Missing history storage stands in for a failed team inventory read. The
// private owner can still use its existing state; history itself fails closed.
func TestPrivateHistoryUnavailableDoesNotBlockOtherSDKActions(t *testing.T) {
	a, gateway := newApplicationRuntimeTestApp(t)
	r := a.applications
	r.native.teamHistory = nil
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now()}
	if err := r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	send := func(id, text string) {
		t.Helper()
		raw.EventID = id
		raw.MessageID = id
		b, _ := json.Marshal(map[string]string{"text": text})
		raw.RawContent = string(b)
		if err := r.hook.Receive(t.Context(), raw); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(after int, want string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			for _, op := range gateway.snapshotOperations()[after:] {
				body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
				if strings.Contains(string(body), want) {
					return
				}
			}
		}
		t.Fatalf("missing %q", want)
	}
	send("status-before", "/status")
	wait(0, "状态")
	before := len(gateway.snapshotOperations())
	send("history", "/list")
	wait(before, "历史目录暂不可用")
	before = len(gateway.snapshotOperations())
	send("status-after", "/status")
	wait(before, "状态")
	o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
	for _, v := range []struct {
		kind control.ActionKind
		want bool
	}{{control.ActionTextMessage, false}, {control.ActionTargetPickerSelectSession, false}, {control.ActionTargetPickerPage, false}, {control.ActionTargetPickerConfirm, true}, {control.ActionListInstances, true}} {
		encoded, _ := json.Marshal(control.Action{Kind: v.kind, SurfaceSessionID: o.SurfaceID, Text: "hello"})
		got, err := shouldPreparePrivateHistory(t.Context(), r.processor, o, 1, encoded)
		if err != nil || got != v.want {
			t.Fatalf("action %s prepare=%v want=%v err=%v", v.kind, got, v.want, err)
		}
	}
}
