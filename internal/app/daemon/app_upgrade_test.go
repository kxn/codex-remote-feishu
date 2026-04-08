package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestDebugUpgradeManualCheckPromptsIdleSurface(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.upgradeLookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDebugCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "/debug upgrade",
	})

	waitForUpgradeOperation(t, gateway, func(ops []feishuOperationView) bool {
		for _, op := range ops {
			if op.CardTitle == "发现可升级版本" {
				return true
			}
		}
		return false
	})

	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if stateValue.PendingUpgrade == nil {
		t.Fatal("expected pending upgrade to be recorded")
	}
	if stateValue.PendingUpgrade.TargetVersion != "v1.1.0" {
		t.Fatalf("pending target version = %q, want v1.1.0", stateValue.PendingUpgrade.TargetVersion)
	}
	if stateValue.PendingUpgrade.Phase != install.PendingUpgradePhasePrompted {
		t.Fatalf("pending phase = %q, want %q", stateValue.PendingUpgrade.Phase, install.PendingUpgradePhasePrompted)
	}
	if stateValue.PendingUpgrade.SurfaceSessionID != "feishu:chat:1" {
		t.Fatalf("pending surface = %q, want feishu:chat:1", stateValue.PendingUpgrade.SurfaceSessionID)
	}
}

func TestDebugTrackSwitchPersistsAndClearsCandidate(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)

	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	stateValue.PendingUpgrade = &install.PendingUpgrade{
		Phase:         install.PendingUpgradePhaseAvailable,
		TargetTrack:   install.ReleaseTrackProduction,
		TargetVersion: "v1.1.0",
	}
	if err := install.WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDebugCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/debug track beta",
	})

	updated, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState updated: %v", err)
	}
	if updated.CurrentTrack != install.ReleaseTrackBeta {
		t.Fatalf("current track = %q, want beta", updated.CurrentTrack)
	}
	if updated.PendingUpgrade != nil {
		t.Fatalf("expected pending upgrade to be cleared, got %#v", updated.PendingUpgrade)
	}
}

func TestAutoUpgradeCheckPromptsMostRecentIdleSurface(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.upgradeStartupDelay = 0
	app.upgradeCheckInterval = time.Hour
	app.upgradeLookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v1.1.0"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "feishu:chat:2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})

	app.onTick(context.Background(), time.Now().UTC())

	waitForUpgradeOperation(t, gateway, func(ops []feishuOperationView) bool {
		for _, op := range ops {
			if op.CardTitle == "发现可升级版本" && op.SurfaceSessionID == "feishu:chat:2" {
				return true
			}
		}
		return false
	})

	updated, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState updated: %v", err)
	}
	if updated.PendingUpgrade == nil {
		t.Fatal("expected pending upgrade after auto check")
	}
	if updated.PendingUpgrade.SurfaceSessionID != "feishu:chat:2" {
		t.Fatalf("pending prompt surface = %q, want feishu:chat:2", updated.PendingUpgrade.SurfaceSessionID)
	}
}

type feishuOperationView struct {
	SurfaceSessionID string
	CardTitle        string
}

func waitForUpgradeOperation(t *testing.T, gateway *lifecycleGateway, predicate func([]feishuOperationView) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops := gateway.snapshotOperations()
		views := make([]feishuOperationView, 0, len(ops))
		for _, op := range ops {
			views = append(views, feishuOperationView{
				SurfaceSessionID: op.SurfaceSessionID,
				CardTitle:        op.CardTitle,
			})
		}
		if predicate(views) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for expected upgrade operation")
}

func newUpgradeTestApp(t *testing.T, gateway *lifecycleGateway) (*App, string) {
	t.Helper()

	dataDir := t.TempDir()
	binaryPath := filepath.Join(dataDir, "codex-remote")
	statePath := filepath.Join(dataDir, "install-state.json")
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		BinaryIdentity: agentproto.BinaryIdentity{
			Version:    "v1.0.0",
			BinaryPath: binaryPath,
		},
	})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: binaryPath,
		Paths: relayruntime.Paths{
			DataDir:  dataDir,
			StateDir: dataDir,
			LogsDir:  dataDir,
		},
	})

	stateValue := install.InstallState{
		StatePath:         statePath,
		InstallSource:     install.InstallSourceRelease,
		CurrentTrack:      install.ReleaseTrackProduction,
		CurrentVersion:    "v1.0.0",
		CurrentBinaryPath: binaryPath,
		VersionsRoot:      filepath.Join(dataDir, "releases"),
		CurrentSlot:       "v1.0.0",
	}
	if err := install.WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	return app, statePath
}
