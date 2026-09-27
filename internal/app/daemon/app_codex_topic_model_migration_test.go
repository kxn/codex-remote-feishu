package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestMigrateLegacyCodexTopicOverridesSeedsExistingSnapshotBeforeMaterialize(t *testing.T) {
	stateDir := t.TempDir()
	surfaces, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load surface store: %v", err)
	}
	for _, surfaceID := range []string{"feishu:app-1:chat:topic-a", "feishu:app-1:chat:topic-b"} {
		if err := surfaces.Put(legacyCodexMigrationSurface(surfaceID, "app-1")); err != nil {
			t.Fatalf("put %s: %v", surfaceID, err)
		}
	}
	bots, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load bot store: %v", err)
	}
	if err := bots.Put(legacyCodexMigrationBot("app-1")); err != nil {
		t.Fatalf("put bot record: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})

	for _, surfaceID := range []string{"feishu:app-1:chat:topic-a", "feishu:app-1:chat:topic-b"} {
		surface := app.service.Surface(surfaceID)
		if surface == nil {
			t.Fatalf("existing surface %s was not materialized", surfaceID)
		}
		if got := surface.CodexPromptOverride; got.Model != "luna" || got.ReasoningEffort != "high" {
			t.Fatalf("%s Codex override = %#v, want luna/high", surfaceID, got)
		}
	}
	records := app.service.BotCapabilitySettings()
	if len(records) != 1 || records[0].PromptOverride.Model != "" || records[0].PromptOverride.ReasoningEffort != "" {
		t.Fatalf("materialized bot records retained legacy Codex override: %#v", records)
	}

	const newSurfaceID = "feishu:app-1:chat:topic-c"
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: newSurfaceID,
		ChatID:           "topic-c",
		ActorUserID:      "user-1",
	})
	newSurface := app.service.Surface(newSurfaceID)
	if newSurface == nil {
		t.Fatal("new surface was not materialized after successful migration")
	}
	if got := newSurface.CodexPromptOverride; got.Model != "" || got.ReasoningEffort != "" {
		t.Fatalf("new surface inherited legacy Codex override: %#v", got)
	}

	reloadedBots, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload bot store: %v", err)
	}
	record, ok := reloadedBots.Get(state.BotCapabilitySettingsKey("app-1"))
	if !ok || record.PromptOverride.Model != "" || record.PromptOverride.ReasoningEffort != "" {
		t.Fatalf("persisted bot record retained legacy Codex override: %#v, ok=%v", record, ok)
	}
}

func TestMigrateLegacyCodexTopicOverridesIgnoresNonFeishuInvalidAndGatewayMismatch(t *testing.T) {
	surfaces := surfaceresume.NewStore(filepath.Join(t.TempDir(), "surfaces.json"))
	entries := map[string]surfaceresume.Entry{
		"feishu:app-1:chat:topic-valid": legacyCodexMigrationSurface("feishu:app-1:chat:topic-valid", "app-1"),
		"local-surface":                 legacyCodexMigrationSurface("local-surface", "app-1"),
		"feishu:app-1:chat":             legacyCodexMigrationSurface("feishu:app-1:chat", "app-1"),
		"feishu:app-2:chat:topic-wrong": legacyCodexMigrationSurface("feishu:app-2:chat:topic-wrong", "app-1"),
		"feishu:app-1:chat:field-wrong": legacyCodexMigrationSurface("feishu:app-1:chat:field-wrong", "app-2"),
		"feishu:app-1:chat:partial":     legacyCodexMigrationSurface("feishu:app-1:chat:partial", "app-1"),
	}
	partial := entries["feishu:app-1:chat:partial"]
	partial.CodexModelOverride = "sol"
	partial.UpdatedAt = time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	entries[partial.SurfaceSessionID] = partial
	if err := surfaces.ReplaceAll(entries); err != nil {
		t.Fatalf("seed surface store: %v", err)
	}
	bots := botcapabilitysettings.NewStore(filepath.Join(t.TempDir(), "bots.json"))
	if err := bots.ReplaceAll([]state.BotCapabilitySettingsRecord{
		legacyCodexMigrationBot("app-1"),
		{
			GatewayID:   "app-2",
			ProductMode: state.ProductModeNormal,
			Backend:     agentproto.BackendClaude,
			PromptOverride: state.ModelConfigRecord{
				Model:           "claude-model",
				ReasoningEffort: "high",
			},
		},
	}); err != nil {
		t.Fatalf("seed bot store: %v", err)
	}

	if err := migrateLegacyCodexTopicOverrides(surfaces, bots); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := surfaces.Entries()
	valid := got["feishu:app-1:chat:topic-valid"]
	if valid.CodexModelOverride != "luna" || valid.CodexReasoningEffortOverride != "high" {
		t.Fatalf("valid Feishu surface override = %#v, want luna/high", valid)
	}
	partial = got["feishu:app-1:chat:partial"]
	if partial.CodexModelOverride != "sol" || partial.CodexReasoningEffortOverride != "high" {
		t.Fatalf("partial existing override = %#v, want sol/high", partial)
	}
	for _, surfaceID := range []string{
		"local-surface",
		"feishu:app-1:chat",
		"feishu:app-2:chat:topic-wrong",
		"feishu:app-1:chat:field-wrong",
	} {
		entry := got[surfaceID]
		if entry.CodexModelOverride != "" || entry.CodexReasoningEffortOverride != "" {
			t.Fatalf("ineligible surface %s was seeded: %#v", surfaceID, entry)
		}
	}
	claude, ok := bots.Get(state.BotCapabilitySettingsKey("app-2"))
	if !ok || claude.PromptOverride.Model != "claude-model" || claude.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("non-Codex bot record changed: %#v, ok=%v", claude, ok)
	}
}

func TestMigrateLegacyCodexTopicOverridesSurfaceFailureKeepsBotAndGateBlocked(t *testing.T) {
	surfaces, surfacePath := legacyCodexMigrationSurfaceStore(t)
	if err := surfaces.Put(legacyCodexMigrationSurface("feishu:app-1:chat:topic-a", "app-1")); err != nil {
		t.Fatalf("seed surface: %v", err)
	}
	bots, botPath := legacyCodexMigrationBotStore(t)
	if err := bots.Put(legacyCodexMigrationBot("app-1")); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	blockLegacyCodexMigrationStoreParent(t, surfacePath)

	app := newLegacyCodexMigrationApp(surfaces, surfacePath, bots, botPath)
	app.mu.Lock()
	err := app.runLegacyCodexTopicOverrideMigrationLocked()
	app.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "surface") {
		t.Fatalf("surface phase error = %v", err)
	}
	if app.surfaceResumeRuntime.codexTopicModelMigrationErr == nil {
		t.Fatal("surface phase failure did not keep migration gate blocked")
	}
	record, ok := bots.Get(state.BotCapabilitySettingsKey("app-1"))
	if !ok || record.PromptOverride.Model != "luna" || record.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("surface phase failure changed bot store: %#v, ok=%v", record, ok)
	}
}

func TestMigrateLegacyCodexTopicOverridesBotFailureRetriesIdempotently(t *testing.T) {
	surfaces, surfacePath := legacyCodexMigrationSurfaceStore(t)
	if err := surfaces.Put(legacyCodexMigrationSurface("feishu:app-1:chat:topic-a", "app-1")); err != nil {
		t.Fatalf("seed surface: %v", err)
	}
	bots, botPath := legacyCodexMigrationBotStore(t)
	if err := bots.Put(legacyCodexMigrationBot("app-1")); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	blockLegacyCodexMigrationStoreParent(t, botPath)

	app := newLegacyCodexMigrationApp(surfaces, surfacePath, bots, botPath)
	app.mu.Lock()
	err := app.runLegacyCodexTopicOverrideMigrationLocked()
	app.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "bot") {
		t.Fatalf("bot phase error = %v", err)
	}
	seeded, ok := surfaces.Get("feishu:app-1:chat:topic-a")
	if !ok || seeded.CodexModelOverride != "luna" || seeded.CodexReasoningEffortOverride != "high" {
		t.Fatalf("bot phase failure lost committed surface seed: %#v, ok=%v", seeded, ok)
	}
	persistedSurfaces, err := surfaceresume.LoadStore(surfacePath)
	if err != nil {
		t.Fatalf("reload committed surface phase: %v", err)
	}
	persistedSeed, ok := persistedSurfaces.Get("feishu:app-1:chat:topic-a")
	if !ok || persistedSeed.CodexModelOverride != "luna" || persistedSeed.CodexReasoningEffortOverride != "high" {
		t.Fatalf("bot phase failure did not persist surface seed: %#v, ok=%v", persistedSeed, ok)
	}
	if app.surfaceResumeRuntime.codexTopicModelMigrationErr == nil {
		t.Fatal("bot phase failure did not keep migration gate blocked")
	}

	repairLegacyCodexMigrationStoreParent(t, botPath)
	app.mu.Lock()
	err = app.runLegacyCodexTopicOverrideMigrationLocked()
	app.mu.Unlock()
	if err != nil || app.surfaceResumeRuntime.codexTopicModelMigrationErr != nil {
		t.Fatalf("migration retry remained blocked: err=%v gate=%v", err, app.surfaceResumeRuntime.codexTopicModelMigrationErr)
	}
	record, ok := bots.Get(state.BotCapabilitySettingsKey("app-1"))
	if !ok || record.PromptOverride.Model != "" || record.PromptOverride.ReasoningEffort != "" {
		t.Fatalf("retry did not clear bot override: %#v, ok=%v", record, ok)
	}

	const newSurfaceID = "feishu:app-1:chat:topic-c"
	if err := surfaces.Put(legacyCodexMigrationSurface(newSurfaceID, "app-1")); err != nil {
		t.Fatalf("add post-migration surface: %v", err)
	}
	if err := migrateLegacyCodexTopicOverrides(surfaces, bots); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	newSurface, ok := surfaces.Get(newSurfaceID)
	if !ok || newSurface.CodexModelOverride != "" || newSurface.CodexReasoningEffortOverride != "" {
		t.Fatalf("idempotent retry polluted new surface: %#v, ok=%v", newSurface, ok)
	}
}

func TestMigrateLegacyCodexTopicOverridesBlocksNilAndDegradedStores(t *testing.T) {
	availableSurfaces := surfaceresume.NewStore("")
	availableBots := botcapabilitysettings.NewStore("")
	for _, test := range []struct {
		name          string
		surfaces      *surfaceresume.Store
		surfaceStatus persistedStoreStatus
		bots          *botcapabilitysettings.Store
		botStatus     persistedStoreStatus
	}{
		{name: "nil surface", bots: availableBots},
		{name: "nil bot", surfaces: availableSurfaces},
		{name: "degraded surface", surfaces: availableSurfaces, surfaceStatus: persistedStoreStatusDegraded, bots: availableBots},
		{name: "degraded bot", surfaces: availableSurfaces, bots: availableBots, botStatus: persistedStoreStatusDegraded},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := New(":0", ":0", nil, agentproto.ServerIdentity{})
			app.surfaceResumeRuntime.store = test.surfaces
			app.surfaceResumeRuntime.status = test.surfaceStatus
			app.botCapabilitySettingsState.store = test.bots
			app.botCapabilitySettingsState.status = test.botStatus
			app.mu.Lock()
			err := app.runLegacyCodexTopicOverrideMigrationLocked()
			app.mu.Unlock()
			if err == nil || app.surfaceResumeRuntime.codexTopicModelMigrationErr == nil {
				t.Fatalf("unavailable stores did not block migration: err=%v gate=%v", err, app.surfaceResumeRuntime.codexTopicModelMigrationErr)
			}
			if len(app.service.Surfaces()) != 0 {
				t.Fatalf("blocked migration materialized surfaces: %#v", app.service.Surfaces())
			}
		})
	}
}

func TestCodexTopicModelMigrationBlocksIngressWithoutCreatingSurface(t *testing.T) {
	gateway := &codexTopicMigrationGateGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	gateway.app = app
	app.gatewayApplyTimeout = time.Second
	app.mu.Lock()
	if err := app.runLegacyCodexTopicOverrideMigrationLocked(); err == nil {
		app.mu.Unlock()
		t.Fatal("nil stores unexpectedly completed migration")
	}
	app.mu.Unlock()

	const surfaceID = "feishu:app-1:chat:topic-new"
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: surfaceID,
		ChatID:           "topic-new",
		ActorUserID:      "user-1",
		Text:             "hello",
	})
	if surface := app.service.Surface(surfaceID); surface != nil {
		t.Fatalf("blocked ingress created surface: %#v", surface)
	}
	if !gateway.lockReleased {
		t.Fatal("maintenance delivery held App mutex during gateway.Apply")
	}
	if !gateway.hadDeadline {
		t.Fatal("maintenance delivery did not use timeout context")
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("maintenance operations = %#v, want one", gateway.operations)
	}
	op := gateway.operations[0]
	if op.Kind != feishu.OperationSendText || op.GatewayID != "app-1" || op.ReceiveIDType != "chat_id" || op.ReceiveID != "topic-new" {
		t.Fatalf("maintenance operation target = %#v", op)
	}
	if op.SurfaceSessionID != "" {
		t.Fatalf("maintenance operation unexpectedly depended on surface %q", op.SurfaceSessionID)
	}
	if !strings.Contains(op.Text, "模型设置迁移未完成") || !strings.Contains(op.Text, "修复状态目录后重启") {
		t.Fatalf("maintenance text = %q", op.Text)
	}
}

func TestCodexTopicModelMigrationKeepsStaleCardGuardBeforeIngress(t *testing.T) {
	gateway := &codexTopicMigrationGateGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	gateway.app = app
	app.mu.Lock()
	if err := app.runLegacyCodexTopicOverrideMigrationLocked(); err == nil {
		app.mu.Unlock()
		t.Fatal("nil stores unexpectedly completed migration")
	}
	app.mu.Unlock()

	const surfaceID = "feishu:app-1:chat:topic-old-card"
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		GatewayID:        "app-1",
		SurfaceSessionID: surfaceID,
		ChatID:           "topic-old-card",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "old-daemon",
		},
	})
	if surface := app.service.Surface(surfaceID); surface != nil {
		t.Fatalf("stale blocked ingress created surface: %#v", surface)
	}
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].Text, "旧卡片已过期") {
		t.Fatalf("stale-card guard did not win before migration gate: %#v", gateway.operations)
	}
	if strings.Contains(gateway.operations[0].Text, "模型设置迁移未完成") {
		t.Fatalf("stale card was misclassified as migration maintenance: %#v", gateway.operations[0])
	}
}

func TestCodexTopicModelMigrationBlocksBotAddedBeforeChatInfo(t *testing.T) {
	gateway := &primaryBootstrapGateway{chatInfo: feishu.ChatInfo{BotCount: 1, ChatMode: "group"}}
	app := newPrimaryBootstrapTestApp(t, gateway)
	app.surfaceResumeRuntime.codexTopicModelMigrationErr = errors.New("migration blocked")

	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))

	if gateway.chatInfoCalls != 0 {
		t.Fatalf("blocked bot-added read chat info %d time(s)", gateway.chatInfoCalls)
	}
	if records := app.service.FeishuRoomState(); len(records) != 0 {
		t.Fatalf("blocked bot-added mutated primary state: %#v", records)
	}
	if got := app.feishuPrimaryGatewayForChat("oc_room"); got != "" {
		t.Fatalf("blocked bot-added primary snapshot = %q, want empty", got)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("blocked bot-added operations = %#v, want one", gateway.operations)
	}
	op := gateway.operations[0]
	if op.Kind != feishu.OperationSendText || op.GatewayID != "app-1" || op.ReceiveIDType != "chat_id" || op.ReceiveID != "oc_room" || !strings.Contains(op.Text, "模型设置迁移未完成") {
		t.Fatalf("blocked bot-added maintenance operation = %#v", op)
	}
}

func TestCodexTopicModelMigrationBlocksUpgradeResultIngressWithoutCreatingSurface(t *testing.T) {
	app, statePath := newUpgradeTestApp(t, newLifecycleGateway())
	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("load upgrade state: %v", err)
	}
	stateValue.PendingUpgrade = &install.PendingUpgrade{
		Phase:            install.PendingUpgradePhaseCommitted,
		TargetVersion:    "v1.1.0",
		GatewayID:        "main",
		SurfaceSessionID: "feishu:main:chat:upgrade-result",
		ChatID:           "upgrade-result",
		ActorUserID:      "user-1",
	}
	if err := install.WriteState(statePath, stateValue); err != nil {
		t.Fatalf("write pending upgrade result: %v", err)
	}
	app.surfaceResumeRuntime.codexTopicModelMigrationErr = errors.New("migration blocked")

	app.mu.Lock()
	events := app.maybeFlushUpgradeResultLocked(time.Now().UTC())
	app.mu.Unlock()

	if len(events) != 0 {
		t.Fatalf("blocked upgrade result emitted events: %#v", events)
	}
	if surface := app.service.Surface("feishu:main:chat:upgrade-result"); surface != nil {
		t.Fatalf("blocked upgrade result created surface: %#v", surface)
	}
	updated, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("reload upgrade state: %v", err)
	}
	if updated.PendingUpgrade == nil || updated.PendingUpgrade.ResultDeliveryAttempts != 0 {
		t.Fatalf("blocked upgrade result consumed a delivery attempt: %#v", updated.PendingUpgrade)
	}
}

func legacyCodexMigrationBot(gatewayID string) state.BotCapabilitySettingsRecord {
	return state.BotCapabilitySettingsRecord{
		GatewayID:   gatewayID,
		ProductMode: state.ProductModeNormal,
		Backend:     agentproto.BackendCodex,
		PromptOverride: state.ModelConfigRecord{
			Model:           "luna",
			ReasoningEffort: "high",
			AccessMode:      "workspace-write",
		},
	}
}

func legacyCodexMigrationSurface(surfaceID, gatewayID string) surfaceresume.Entry {
	return surfaceresume.Entry{
		SurfaceSessionID: surfaceID,
		GatewayID:        gatewayID,
		ChatID:           surfaceID,
		ActorUserID:      "user-1",
		ProductMode:      string(state.ProductModeNormal),
		Backend:          string(agentproto.BackendCodex),
	}
}

func legacyCodexMigrationSurfaceStore(t *testing.T) (*surfaceresume.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "surface-store", surfaceresume.StateFileName)
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load surface store: %v", err)
	}
	return store, path
}

func legacyCodexMigrationBotStore(t *testing.T) (*botcapabilitysettings.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bot-store", botcapabilitysettings.StateFileName)
	store, err := botcapabilitysettings.LoadStore(path)
	if err != nil {
		t.Fatalf("load bot store: %v", err)
	}
	return store, path
}

func newLegacyCodexMigrationApp(surfaces *surfaceresume.Store, surfacePath string, bots *botcapabilitysettings.Store, botPath string) *App {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.surfaceResumeRuntime.persistedStoreRuntimeState = persistedStoreRuntimeState[*surfaceresume.Store]{
		store: surfaces,
		path:  surfacePath,
	}
	app.botCapabilitySettingsState.persistedStoreRuntimeState = persistedStoreRuntimeState[*botcapabilitysettings.Store]{
		store: bots,
		path:  botPath,
	}
	return app
}

func blockLegacyCodexMigrationStoreParent(t *testing.T, path string) {
	t.Helper()
	parent := filepath.Dir(path)
	if err := os.RemoveAll(parent); err != nil {
		t.Fatalf("remove store parent: %v", err)
	}
	if err := os.WriteFile(parent, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("replace store parent with file: %v", err)
	}
}

func repairLegacyCodexMigrationStoreParent(t *testing.T, path string) {
	t.Helper()
	parent := filepath.Dir(path)
	if err := os.Remove(parent); err != nil {
		t.Fatalf("remove blocking file: %v", err)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("recreate store parent: %v", err)
	}
}

type codexTopicMigrationGateGateway struct {
	app          *App
	operations   []feishu.Operation
	lockReleased bool
	hadDeadline  bool
}

func (g *codexTopicMigrationGateGateway) Start(context.Context, feishu.ActionHandler) error {
	return nil
}

func (g *codexTopicMigrationGateGateway) Apply(ctx context.Context, operations []feishu.Operation) error {
	_, g.hadDeadline = ctx.Deadline()
	if g.app != nil && g.app.mu.TryLock() {
		g.lockReleased = true
		g.app.mu.Unlock()
	}
	g.operations = append(g.operations, operations...)
	return nil
}

func TestMigrateLegacyCodexTopicOverridesPreservesExplicitClear(t *testing.T) {
	stamp := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	surfaces := surfaceresume.NewStore(filepath.Join(t.TempDir(), surfaceresume.StateFileName))
	bots := botcapabilitysettings.NewStore(filepath.Join(t.TempDir(), botcapabilitysettings.StateFileName))
	entry := legacyCodexMigrationSurface("feishu:app-1:chat:cleared", "app-1")
	entry.CodexPromptOverrideUpdatedAt = stamp
	if err := surfaces.Put(entry); err != nil {
		t.Fatal(err)
	}
	if err := bots.Put(legacyCodexMigrationBot("app-1")); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyCodexTopicOverrides(surfaces, bots); err != nil {
		t.Fatal(err)
	}
	got, _ := surfaces.Get(entry.SurfaceSessionID)
	if got.CodexModelOverride != "" || got.CodexReasoningEffortOverride != "" || !got.CodexPromptOverrideUpdatedAt.Equal(stamp) {
		t.Fatalf("bot migration resurrected explicit clear: %#v", got)
	}
}
