package daemon

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateSDKTextClosesUnattachedPicker(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	n := len(f.gateway.snapshotOperations())
	f.user.send(f.app, "cold-picker-hi", "hi")
	f.user.waitVisible(f.app, n, "/list")
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "open-picker", "/list")
	var card feishu.Operation
	var doc any
	for card.MessageID == "" {
		for _, op := range f.gateway.snapshotOperations()[n:] {
			body, err := feishu.FreezePrivateOperation(op, f.app.daemonLifecycleID)
			if err == nil && json.Unmarshal(body, &doc) == nil && findPrivateCallback(doc, "target_picker_confirm") != nil {
				card = op
				break
			}
		}
		if card.MessageID != "" {
			break
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("picker not visible")
		case <-time.After(20 * time.Millisecond):
		}
	}
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "text-during-picker", "unattached-picker-text")
	f.user.waitVisible(f.app, n, "/list")
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now(), EventID: "stale-picker-confirm", Values: findPrivateCallback(doc, "target_picker_confirm")}
	_, err := f.app.applications.hook.ReceiveInteractionResponse(f.ctx, click)
	if err != nil {
		t.Fatal(err)
	}
	f.user.chooseNew(f.app, "after-picker-text")
	n = len(f.gateway.snapshotOperations())
	f.user.send(f.app, "after-picker", "fresh-after-picker")
	f.user.waitFinal(f.app, n, "after-picker", "continuity-final")
	data, err := os.ReadFile(f.trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "unattached-picker-text") || !strings.Contains(string(data), "fresh-after-picker") {
		t.Fatal("picker text was replayed or fresh input missing")
	}
}
