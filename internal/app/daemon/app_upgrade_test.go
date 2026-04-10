package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestUpgradeLatestManualCheckPromptsIdleSurface(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.upgradeLookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
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
	if stateValue.PendingUpgrade.Source != install.UpgradeSourceRelease {
		t.Fatalf("pending source = %q, want release", stateValue.PendingUpgrade.Source)
	}
	if stateValue.PendingUpgrade.TargetSlot != "v1.1.0" {
		t.Fatalf("pending target slot = %q, want v1.1.0", stateValue.PendingUpgrade.TargetSlot)
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

func TestFlushUpgradeResultEmitsNoticeAndClearsPendingState(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)

	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	stateValue.CurrentVersion = "v1.1.0"
	stateValue.PendingUpgrade = &install.PendingUpgrade{
		Phase:            install.PendingUpgradePhaseCommitted,
		TargetTrack:      install.ReleaseTrackProduction,
		TargetVersion:    "v1.1.0",
		GatewayID:        "main",
		SurfaceSessionID: "feishu:chat:9",
		ChatID:           "chat-9",
		ActorUserID:      "user-9",
	}
	if err := install.WriteState(statePath, stateValue); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	app.onTick(context.Background(), time.Now().UTC())

	waitForUpgradeOperation(t, gateway, func(ops []feishuOperationView) bool {
		for _, op := range ops {
			if op.CardTitle == "Debug" && op.SurfaceSessionID == "feishu:chat:9" {
				return true
			}
		}
		return false
	})

	updated, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState updated: %v", err)
	}
	if updated.PendingUpgrade != nil {
		t.Fatalf("expected pending upgrade result to be cleared, got %#v", updated.PendingUpgrade)
	}
}

func TestCopyUpgradeHelperBinaryUsesCurrentBinaryPath(t *testing.T) {
	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)

	currentBinary := filepath.Join(filepath.Dir(statePath), "current-live")
	otherBinary := filepath.Join(filepath.Dir(statePath), "daemon-self")
	if err := os.WriteFile(currentBinary, []byte("current-live-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile current: %v", err)
	}
	if err := os.WriteFile(otherBinary, []byte("daemon-self-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile self: %v", err)
	}
	app.serverIdentity.BinaryPath = otherBinary

	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	stateValue.CurrentBinaryPath = currentBinary

	helperPath, err := app.copyUpgradeHelperBinaryLocked(stateValue)
	if err != nil {
		t.Fatalf("copyUpgradeHelperBinaryLocked: %v", err)
	}
	raw, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("ReadFile helper: %v", err)
	}
	if string(raw) != "current-live-binary" {
		t.Fatalf("helper content = %q, want current-live-binary", string(raw))
	}
}

func TestBuildDebugStatusCatalogIsInteractiveAndIncludesForm(t *testing.T) {
	catalog := buildDebugStatusCatalog(install.InstallState{
		CurrentTrack:   install.ReleaseTrackBeta,
		CurrentVersion: "v1.0.0",
	}, false)
	if catalog == nil || !catalog.Interactive {
		t.Fatalf("expected interactive debug catalog, got %#v", catalog)
	}
	if len(catalog.Sections) != 3 {
		t.Fatalf("expected quick actions + track + form sections, got %#v", catalog.Sections)
	}
	if catalog.Sections[1].Entries[0].Buttons[1].Disabled != true {
		t.Fatalf("expected current beta track button to be disabled, got %#v", catalog.Sections[1].Entries[0].Buttons)
	}
	form := catalog.Sections[2].Entries[0].Form
	if form == nil || form.CommandText != "/debug" {
		t.Fatalf("expected debug form entry, got %#v", catalog.Sections[2].Entries[0])
	}
	if got := catalog.Sections[0].Entries[0].Buttons[1].CommandText; got != "/debug admin" {
		t.Fatalf("expected debug catalog to expose admin link button, got %#v", catalog.Sections[0].Entries[0].Buttons)
	}
}

func TestBuildUpgradeStatusCatalogIsInteractiveAndIncludesForm(t *testing.T) {
	catalog := buildUpgradeStatusCatalog(install.InstallState{
		CurrentTrack:   install.ReleaseTrackProduction,
		CurrentVersion: "v1.0.0",
	}, false)
	if catalog == nil || !catalog.Interactive {
		t.Fatalf("expected interactive upgrade catalog, got %#v", catalog)
	}
	if len(catalog.Sections) != 2 {
		t.Fatalf("expected quick actions + form sections, got %#v", catalog.Sections)
	}
	form := catalog.Sections[1].Entries[0].Form
	if form == nil || form.CommandText != "/upgrade" {
		t.Fatalf("expected upgrade form entry, got %#v", catalog.Sections[1].Entries[0])
	}
	if !strings.Contains(catalog.Summary, "本地升级产物：") {
		t.Fatalf("expected upgrade summary to keep artifact path, got %#v", catalog.Summary)
	}
}

func TestParseDebugCommandTextRecognizesAdmin(t *testing.T) {
	parsed, err := parseDebugCommandText("/debug admin")
	if err != nil {
		t.Fatalf("parseDebugCommandText: %v", err)
	}
	if parsed.Mode != debugCommandAdmin {
		t.Fatalf("mode = %q, want %q", parsed.Mode, debugCommandAdmin)
	}
}

func TestDebugAdminCommandIssuesExternalAccessLink(t *testing.T) {
	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	defer app.Shutdown(nil)
	app.ConfigureAdmin(AdminRuntimeOptions{
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	app.SetExternalAccess(ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        0,
			DefaultLinkTTL:    10 * time.Second,
			DefaultSessionTTL: 30 * time.Second,
			ProviderKind:      "disabled",
		},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDebugCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/debug admin",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops := gateway.snapshotOperations()
		for _, op := range ops {
			if op.CardTitle != "Debug" {
				continue
			}
			if !strings.Contains(op.CardBody, "临时管理页外链已生成") || !strings.Contains(op.CardBody, "/g/") {
				t.Fatalf("unexpected debug admin body: %#v", op.CardBody)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for debug admin link notice")
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
