package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestUpgradeLatestUsesSameOwnerCardAcrossCheckingAndConfirm(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.upgradeRuntime.lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "/upgrade latest",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var initial, confirm *feishu.Operation
		for _, op := range gateway.snapshotOperations() {
			opCopy := op
			switch {
			case op.Kind == feishu.OperationSendCard && op.CardTitle == "正在检查升级":
				initial = &opCopy
			case op.Kind == feishu.OperationUpdateCard && op.CardTitle == "发现可升级版本":
				confirm = &opCopy
			}
		}
		if initial != nil && confirm != nil {
			if confirm.MessageID != initial.MessageID {
				t.Fatalf("expected confirm update to target same owner card, initial=%#v confirm=%#v", initial, confirm)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for owner-card update, got %#v", gateway.snapshotOperations())
}

func TestUpgradeLatestFromStampedCardUsesUpdateCard(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.upgradeRuntime.lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-1",
		Text:             "/upgrade latest",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var sawChecking, sawConfirm bool
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.MessageID != "om-card-1" {
				continue
			}
			if op.CardTitle == "正在检查升级" {
				sawChecking = true
			}
			if op.CardTitle == "发现可升级版本" {
				sawConfirm = true
			}
		}
		if sawChecking && sawConfirm {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for in-place owner-card updates, got %#v", gateway.snapshotOperations())
}

func TestUpgradeOwnerCancelConfirmClearsPendingAndSealsSameCard(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.upgradeRuntime.lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "/upgrade latest",
	})

	var confirmOp feishu.Operation
	var flowID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.CardTitle != "发现可升级版本" {
				continue
			}
			for _, button := range operationCardButtons(op) {
				value := cardButtonPayload(button)
				if len(value) == 0 || value["kind"] != "upgrade_owner_flow" || value["option_id"] != upgradeOwnerActionCancel {
					continue
				}
				flowID, _ = value["picker_id"].(string)
				confirmOp = op
				break
			}
		}
		if flowID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if flowID == "" {
		t.Fatalf("timed out waiting for confirm owner card, got %#v", gateway.snapshotOperations())
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        confirmOp.MessageID,
		PickerID:         flowID,
		OptionID:         upgradeOwnerActionCancel,
	})

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.CardTitle != "升级已取消" {
				continue
			}
			if op.MessageID != confirmOp.MessageID {
				t.Fatalf("expected cancel terminal to update same owner card, confirm=%#v cancel=%#v", confirmOp, op)
			}
			stateValue, err := install.LoadState(statePath)
			if err != nil {
				t.Fatalf("LoadState: %v", err)
			}
			if stateValue.PendingUpgrade != nil {
				t.Fatalf("expected pending upgrade to be cleared, got %#v", stateValue.PendingUpgrade)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for cancel terminal card, got %#v", gateway.snapshotOperations())
}

func TestUpgradeOwnerFlowBlocksOrdinaryTextInput(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	app.mu.Lock()
	app.newUpgradeOwnerFlowLocked("surface-1", "user-1", "om-card-1", upgradeOwnerFlowStageRunning)
	app.mu.Unlock()

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续处理一下",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			if op.CardTitle != "Upgrade" {
				continue
			}
			cardText := op.CardBody + "\n" + strings.Join(cardMarkdownContents(op.CardElements), "\n")
			if strings.Contains(cardText, "普通输入和其他操作已暂停") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for upgrade running gate notice, got %#v", gateway.snapshotOperations())
}
