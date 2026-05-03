package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestUpgradeLatestUsesSameOwnerCardAcrossCheckingAndConfirm(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:main:chat:1",
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
	app.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	applyGatewayActionForTest(context.Background(), app, control.Action{
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
	app.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:main:chat:1",
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
		SurfaceSessionID: "feishu:main:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        confirmOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionCancel,
		},
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
	app.newUpgradeOwnerFlowLocked("surface-1", "user-1", "om-card-1", upgraderuntime.OwnerFlowStageRunning)
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

func TestUpgradeOwnerConfirmEventUsesBodyAndNoticeSections(t *testing.T) {
	flow := &upgraderuntime.OwnerCardFlowRecord{
		FlowID:         "upgrade-owner-1",
		CurrentVersion: "v1.0.0",
		TargetVersion:  "v1.1.0",
		Track:          install.ReleaseTrackAlpha,
	}

	event := upgradeOwnerConfirmEvent("surface-1", flow, install.InstallState{
		CurrentTrack:   install.ReleaseTrackAlpha,
		CurrentVersion: "v1.0.0",
	})
	page := catalogFromUIEvent(t, event)
	if page.Sealed || !page.Interactive {
		t.Fatalf("expected confirm page to stay interactive, got %#v", page)
	}
	if len(page.BodySections) < 3 {
		t.Fatalf("expected confirm page to preserve upgrade context in body sections, got %#v", page.BodySections)
	}
	if len(page.NoticeSections) != 1 || !strings.Contains(strings.Join(page.NoticeSections[0].Lines, "\n"), "确认后会开始下载并准备升级") {
		t.Fatalf("expected confirm page to place action warning in notice area, got %#v", page.NoticeSections)
	}
}

func TestUpgradeOwnerTerminalEventSealsPageContract(t *testing.T) {
	flow := &upgraderuntime.OwnerCardFlowRecord{
		FlowID:         "upgrade-owner-2",
		CurrentVersion: "v1.0.0",
		TargetVersion:  "v1.1.0",
		Track:          install.ReleaseTrackBeta,
	}

	event := upgradeOwnerTerminalEvent(
		"surface-1",
		flow,
		"升级失败",
		"error",
		upgradeOwnerContextSections(flow.CurrentVersion, flow.TargetVersion, string(flow.Track)),
		upgradeOwnerNoticeSections("下载失败，请稍后重试。"),
	)
	page := catalogFromUIEvent(t, event)
	if !page.Sealed || page.Interactive || len(page.RelatedButtons) != 0 {
		t.Fatalf("expected terminal upgrade page to be sealed without footer actions, got %#v", page)
	}
	if len(page.BodySections) == 0 || len(page.NoticeSections) != 1 {
		t.Fatalf("expected terminal upgrade page to keep context plus notice, got %#v", page)
	}
}
