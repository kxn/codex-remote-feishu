package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestDaemonTickRunsVSCodeCompatibilityDetectInBackgroundAndAvoidsDuplicateLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	putSurfaceResumeStateForTest(t, filepath.Join(home, ".local", "state", "codex-remote"), SurfaceResumeEntry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
	})

	gateway := newLifecycleGateway()
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, false)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	detectCalls := 0
	app.vscodeDetect = func() (vscodeDetectResponse, error) {
		detectCalls++
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return vscodeDetectResponse{
			CurrentMode:            "editor_settings",
			LatestBundleEntrypoint: "/tmp/fake-entrypoint",
		}, nil
	}

	base := time.Now().UTC()
	app.onTick(context.Background(), base)
	waitForTestSignal(t, started, "vscode compatibility detect start")

	app.onTick(context.Background(), base.Add(time.Second))
	if detectCalls != 1 {
		t.Fatalf("detect calls while refresh is in flight = %d, want 1", detectCalls)
	}
	if snapshot := app.service.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected vscode surface to remain detached while detect is pending, got %#v", snapshot)
	}

	close(release)
	card := waitForLifecycleOperationTitle(t, gateway, "VS Code 接入需要迁移")
	if !operationHasCallbackButton(card, "迁移并重新接入", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected migration button after async detect, got %#v", card.CardElements)
	}
}

func TestDaemonTickRetriesVSCodeCompatibilityDetectAfterBackoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	putSurfaceResumeStateForTest(t, filepath.Join(home, ".local", "state", "codex-remote"), SurfaceResumeEntry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
	})

	gateway := newLifecycleGateway()
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, false)
	detectCalls := 0
	app.vscodeDetect = func() (vscodeDetectResponse, error) {
		detectCalls++
		if detectCalls == 1 {
			return vscodeDetectResponse{}, errors.New("boom")
		}
		return vscodeDetectResponse{
			CurrentMode:            "editor_settings",
			LatestBundleEntrypoint: "/tmp/fake-entrypoint",
		}, nil
	}

	base := time.Now().UTC()
	app.onTick(context.Background(), base)
	waitForDaemonCondition(t, 2*time.Second, func() bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return detectCalls == 1 && !app.vscodeCompatibility.RefreshInFlight && app.vscodeCompatibility.NextRetryAt.Equal(base.Add(vscodeCompatibilityRetryBackoff))
	})

	app.onTick(context.Background(), base.Add(5*time.Second))
	if detectCalls != 1 {
		t.Fatalf("detect calls before backoff expiry = %d, want 1", detectCalls)
	}

	app.onTick(context.Background(), base.Add(vscodeCompatibilityRetryBackoff+time.Second))
	waitForDaemonCondition(t, 2*time.Second, func() bool { return detectCalls == 2 })
	card := waitForLifecycleOperationTitle(t, gateway, "VS Code 接入需要迁移")
	if !operationHasCallbackButton(card, "迁移并重新接入", vscodeMigrationOwnerPayloadKind, vscodeMigrationOwnerActionRun) {
		t.Fatalf("expected migration button after retry succeeds, got %#v", card.CardElements)
	}
}

func TestDaemonTickVSCodeFollowupGuidancePatchesAsyncCompatibilityCard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	binaryPath := filepath.Join(home, "bin", "codex-remote")
	writeExecutableFile(t, binaryPath, "wrapper-binary")

	putSurfaceResumeStateForTest(t, filepath.Join(home, ".local", "state", "codex-remote"), SurfaceResumeEntry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
	})

	gateway := newLifecycleGateway()
	app, _, _ := newVSCodeAdminTestAppWithGateway(t, gateway, home, binaryPath, false)
	app.vscodeDetect = func() (vscodeDetectResponse, error) {
		return vscodeDetectResponse{
			CurrentMode:            "editor_settings",
			LatestBundleEntrypoint: "/tmp/fake-entrypoint",
		}, nil
	}

	app.onTick(context.Background(), time.Now().UTC())
	card := waitForLifecycleOperationTitle(t, gateway, "VS Code 接入需要迁移")

	app.handleUIEvents(context.Background(), []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:  "surface_resume_open_vscode",
			Title: "请先打开 VS Code",
			Text:  "还没有找到之前的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。",
		},
	}})

	ops := gateway.snapshotOperations()
	if len(ops) != 2 {
		t.Fatalf("expected async vscode guidance to reuse compatibility card, got %#v", ops)
	}
	if ops[0].Kind != feishu.OperationSendCard || ops[1].Kind != feishu.OperationUpdateCard {
		t.Fatalf("expected send then in-place update, got %#v", ops)
	}
	if ops[1].MessageID != card.MessageID {
		t.Fatalf("expected follow-up guidance to patch original compatibility card %q, got %#v", card.MessageID, ops[1])
	}
	if ops[1].CardTitle != "请先打开 VS Code" || !strings.Contains(operationCardText(ops[1]), "请先打开 VS Code") {
		t.Fatalf("expected open-vscode guidance update, got %#v", ops[1].CardElements)
	}
}

func waitForLifecycleOperationTitle(t *testing.T, gateway *lifecycleGateway, title string) feishu.Operation {
	t.Helper()
	var found feishu.Operation
	waitForDaemonCondition(t, 2*time.Second, func() bool {
		for _, operation := range gateway.snapshotOperations() {
			if operation.CardTitle == title {
				found = operation
				return true
			}
		}
		return false
	})
	return found
}
