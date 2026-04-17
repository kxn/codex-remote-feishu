package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestParseUpgradeCommandTextRecognizesDev(t *testing.T) {
	parsed, err := parseUpgradeCommandText("/upgrade dev")
	if err != nil {
		t.Fatalf("parseUpgradeCommandText: %v", err)
	}
	if parsed.Mode != upgradeCommandDev {
		t.Fatalf("mode = %q, want %q", parsed.Mode, upgradeCommandDev)
	}
}

func TestBuildUpgradePromptCatalogUsesDevCommandForDevCandidate(t *testing.T) {
	catalog := buildUpgradePromptCatalog(install.InstallState{
		CurrentVersion: "v1.0.0",
		PendingUpgrade: &install.PendingUpgrade{
			Source:        install.UpgradeSourceDev,
			TargetVersion: "dev-abc123",
			TargetSlot:    "dev-abc123",
		},
	})
	if catalog == nil {
		t.Fatal("expected prompt catalog")
	}
	if catalog.Title != "发现开发版更新" {
		t.Fatalf("title = %q, want 开发版更新", catalog.Title)
	}
	entry := catalog.Sections[0].Entries[0]
	if got := entry.Buttons[0].CommandText; got != "/upgrade dev" {
		t.Fatalf("confirm command = %q, want /upgrade dev", got)
	}
	if !strings.Contains(catalog.Summary, "再次发送 /upgrade dev") {
		t.Fatalf("summary = %q, want /upgrade dev guidance", catalog.Summary)
	}
}

func TestUpgradeDevManualCheckPromptsIdleSurface(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.upgradeRuntime.devManifest = func(context.Context) (install.DevManifest, install.DevManifestAsset, error) {
		return install.DevManifest{Version: "dev-abc123"}, install.DevManifestAsset{Name: "target", URL: "https://example.invalid/target"}, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "/upgrade dev",
	})

	waitForUpgradeOperation(t, gateway, func(ops []feishuOperationView) bool {
		for _, op := range ops {
			if op.CardTitle == "发现开发版更新" {
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
		t.Fatal("expected pending upgrade")
	}
	if stateValue.PendingUpgrade.Source != install.UpgradeSourceDev {
		t.Fatalf("pending source = %q, want dev", stateValue.PendingUpgrade.Source)
	}
	if stateValue.PendingUpgrade.TargetVersion != "dev-abc123" {
		t.Fatalf("pending target version = %q, want dev-abc123", stateValue.PendingUpgrade.TargetVersion)
	}
	if stateValue.PendingUpgrade.Phase != install.PendingUpgradePhasePrompted {
		t.Fatalf("pending phase = %q, want prompted", stateValue.PendingUpgrade.Phase)
	}
}

func TestUpgradeLatestDoesNotContinueDevCandidate(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)

	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	stateValue.PendingUpgrade = &install.PendingUpgrade{
		Phase:         install.PendingUpgradePhasePrompted,
		Source:        install.UpgradeSourceDev,
		TargetTrack:   install.ReleaseTrackProduction,
		TargetVersion: "dev-abc123",
		TargetSlot:    "dev-abc123",
	}
	if err := install.WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade latest",
	})

	waitForUpgradeNoticeBody(t, gateway, "当前已有 dev 构建升级候选")

	updated, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState updated: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.Source != install.UpgradeSourceDev {
		t.Fatalf("pending upgrade = %#v, want dev candidate to remain", updated.PendingUpgrade)
	}
}

func TestUpgradeDevClearsStalePendingCandidateMatchingLiveVersion(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.serverIdentity.Version = "dev-abc123"
	app.upgradeRuntime.devManifest = func(context.Context) (install.DevManifest, install.DevManifestAsset, error) {
		return install.DevManifest{Version: "dev-abc123"}, install.DevManifestAsset{Name: "target", URL: "https://example.invalid/target"}, nil
	}

	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	stateValue.CurrentVersion = "v1.0.0"
	stateValue.PendingUpgrade = &install.PendingUpgrade{
		Phase:         install.PendingUpgradePhasePrompted,
		Source:        install.UpgradeSourceDev,
		TargetTrack:   install.ReleaseTrackProduction,
		TargetVersion: "dev-abc123",
		TargetSlot:    "dev-abc123",
	}
	if err := install.WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade dev",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			if op.CardTitle == "Upgrade" && strings.Contains(op.CardBody, "当前已经是最新 dev 构建 dev-abc123。") {
				updated, err := install.LoadState(statePath)
				if err != nil {
					t.Fatalf("LoadState updated: %v", err)
				}
				if updated.PendingUpgrade != nil {
					t.Fatalf("expected stale pending upgrade to be cleared, got %#v", updated.PendingUpgrade)
				}
				if updated.CurrentVersion != "dev-abc123" {
					t.Fatalf("current version = %q, want dev-abc123", updated.CurrentVersion)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for latest dev notice")
}
