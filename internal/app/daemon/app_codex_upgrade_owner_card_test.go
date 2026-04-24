package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexUpgradeOwnerFlowOpensWithoutAutoCheck(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	lookupCalls := 0
	app.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return stubStandaloneCodexInstallation("0.123.0"), nil
	}
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		lookupCalls++
		return "0.124.0", nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade codex",
	})

	openOp := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.CardTitle == "Codex 升级"
	})
	if lookupCalls != 0 {
		t.Fatalf("expected opening owner card not to auto-check latest version, got %d lookups", lookupCalls)
	}
	if !operationHasActionValue(openOp, upgradeOwnerPayloadKind, upgradeOwnerPayloadOptionKey, upgradeOwnerActionCheck) {
		t.Fatalf("expected initial Codex upgrade card to expose check action, got %#v", openOp.CardElements)
	}
}

func TestCodexUpgradeOwnerFlowCheckIsRepeatable(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	lookupCalls := 0
	app.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return stubStandaloneCodexInstallation("0.123.0"), nil
	}
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		lookupCalls++
		return "0.124.0", nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade codex",
	})
	openOp := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.CardTitle == "Codex 升级"
	})
	flowID := codexUpgradeOwnerFlowIDFromOperation(t, openOp, upgradeOwnerActionCheck)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        openOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionCheck,
		},
	})
	firstReady := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.Kind == feishu.OperationUpdateCard && op.MessageID == openOp.MessageID && op.CardTitle == "发现可升级版本"
	})
	if lookupCalls != 1 {
		t.Fatalf("expected first check to perform exactly one lookup, got %d", lookupCalls)
	}
	if !operationHasActionValue(firstReady, upgradeOwnerPayloadKind, upgradeOwnerPayloadOptionKey, upgradeOwnerActionConfirm) {
		t.Fatalf("expected ready card to expose confirm button, got %#v", firstReady.CardElements)
	}
	if !operationHasActionValue(firstReady, upgradeOwnerPayloadKind, upgradeOwnerPayloadOptionKey, upgradeOwnerActionCheck) {
		t.Fatalf("expected ready card to stay re-checkable, got %#v", firstReady.CardElements)
	}

	before := len(gateway.snapshotOperations())
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        openOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionCheck,
		},
	})
	secondReady := waitForRepeatedCodexReadyCard(t, gateway, openOp.MessageID, before, func() bool {
		return lookupCalls >= 2
	})
	if lookupCalls != 2 {
		t.Fatalf("expected repeat check to perform a second lookup, got %d", lookupCalls)
	}
	if !operationHasActionValue(secondReady, upgradeOwnerPayloadKind, upgradeOwnerPayloadOptionKey, upgradeOwnerActionCheck) {
		t.Fatalf("expected repeated check to return to the same stable card, got %#v", secondReady.CardElements)
	}
}

func TestCodexUpgradeOwnerConfirmFailureReturnsToStableCheckedState(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return stubStandaloneCodexInstallation("0.123.0"), nil
	}
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade codex",
	})
	openOp := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.CardTitle == "Codex 升级"
	})
	flowID := codexUpgradeOwnerFlowIDFromOperation(t, openOp, upgradeOwnerActionCheck)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        openOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionCheck,
		},
	})
	waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.Kind == feishu.OperationUpdateCard && op.MessageID == openOp.MessageID && op.CardTitle == "发现可升级版本"
	})

	app.service.Instance("inst-1").ActiveTurnID = "turn-1"
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        openOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionConfirm,
		},
	})

	blocked := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.Kind == feishu.OperationUpdateCard && op.MessageID == openOp.MessageID && op.CardTitle == "发现新版本，但暂时不能升级"
	})
	cardText := blocked.CardBody + "\n" + strings.Join(cardMarkdownContents(blocked.CardElements), "\n")
	if !strings.Contains(cardText, "确认升级失败") {
		t.Fatalf("expected blocked state to explain confirm-time revalidation failure, got %#v", blocked)
	}
	if operationHasActionValue(blocked, upgradeOwnerPayloadKind, upgradeOwnerPayloadOptionKey, upgradeOwnerActionConfirm) {
		t.Fatalf("expected blocked state to drop confirm button, got %#v", blocked.CardElements)
	}
	if !operationHasActionValue(blocked, upgradeOwnerPayloadKind, upgradeOwnerPayloadOptionKey, upgradeOwnerActionCheck) {
		t.Fatalf("expected blocked state to remain re-checkable, got %#v", blocked.CardElements)
	}
	if app.codexUpgradeRuntime.Active != nil {
		t.Fatalf("expected confirm-time revalidation failure not to start upgrade, got %#v", app.codexUpgradeRuntime.Active)
	}
}

func TestCodexUpgradeOwnerFlowRejectsOldCardAfterReopen(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return stubStandaloneCodexInstallation("0.123.0"), nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade codex",
	})
	first := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.CardTitle == "Codex 升级"
	})
	firstFlowID := codexUpgradeOwnerFlowIDFromOperation(t, first, upgradeOwnerActionCheck)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade codex",
	})
	second := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.CardTitle == "Codex 升级" && op.MessageID != first.MessageID
	})
	if second.MessageID == first.MessageID {
		t.Fatalf("expected reopen to create a fresh owner card, first=%#v second=%#v", first, second)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        first.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   firstFlowID,
			OptionID: upgradeOwnerActionCheck,
		},
	})

	waitForUpgradeNoticeBody(t, gateway, "这张 Codex 升级卡片已失效")
}

func TestCodexUpgradeOwnerFlowTerminalStaysOnInitiatorSurface(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	currentVersion := "0.123.0"
	app.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return stubStandaloneCodexInstallation(currentVersion), nil
	}
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	installStarted := make(chan struct{})
	releaseInstall := make(chan struct{})
	app.codexUpgradeRuntime.Install = func(_ context.Context, _ codexupgrade.Installation, version string) error {
		close(installStarted)
		<-releaseInstall
		currentVersion = version
		return nil
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	attachStandaloneCodexTestSurface(app, "surface-init", "chat-init", "user-init", "inst-1")
	attachStandaloneCodexTestSurface(app, "surface-other", "chat-other", "user-other", "inst-1")
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "surface-init",
		ChatID:           "chat-init",
		ActorUserID:      "user-init",
		Text:             "/upgrade codex",
	})
	openOp := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.SurfaceSessionID == "surface-init" && op.CardTitle == "Codex 升级"
	})
	flowID := codexUpgradeOwnerFlowIDFromOperation(t, openOp, upgradeOwnerActionCheck)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-init",
		ChatID:           "chat-init",
		ActorUserID:      "user-init",
		MessageID:        openOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionCheck,
		},
	})
	waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.SurfaceSessionID == "surface-init" && op.Kind == feishu.OperationUpdateCard && op.MessageID == openOp.MessageID && op.CardTitle == "发现可升级版本"
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeOwnerFlow,
		SurfaceSessionID: "surface-init",
		ChatID:           "chat-init",
		ActorUserID:      "user-init",
		MessageID:        openOp.MessageID,
		OwnerFlow: &control.ActionOwnerCardFlow{
			FlowID:   flowID,
			OptionID: upgradeOwnerActionConfirm,
		},
	})
	waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.SurfaceSessionID == "surface-init" && op.Kind == feishu.OperationUpdateCard && op.MessageID == openOp.MessageID && op.CardTitle == "正在升级 Codex"
	})
	select {
	case <-installStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for codex owner flow install to start")
	}

	close(releaseInstall)
	success := waitForCodexUpgradeOperation(t, gateway, func(op feishu.Operation) bool {
		return op.SurfaceSessionID == "surface-init" && op.Kind == feishu.OperationUpdateCard && op.MessageID == openOp.MessageID && op.CardTitle == "Codex 升级完成"
	})
	if success.SurfaceSessionID != "surface-init" {
		t.Fatalf("expected terminal card to stay on initiator surface, got %#v", success)
	}
	for _, op := range gateway.snapshotOperations() {
		if op.SurfaceSessionID == "surface-other" && (op.CardTitle == "正在升级 Codex" || op.CardTitle == "Codex 升级完成") {
			t.Fatalf("expected non-initiator surface to stay silent about terminal owner card, got %#v", op)
		}
	}
	if len(sent) == 0 || sent[0].Kind != agentproto.CommandProcessChildRestart {
		t.Fatalf("expected successful owner flow to restart child codex, got %#v", sent)
	}
}

func waitForCodexUpgradeOperation(t *testing.T, gateway *lifecycleGateway, predicate func(feishu.Operation) bool) feishu.Operation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			if predicate(op) {
				return op
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for codex upgrade operation")
	return feishu.Operation{}
}

func codexUpgradeOwnerFlowIDFromOperation(t *testing.T, operation feishu.Operation, optionID string) string {
	t.Helper()
	for _, button := range operationCardButtons(operation) {
		value := cardButtonPayload(button)
		if len(value) == 0 || value["kind"] != upgradeOwnerPayloadKind || value[upgradeOwnerPayloadOptionKey] != optionID {
			continue
		}
		flowID, _ := value[upgradeOwnerPayloadFlowKey].(string)
		if strings.TrimSpace(flowID) != "" {
			return strings.TrimSpace(flowID)
		}
	}
	t.Fatalf("expected operation %#v to carry %s owner-flow button", operation, optionID)
	return ""
}

func waitForRepeatedCodexReadyCard(t *testing.T, gateway *lifecycleGateway, messageID string, before int, ready func() bool) feishu.Operation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !ready() {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var latest feishu.Operation
		count := 0
		ops := gateway.snapshotOperations()
		for idx, op := range ops {
			if idx < before {
				continue
			}
			if op.Kind != feishu.OperationUpdateCard || op.MessageID != messageID || op.CardTitle != "发现可升级版本" {
				continue
			}
			latest = op
			count++
		}
		if count != 0 {
			return latest
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for repeated Codex ready card")
	return feishu.Operation{}
}

func stubStandaloneCodexInstallation(version string) codexupgrade.Installation {
	return codexupgrade.Installation{
		ConfiguredBinary: "codex",
		EffectiveBinary:  "codex",
		ResolvedBinary:   "/usr/local/bin/codex",
		SourceKind:       codexupgrade.SourceStandaloneNPM,
		PackageVersion:   strings.TrimSpace(version),
		NPMCommand:       "npm",
		PackageName:      "@openai/codex",
		PackageRoot:      "/tmp/npm/@openai/codex",
		PackageBinPath:   "/tmp/npm/@openai/codex/bin/codex.js",
	}
}
