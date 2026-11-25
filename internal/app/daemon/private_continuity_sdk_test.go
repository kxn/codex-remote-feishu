package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateSDKBusyNewThenStopThenNew(t *testing.T) {
	f := newContinuityFixture(t)
	u := f.user
	u.send(f.app, "busy", "busy-turn")
	// A person can inspect status; never infer that the turn is running from
	// an internal checkpoint or from a local process marker.
	for attempt := 0; ; attempt++ {
		before := len(f.gateway.snapshotOperations())
		u.send(f.app, fmt.Sprintf("busy-status-%d", attempt), "/status")
		observed, running := false, false
		for !observed {
			for _, op := range f.gateway.snapshotOperations()[before:] {
				if op.CardTitle != "当前状态" {
					continue
				}
				body, _ := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
				observed, running = true, strings.Contains(string(body), "当前 1 条执行中")
			}
			if !observed {
				select {
				case <-f.ctx.Done():
					t.Fatal("busy status did not appear")
				case <-time.After(20 * time.Millisecond):
				}
			}
		}
		if running {
			break
		}
		if attempt >= 20 {
			t.Fatal("busy status never showed execution")
		}
	}
	n := len(f.gateway.snapshotOperations())
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "new-while-busy", "/new")
	u.waitVisible(f.app, n, "正在执行")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "stop-busy", "/stop")
	u.waitVisible(f.app, n, "停止")
	// The busy reaction disappears only after the native terminal projection;
	// the preceding stop acknowledgement alone is not terminal evidence.
	for {
		stopped := false
		for _, op := range f.gateway.snapshotOperations()[n:] {
			if op.Kind == feishu.OperationRemoveReaction && op.MessageID == "busy" && op.EmojiType == "THINKING" {
				stopped = true
			}
		}
		if stopped {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("native stop did not clear visible busy reaction")
		case <-time.After(20 * time.Millisecond):
		}
	}
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "new-after-stop", "/new")
	u.waitVisible(f.app, n, "下一条文本")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "new-after-stop-hi", "hi")
	u.waitFinal(f.app, n, "new-after-stop-hi", "continuity-final")
}
func findDeclineRequest(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "request_respond" && x["request_option_id"] == "decline" && x["private_view"] != nil {
			return x
		}
		for _, v := range x {
			if a := findDeclineRequest(v); a != nil {
				return a
			}
		}
	case []any:
		for _, v := range x {
			if a := findDeclineRequest(v); a != nil {
				return a
			}
		}
	}
	return nil
}
func TestPrivateSDKDeclineApprovalThenNew(t *testing.T) {
	f := newContinuityFixture(t)
	u := f.user
	before := len(f.gateway.snapshotOperations())
	u.send(f.app, "ask", "ask-approval")
	var card feishu.Operation
	var choice map[string]any
	for choice == nil {
		for _, op := range f.gateway.snapshotOperations()[before:] {
			if op.MessageID == "" {
				continue
			}
			body, e := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
			if e != nil {
				continue
			}
			var doc any
			if json.Unmarshal(body, &doc) == nil {
				if v := findDeclineRequest(doc); v != nil {
					card, choice = op, v
					break
				}
			}
		}
		if choice != nil {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("decline approval card not visible")
		case <-time.After(20 * time.Millisecond):
		}
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "decline", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: choice, ReceivedAt: time.Now()}
	n := len(f.gateway.snapshotOperations())
	if _, err := f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click); err != nil {
		t.Fatal(err)
	}
	u.waitVisible(f.app, n, "approval ended")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "new-after-decline", "/new")
	u.waitVisible(f.app, n, "下一条文本")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "hi-after-decline", "hi")
	u.waitFinal(f.app, n, "hi-after-decline", "continuity-final")
	data, e := os.ReadFile(f.trace)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), "ask-approval") {
		t.Fatal("approval input did not reach native")
	}
	if !strings.Contains(string(data), `"decision":"decline"`) {
		t.Fatalf("native did not receive declined approval: %s", data)
	}
}
