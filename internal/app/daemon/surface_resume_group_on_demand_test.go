package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestFeishuGroupOnDemandTextStartsHeadlessAndDefersMessage(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})
	app := newRestoreHintTestApp(stateDir)
	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-1",
		Text:             "继续刚才的任务",
	})

	snapshot := app.service.SurfaceSnapshot("feishu:app-1:chat:oc_room")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID == "" {
		t.Fatalf("expected on-demand group text to start pending headless recovery, got %#v", snapshot)
	}
	if captured.InstanceID != snapshot.PendingHeadless.InstanceID || !testutil.SamePath(captured.WorkDir, workspaceDir) {
		t.Fatalf("unexpected on-demand headless launch options: captured=%#v pending=%#v", captured, snapshot.PendingHeadless)
	}
	if len(app.service.PendingRemoteTurns()) != 0 {
		t.Fatalf("expected original message to wait for recovery before dispatch, got %#v", app.service.PendingRemoteTurns())
	}
	if got := app.service.FeishuRoomActiveCount("feishu:app-1:chat:oc_room"); got != 1 {
		t.Fatalf("active room reservations during on-demand launch = %d, want 1", got)
	}
}

func TestFeishuGroupOnDemandLaunchFailureClearsContinuationAndRepliesOnce(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})
	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 0, errors.New("boom")
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-1",
		Text:             "继续刚才的任务",
	})

	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected launch failure to clear continuation, got %#v", continuation)
	}
	if got := app.service.FeishuRoomActiveCount("feishu:app-1:chat:oc_room"); got != 0 {
		t.Fatalf("active room reservations after launch failure = %d, want 0", got)
	}
	gateway := app.gateway.(*recordingGateway)
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one on-demand failure notice, got %#v", gateway.operations)
	}
	if gateway.operations[0].CardTitle != "恢复失败" {
		t.Fatalf("expected restore failure notice, got %#v", gateway.operations[0])
	}
}

func TestFeishuGroupOnDemandTerminalFailureDoesNotRepeatAcrossMessages(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})
	app := newRestoreHintTestApp(stateDir)
	app.headlessRuntime.CodexRealBinary = "codex"
	app.runCodexCapabilityPreflight = func(context.Context, codexprofile.CapabilityPreflightOptions) (codexprofile.CapabilityPreflightObservation, error) {
		return codexprofile.CapabilityPreflightObservation{}, &codexprofile.OAuthProbeError{
			Code:  codexprofile.ErrorCodexCapabilityUnsupported,
			Stage: "capability_initialize",
		}
	}
	app.ensureCodexRuntimeCapability(context.Background())
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatal("capability failure must not reach launcher")
		return 0, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-1",
		Text:             "继续刚才的任务",
	})
	gateway := app.gateway.(*recordingGateway)
	if len(gateway.operations) != 1 {
		t.Fatalf("expected first failure notice, got %#v", gateway.operations)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-2",
		Text:             "继续刚才的任务",
	})
	if len(gateway.operations) != 1 {
		t.Fatalf("expected terminal failure notice to be suppressed across messages, got %#v", gateway.operations)
	}
}

func TestFeishuGroupOnDemandTerminalFailureRepeatsAfterTargetChange(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	entry := surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	}
	putSurfaceResumeStateForTest(t, stateDir, entry)
	app := newRestoreHintTestApp(stateDir)
	app.headlessRuntime.CodexRealBinary = "codex"
	app.runCodexCapabilityPreflight = func(context.Context, codexprofile.CapabilityPreflightOptions) (codexprofile.CapabilityPreflightObservation, error) {
		return codexprofile.CapabilityPreflightObservation{}, &codexprofile.OAuthProbeError{
			Code:  codexprofile.ErrorCodexCapabilityUnsupported,
			Stage: "capability_initialize",
		}
	}
	app.ensureCodexRuntimeCapability(context.Background())
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatal("capability failure must not reach launcher")
		return 0, nil
	}

	sendText := func(messageID string) {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionTextMessage,
			GatewayID:        "app-1",
			SurfaceSessionID: "feishu:app-1:chat:oc_room",
			ChatID:           "oc_room",
			ActorUserID:      "ou_user",
			MessageID:        messageID,
			Text:             "继续刚才的任务",
		})
	}
	sendText("om-on-demand-1")
	gateway := app.gateway.(*recordingGateway)
	if len(gateway.operations) != 1 {
		t.Fatalf("expected first failure notice, got %#v", gateway.operations)
	}

	entry.ResumeThreadID = "thread-2"
	entry.ResumeThreadCWD = filepath.Join(workspaceDir, "other")
	if !app.putSurfaceResumeEntryLocked(entry, time.Now().UTC()) {
		t.Fatal("expected target change to persist a new resume entry")
	}
	app.syncSurfaceResumeRecoveryStateLocked()

	sendText("om-on-demand-2")
	if len(gateway.operations) != 2 {
		t.Fatalf("expected target change to re-enable failure notice, got %#v", gateway.operations)
	}
}

func TestGroupOnDemandTerminalFailureRecordAndClear(t *testing.T) {
	app := newRestoreHintTestApp(t.TempDir())
	surfaceID := "feishu:app-1:chat:oc_room"

	if code, emit := app.recordGroupOnDemandTerminalFailureLocked(surfaceID, "codex_capability_unsupported"); !emit || code != "codex_capability_unsupported" {
		t.Fatalf("first terminal failure emit=%t code=%q, want emit with code", emit, code)
	}
	if _, emit := app.recordGroupOnDemandTerminalFailureLocked(surfaceID, "codex_capability_unsupported"); emit {
		t.Fatal("same terminal failure must be suppressed")
	}
	if _, emit := app.recordGroupOnDemandTerminalFailureLocked(surfaceID, "thread_not_found"); !emit {
		t.Fatal("non-terminal failure must always emit")
	}
	if _, emit := app.recordGroupOnDemandTerminalFailureLocked(surfaceID, "workspace_not_found"); !emit {
		t.Fatal("different terminal failure must emit again")
	}

	app.clearGroupOnDemandTerminalFailureLocked(surfaceID)
	if _, emit := app.recordGroupOnDemandTerminalFailureLocked(surfaceID, "codex_capability_unsupported"); !emit {
		t.Fatal("clear must allow the same terminal failure to emit again")
	}
}

func TestFeishuGroupOnDemandTimeoutClearsContinuationAndRepliesOnce(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})
	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-1",
		Text:             "继续刚才的任务",
	})
	pending := app.service.SurfaceSnapshot("feishu:app-1:chat:oc_room").PendingHeadless
	if pending.InstanceID == "" {
		t.Fatalf("expected pending headless before timeout")
	}
	gateway := app.gateway.(*recordingGateway)
	gateway.operations = nil

	app.onTick(context.Background(), pending.ExpiresAt.Add(time.Second))

	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected timeout to clear continuation, got %#v", continuation)
	}
	if got := app.service.FeishuRoomActiveCount("feishu:app-1:chat:oc_room"); got != 0 {
		t.Fatalf("active room reservations after timeout = %d, want 0", got)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one timeout notice, got %#v", gateway.operations)
	}
	if gateway.operations[0].CardTitle != "恢复失败" {
		t.Fatalf("expected restore timeout notice, got %#v", gateway.operations[0])
	}
}

func TestFeishuGroupOnDemandImmediateFailureRepliesOnce(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		ProductMode:      "normal",
		Backend:          "codex",
		ResumeThreadID:   "thread-missing",
		ResumeHeadless:   true,
	})
	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatal("missing target must fail before headless launch")
		return 0, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-missing-1",
		Text:             "继续刚才的任务",
	})

	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected immediate failure to avoid continuation, got %#v", continuation)
	}
	gateway := app.gateway.(*recordingGateway)
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one immediate failure notice, got %#v", gateway.operations)
	}
	if gateway.operations[0].CardTitle != "恢复失败" {
		t.Fatalf("expected restore failure notice, got %#v", gateway.operations[0])
	}
}

func TestFeishuGroupOnDemandReplayDispatchesOriginalTextAfterHeadlessConnect(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})
	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-1",
		Text:             "继续刚才的任务",
	})
	pending := app.service.SurfaceSnapshot("feishu:app-1:chat:oc_room").PendingHeadless
	if pending.InstanceID == "" {
		t.Fatalf("expected pending headless before replay")
	}
	if got := app.service.FeishuRoomActiveCount("feishu:app-1:chat:oc_room"); got != 1 {
		t.Fatalf("active room reservations before replay = %d, want 1", got)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    pending.InstanceID,
			DisplayName:   "headless",
			WorkspaceRoot: workspaceDir,
			WorkspaceKey:  workspaceDir,
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	prompts := make([]agentproto.Command, 0)
	for _, command := range sent {
		if command.Kind == agentproto.CommandPromptSend {
			prompts = append(prompts, command)
		}
	}
	if len(prompts) != 1 {
		t.Fatalf("expected replay to dispatch original text once, got commands=%#v prompts=%#v", sent, prompts)
	}
	if prompts[0].Origin.MessageID != "om-on-demand-1" {
		t.Fatalf("expected replayed command to keep original message id, got %#v", prompts[0].Origin)
	}
	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected replay to clear continuation, got %#v", continuation)
	}
	if got := app.service.FeishuRoomActiveCount("feishu:app-1:chat:oc_room"); got != 1 {
		t.Fatalf("active room reservations after replayed prompt = %d, want 1", got)
	}
}

func TestFeishuGroupOnDemandReplayRechecksUpgradeOwnerGate(t *testing.T) {
	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putGroupOnDemandResumeStateForTest(t, stateDir, workspaceDir)
	app := newRestoreHintTestApp(stateDir)
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	pendingInstanceID := startGroupOnDemandPendingForTest(t, app, workspaceDir)
	app.mu.Lock()
	app.newUpgradeOwnerFlowLocked("feishu:app-1:chat:oc_room", "ou_user", "om-upgrade-card", upgraderuntime.OwnerFlowStageRunning)
	app.mu.Unlock()
	gateway := app.gateway.(*recordingGateway)
	gateway.operations = nil

	connectGroupOnDemandPendingForTest(app, pendingInstanceID, workspaceDir)

	if prompts := promptCommands(sent); len(prompts) != 0 {
		t.Fatalf("expected upgrade owner gate to block replayed prompt dispatch, got %#v", prompts)
	}
	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected blocked replay to clear continuation, got %#v", continuation)
	}
	if !gatewayOperationsContainText(gateway.operations, "普通输入和其他操作已暂停") {
		t.Fatalf("expected upgrade running notice after replay, got %#v", gateway.operations)
	}
}

func TestFeishuGroupOnDemandReplayRechecksTurnPatchTransactionGate(t *testing.T) {
	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putGroupOnDemandResumeStateForTest(t, stateDir, workspaceDir)
	app := newRestoreHintTestApp(stateDir)
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	pendingInstanceID := startGroupOnDemandPendingForTest(t, app, workspaceDir)
	app.turnPatchRuntime.ActiveTx[pendingInstanceID] = &turnpatchruntime.Transaction{
		ID:               "tx-1",
		InstanceID:       pendingInstanceID,
		Kind:             turnpatchruntime.TransactionKindApply,
		InitiatorSurface: "feishu:app-1:chat:oc_room",
		PausedSurfaceIDs: map[string]bool{"feishu:app-1:chat:oc_room": true},
		Stage:            turnpatchruntime.TransactionStageApplyingRestart,
		StartedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	gateway := app.gateway.(*recordingGateway)
	gateway.operations = nil

	connectGroupOnDemandPendingForTest(app, pendingInstanceID, workspaceDir)

	if prompts := promptCommands(sent); len(prompts) != 0 {
		t.Fatalf("expected turn patch gate to block replayed prompt dispatch, got %#v", prompts)
	}
	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected blocked replay to clear continuation, got %#v", continuation)
	}
	if !gatewayOperationsContainText(gateway.operations, "当前正在修补当前会话") {
		t.Fatalf("expected turn patch running notice after replay, got %#v", gateway.operations)
	}
}

func TestFeishuGroupOnDemandTextWithFilesStartsHeadlessAndDefersMessage(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeHeadless:     true,
	})
	app := newRestoreHintTestApp(stateDir)
	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-file-1",
		Text:             "看这个文件",
		Files: []control.ActionFileAttachment{{
			SourceMessageID: "om-file-1",
			LocalPath:       "/tmp/file.txt",
			FileName:        "file.txt",
		}},
	})

	snapshot := app.service.SurfaceSnapshot("feishu:app-1:chat:oc_room")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID == "" {
		t.Fatalf("expected text with files to start pending headless recovery, got %#v", snapshot)
	}
	if captured.InstanceID != snapshot.PendingHeadless.InstanceID || !testutil.SamePath(captured.WorkDir, workspaceDir) {
		t.Fatalf("unexpected on-demand headless launch options: captured=%#v pending=%#v", captured, snapshot.PendingHeadless)
	}
	continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]
	if continuation == nil || len(continuation.Action.Files) != 1 || continuation.Action.Files[0].FileName != "file.txt" {
		t.Fatalf("expected continuation to preserve text file attachments, got %#v", continuation)
	}
	gateway := app.gateway.(*recordingGateway)
	if len(gateway.operations) != 0 {
		t.Fatalf("text with files should wait for recovery without unsupported prompt, got %#v", gateway.operations)
	}
}

func TestFeishuGroupOnDemandVSCodeReturnsUnsupportedPrompt(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		ProductMode:      "vscode",
		Backend:          "vscode",
		ResumeInstanceID: "inst-vscode-1",
	})
	app := newRestoreHintTestApp(stateDir)
	startHeadlessCalls := 0
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		startHeadlessCalls++
		return 0, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-vscode-1",
		Text:             "继续刚才的任务",
	})

	if continuation := app.surfaceResumeRuntime.groupOnDemandContinuations["feishu:app-1:chat:oc_room"]; continuation != nil {
		t.Fatalf("expected no VS Code continuation, got %#v", continuation)
	}
	if startHeadlessCalls != 0 {
		t.Fatalf("VS Code group on-demand recovery must not start headless, got %d calls", startHeadlessCalls)
	}
	gateway := app.gateway.(*recordingGateway)
	if len(gateway.operations) != 1 || gateway.operations[0].CardTitle != "当前群暂不能自动恢复 VS Code" {
		t.Fatalf("expected one VS Code unsupported prompt, got %#v", gateway.operations)
	}
}

func putGroupOnDemandResumeStateForTest(t *testing.T, stateDir, workspaceDir string) {
	t.Helper()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_user",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})
}

func startGroupOnDemandPendingForTest(t *testing.T, app *App, workspaceDir string) string {
	t.Helper()
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-on-demand-1",
		Text:             "继续刚才的任务",
	})
	pending := app.service.SurfaceSnapshot("feishu:app-1:chat:oc_room").PendingHeadless
	if pending.InstanceID == "" || !testutil.SamePath(pending.ThreadCWD, workspaceDir) {
		t.Fatalf("expected pending headless before replay, got %#v", pending)
	}
	return pending.InstanceID
}

func connectGroupOnDemandPendingForTest(app *App, instanceID, workspaceDir string) {
	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    instanceID,
			DisplayName:   "headless",
			WorkspaceRoot: workspaceDir,
			WorkspaceKey:  workspaceDir,
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})
}

func promptCommands(commands []agentproto.Command) []agentproto.Command {
	prompts := make([]agentproto.Command, 0)
	for _, command := range commands {
		if command.Kind == agentproto.CommandPromptSend {
			prompts = append(prompts, command)
		}
	}
	return prompts
}

func gatewayOperationsContainText(operations []feishu.Operation, want string) bool {
	for _, operation := range operations {
		if strings.Contains(operation.CardBody, want) || strings.Contains(operationCardText(operation), want) {
			return true
		}
	}
	return false
}
