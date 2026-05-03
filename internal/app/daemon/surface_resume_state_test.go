package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

const testCanonicalResumeWorkspace = "/tmp/codex-remote/workspace-demo"

func TestSurfaceResumeStoreRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}

	updatedAt := time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC)
	if err := store.Put(surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "vscode",
		Verbosity:          " verbose ",
		ResumeInstanceID:   "inst-1",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  " 修复登录流程 ",
		ResumeThreadCWD:    " /data/dl/work/../droid/ ",
		ResumeWorkspaceKey: " /data/dl/work/../droid/ ",
		ResumeRouteMode:    "follow_local",
		ResumeHeadless:     true,
		UpdatedAt:          updatedAt,
	}); err != nil {
		t.Fatalf("put surface resume entry: %v", err)
	}

	reloaded, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	entry, ok := reloaded.Get("surface-1")
	if !ok {
		t.Fatal("expected surface resume entry after reload")
	}
	if entry.GatewayID != "app-1" || entry.ChatID != "chat-1" || entry.ActorUserID != "user-1" {
		t.Fatalf("unexpected routing fields: %#v", entry)
	}
	if entry.ProductMode != "vscode" || entry.Verbosity != "verbose" || entry.ResumeInstanceID != "inst-1" || entry.ResumeThreadID != "thread-1" {
		t.Fatalf("unexpected resume target fields: %#v", entry)
	}
	if entry.ResumeThreadTitle != "修复登录流程" || entry.ResumeThreadCWD != "/data/dl/droid" || entry.ResumeHeadless {
		t.Fatalf("unexpected normalized resume metadata: %#v", entry)
	}
	if entry.ResumeWorkspaceKey != "/data/dl/droid" || entry.ResumeRouteMode != "follow_local" {
		t.Fatalf("unexpected normalized workspace or route: %#v", entry)
	}
	if !entry.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected updatedAt: %s", entry.UpdatedAt)
	}
}

func TestSurfaceResumeStoreDedupesSplitFeishuP2PSurfacesOnPut(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}

	shadowUpdatedAt := time.Date(2026, 4, 11, 2, 47, 45, 0, time.UTC)
	if err := store.Put(surfaceresume.Entry{
		SurfaceSessionID:   "feishu:Codex-5:user:a756fefe",
		GatewayID:          "Codex-5",
		ChatID:             "oc_099318e4d660955369af84e8c8aea268",
		ActorUserID:        "a756fefe",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-headless-pool-1",
		ResumeWorkspaceKey: "/data/dl/.local/state/codex-remote",
		ResumeRouteMode:    "unbound",
		ResumeHeadless:     true,
		UpdatedAt:          shadowUpdatedAt,
	}); err != nil {
		t.Fatalf("put shadow feishu surface: %v", err)
	}

	canonicalUpdatedAt := time.Date(2026, 4, 14, 4, 10, 35, 0, time.UTC)
	if err := store.Put(surfaceresume.Entry{
		SurfaceSessionID:   "feishu:Codex-5:user:ou_7588194bf7ffe98ef2845026aa398169",
		GatewayID:          "Codex-5",
		ChatID:             "oc_099318e4d660955369af84e8c8aea268",
		ActorUserID:        "ou_7588194bf7ffe98ef2845026aa398169",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-headless-2",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "你好你好",
		ResumeThreadCWD:    testCanonicalResumeWorkspace,
		ResumeWorkspaceKey: testCanonicalResumeWorkspace,
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
		UpdatedAt:          canonicalUpdatedAt,
	}); err != nil {
		t.Fatalf("put canonical feishu surface: %v", err)
	}

	reloaded, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	entries := reloaded.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected one deduped entry, got %#v", entries)
	}
	if _, ok := reloaded.Get("feishu:Codex-5:user:a756fefe"); ok {
		t.Fatalf("expected shadow surface entry to be removed, got %#v", entries)
	}
	entry, ok := reloaded.Get("feishu:Codex-5:user:ou_7588194bf7ffe98ef2845026aa398169")
	if !ok {
		t.Fatalf("expected canonical feishu entry after dedupe, got %#v", entries)
	}
	if entry.ResumeThreadID != "thread-1" || entry.ResumeRouteMode != "pinned" || entry.ResumeWorkspaceKey != testCanonicalResumeWorkspace {
		t.Fatalf("expected richer canonical resume target to win, got %#v", entry)
	}
	if !entry.UpdatedAt.Equal(canonicalUpdatedAt) {
		t.Fatalf("expected latest updatedAt to be preserved, got %#v", entry)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	if strings.Contains(string(raw), "a756fefe") {
		t.Fatalf("expected persisted state to drop shadow surface, got %s", raw)
	}
}

func TestDaemonStartupCanonicalizesLegacySplitFeishuP2PSurfaceResumeState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	writeSurfaceResumeStateForTest(t, stateDir, surfaceresume.StateFile{
		Version: surfaceresume.StateVersion,
		Entries: map[string]surfaceresume.Entry{
			"feishu:Codex-5:user:a756fefe": {
				SurfaceSessionID:   "feishu:Codex-5:user:a756fefe",
				GatewayID:          "Codex-5",
				ChatID:             "oc_099318e4d660955369af84e8c8aea268",
				ActorUserID:        "a756fefe",
				ProductMode:        "normal",
				ResumeInstanceID:   "inst-headless-pool-1",
				ResumeWorkspaceKey: "/data/dl/.local/state/codex-remote",
				ResumeRouteMode:    "unbound",
				ResumeHeadless:     true,
				UpdatedAt:          time.Date(2026, 4, 11, 2, 47, 45, 0, time.UTC),
			},
			"feishu:Codex-5:user:ou_7588194bf7ffe98ef2845026aa398169": {
				SurfaceSessionID:   "feishu:Codex-5:user:ou_7588194bf7ffe98ef2845026aa398169",
				GatewayID:          "Codex-5",
				ChatID:             "oc_099318e4d660955369af84e8c8aea268",
				ActorUserID:        "ou_7588194bf7ffe98ef2845026aa398169",
				ProductMode:        "normal",
				ResumeInstanceID:   "inst-headless-2",
				ResumeThreadID:     "thread-1",
				ResumeThreadTitle:  "你好你好",
				ResumeThreadCWD:    testCanonicalResumeWorkspace,
				ResumeWorkspaceKey: testCanonicalResumeWorkspace,
				ResumeRouteMode:    "pinned",
				ResumeHeadless:     true,
				UpdatedAt:          time.Date(2026, 4, 14, 4, 10, 35, 0, time.UTC),
			},
		},
	})

	app := newRestoreHintTestApp(stateDir)
	if snapshot := app.service.SurfaceSnapshot("feishu:Codex-5:user:a756fefe"); snapshot != nil {
		t.Fatalf("expected shadow surface not to materialize, got %#v", snapshot)
	}
	snapshot := app.service.SurfaceSnapshot("feishu:Codex-5:user:ou_7588194bf7ffe98ef2845026aa398169")
	if snapshot == nil {
		t.Fatal("expected canonical feishu surface to materialize")
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected canonical surface to stay latent after startup materialization, got %#v", snapshot)
	}

	if entry := app.SurfaceResumeState("feishu:Codex-5:user:a756fefe"); entry != nil {
		t.Fatalf("expected shadow resume state entry to be removed, got %#v", entry)
	}
	entry := app.SurfaceResumeState("feishu:Codex-5:user:ou_7588194bf7ffe98ef2845026aa398169")
	if entry == nil {
		t.Fatal("expected canonical resume state entry after startup")
	}
	if entry.ResumeThreadID != "thread-1" || entry.ResumeRouteMode != "pinned" || entry.ResumeWorkspaceKey != testCanonicalResumeWorkspace {
		t.Fatalf("expected canonical resume target after startup, got %#v", entry)
	}
}

func TestSurfaceResumeStoreDefaultsLegacyMissingVerbosityToNormal(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	raw := []byte("{\n  \"version\": 1,\n  \"entries\": {\n    \"surface-1\": {\n      \"surfaceSessionID\": \"surface-1\",\n      \"productMode\": \"normal\"\n    }\n  }\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write legacy surface resume state: %v", err)
	}

	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load legacy store: %v", err)
	}
	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("expected legacy surface resume entry after reload")
	}
	if entry.Verbosity != "normal" {
		t.Fatalf("expected missing verbosity to normalize to normal, got %#v", entry)
	}
}

func TestSurfaceResumeStoreDefaultsLegacyMissingBackendToCodex(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	raw := []byte("{\n  \"version\": 1,\n  \"entries\": {\n    \"surface-1\": {\n      \"surfaceSessionID\": \"surface-1\",\n      \"productMode\": \"normal\"\n    }\n  }\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write legacy surface resume state: %v", err)
	}

	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load legacy store: %v", err)
	}
	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("expected legacy surface resume entry after reload")
	}
	if entry.Backend != "codex" {
		t.Fatalf("expected missing backend to normalize to codex, got %#v", entry)
	}
}

func TestSurfaceResumeStatePersistsCodexProviderID(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeSurfaceResumeWithCodexProvider(
		"surface-1",
		"app-1",
		"chat-1",
		"user-1",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"team-proxy",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)

	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.CodexProviderID != "team-proxy" {
		t.Fatalf("expected persisted codex provider id, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	if got := restarted.service.SurfaceCodexProviderID("surface-1"); got != "team-proxy" {
		t.Fatalf("expected codex provider id restored after restart, got %q", got)
	}
}

func TestDaemonDoesNotRestoreCodexPlanModeAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "normal",
		Backend:          "codex",
		CodexProviderID:  "team-proxy",
		PlanMode:         "on",
	})

	app := newRestoreHintTestApp(stateDir)
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface after restart")
	}
	events := app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/plan off",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_plan_mode_current" {
		t.Fatalf("expected codex plan mode to restore as off, got %#v", events)
	}

	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected surface resume entry after sync")
	}
	if entry.PlanMode != "" {
		t.Fatalf("expected codex plan mode to be omitted from persisted resume state, got %#v", entry)
	}
}

func TestSurfaceResumeStoreCanonicalizesLegacyCodexBackendWithClaudeProfile(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	raw := []byte("{\n  \"version\": 1,\n  \"entries\": {\n    \"surface-1\": {\n      \"surfaceSessionID\": \"surface-1\",\n      \"productMode\": \"normal\",\n      \"backend\": \"codex\",\n      \"claudeProfileID\": \"mimo\",\n      \"resumeThreadID\": \"ca0c6c4c-4ba1-4729-b5cf-3cd7c299add1\",\n      \"resumeThreadTitle\": \"Claude 会话\",\n      \"resumeThreadCWD\": \"/data/dl/ds4debug\",\n      \"resumeWorkspaceKey\": \"/data/dl/ds4debug\",\n      \"resumeRouteMode\": \"pinned\",\n      \"resumeHeadless\": true\n    }\n  }\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write legacy surface resume state: %v", err)
	}

	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load legacy store: %v", err)
	}
	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("expected canonicalized surface resume entry after reload")
	}
	if entry.Backend != "claude" {
		t.Fatalf("expected claude profile to canonicalize backend back to claude, got %#v", entry)
	}
	if entry.ClaudeProfileID != "mimo" {
		t.Fatalf("expected claude profile id to be preserved, got %#v", entry)
	}
	if entry.ResumeThreadID != "ca0c6c4c-4ba1-4729-b5cf-3cd7c299add1" || entry.ResumeWorkspaceKey != "/data/dl/ds4debug" || !entry.ResumeHeadless {
		t.Fatalf("expected resume target to survive canonicalization, got %#v", entry)
	}
}

func TestSurfaceResumeStoreKeepsExplicitBackendEvenWhenInactiveProfileStorageExists(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	raw := []byte("{\n  \"version\": 1,\n  \"entries\": {\n    \"surface-1\": {\n      \"surfaceSessionID\": \"surface-1\",\n      \"productMode\": \"normal\",\n      \"backend\": \"codex\",\n      \"codexProviderID\": \"team-proxy\",\n      \"claudeProfileID\": \"devseek\"\n    }\n  }\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write mixed backend state: %v", err)
	}

	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load mixed backend state: %v", err)
	}
	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("expected mixed backend entry after reload")
	}
	if entry.Backend != "codex" {
		t.Fatalf("expected explicit backend to win, got %#v", entry)
	}
	if entry.CodexProviderID != "team-proxy" {
		t.Fatalf("expected active codex provider to be preserved, got %#v", entry)
	}
	if entry.ClaudeProfileID != "" {
		t.Fatalf("expected inactive claude profile projection to stay hidden, got %#v", entry)
	}
}

func TestSurfaceResumeStoreMigratesLegacyPendingFreshWorkspaceHeadlessEntry(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	raw := []byte("{\n  \"version\": 1,\n  \"entries\": {\n    \"surface-1\": {\n      \"surfaceSessionID\": \"surface-1\",\n      \"productMode\": \"normal\",\n      \"backend\": \"claude\",\n      \"resumeWorkspaceKey\": \"/data/dl/repo\",\n      \"resumeRouteMode\": \"pinned\",\n      \"resumeHeadless\": true\n    }\n  }\n}\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write legacy pending workspace state: %v", err)
	}

	store, err := surfaceresume.LoadStore(path)
	if err != nil {
		t.Fatalf("load migrated store: %v", err)
	}
	entry, ok := store.Get("surface-1")
	if !ok {
		t.Fatal("expected migrated entry after reload")
	}
	if entry.ResumeHeadless {
		t.Fatalf("expected legacy pending workspace headless flag to clear, got %#v", entry)
	}
	if entry.ResumeRouteMode != "new_thread_ready" || entry.ResumeWorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected legacy pending workspace entry to migrate into prepared workspace route, got %#v", entry)
	}
}

func TestDaemonHeadlessAttachPersistsResumeMetadataIntoSurfaceResumeState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedHeadlessInstance(app, "inst-headless-1", "thread-1")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-1",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected surface resume entry after headless attach")
	}
	if entry.ResumeInstanceID != "inst-headless-1" || entry.ResumeThreadID != "thread-1" {
		t.Fatalf("unexpected persisted headless resume target: %#v", entry)
	}
	if entry.ResumeThreadTitle != "修复登录流程" || entry.ResumeThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected persisted headless thread metadata, got %#v", entry)
	}
	if !entry.ResumeHeadless {
		t.Fatalf("expected persisted resume entry to mark headless recovery, got %#v", entry)
	}
}

func TestDaemonPendingFreshWorkspacePersistsPreparedWorkspaceResumeState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-codex",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendCodex,
		Online:        true,
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected surface resume state after pending workspace switch")
	}
	if entry.Backend != "claude" || entry.ResumeWorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected pending workspace switch to persist claude workspace target, got %#v", entry)
	}
	if entry.ResumeRouteMode != "new_thread_ready" || entry.ResumeThreadID != "" || entry.ResumeHeadless {
		t.Fatalf("expected pending workspace switch to persist prepared workspace route instead of headless restore, got %#v", entry)
	}
}

func TestDaemonPersistsSurfaceModeAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.ProductMode != "vscode" {
		t.Fatalf("expected persisted vscode surface mode, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface to materialize from resume state")
	}
	if snapshot.ProductMode != "vscode" {
		t.Fatalf("expected vscode mode after restart, got %#v", snapshot)
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected restarted surface to stay detached, got %#v", snapshot)
	}
	if restarted.service.SurfaceGatewayID("surface-1") != "app-1" || restarted.service.SurfaceChatID("surface-1") != "chat-1" || restarted.service.SurfaceActorUserID("surface-1") != "user-1" {
		t.Fatalf("unexpected restored routing: gateway=%q chat=%q actor=%q", restarted.service.SurfaceGatewayID("surface-1"), restarted.service.SurfaceChatID("surface-1"), restarted.service.SurfaceActorUserID("surface-1"))
	}
}

func TestDaemonPersistsSurfaceBackendAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.ProductMode != "normal" || entry.Backend != "claude" {
		t.Fatalf("expected persisted claude surface mode, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface to materialize from resume state")
	}
	if snapshot.ProductMode != "normal" || snapshot.Backend != agentproto.BackendClaude {
		t.Fatalf("expected claude backend after restart, got %#v", snapshot)
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected restarted surface to stay detached, got %#v", snapshot)
	}
}

func TestDaemonPersistsSurfaceClaudeProfileAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)

	app.service.MaterializeSurfaceResume(
		"surface-1",
		"app-1",
		"chat-1",
		"user-1",
		state.ProductModeNormal,
		agentproto.BackendClaude,
		"devseek",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	app.syncSurfaceResumeStateLocked(nil)

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.ClaudeProfileID != "devseek" {
		t.Fatalf("expected persisted claude profile id, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	if got := restarted.service.SurfaceClaudeProfileID("surface-1"); got != "devseek" {
		t.Fatalf("expected claude profile id restored after restart, got %q", got)
	}
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "normal" || snapshot.Backend != agentproto.BackendClaude {
		t.Fatalf("expected latent claude surface after restart, got %#v", snapshot)
	}
}

func TestDaemonPersistsSurfaceVerbosityAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionVerboseCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/verbose quiet",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.Verbosity != "quiet" {
		t.Fatalf("expected persisted quiet verbosity, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	events := restarted.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/verbose",
	})
	if len(events) != 1 {
		t.Fatalf("expected one verbose config event after restart, got %#v", events)
	}
	catalog := catalogFromUIEvent(t, events[0])
	if catalog.CommandID != control.FeishuCommandVerbose {
		t.Fatalf("expected verbose config catalog after restart, got %#v", catalog)
	}
	if summary := catalogSummaryText(catalog); !strings.Contains(summary, "quiet") {
		t.Fatalf("expected quiet verbosity after restart, got summary %q", summary)
	}
}

func TestDaemonMaterializesLatentSurfaceFromSurfaceResumeStateOnRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		Verbosity:          "verbose",
		ResumeInstanceID:   "inst-visible-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
	})

	app := newRestoreHintTestApp(stateDir)
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface to materialize from resume state")
	}
	if snapshot.ProductMode != "normal" {
		t.Fatalf("expected normal mode after restart, got %#v", snapshot)
	}
	events := app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/verbose",
	})
	if len(events) != 1 {
		t.Fatalf("expected one verbose config event after resume materialization, got %#v", events)
	}
	catalog := catalogFromUIEvent(t, events[0])
	if catalog.CommandID != control.FeishuCommandVerbose {
		t.Fatalf("expected verbose config catalog after restart materialization, got %#v", catalog)
	}
	if summary := catalogSummaryText(catalog); !strings.Contains(summary, "verbose") {
		t.Fatalf("expected verbose setting after restart materialization, got summary %q", summary)
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected restored surface to stay detached, got %#v", snapshot)
	}

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected resume entry after startup sync")
	}
	if entry.Verbosity != "verbose" || entry.ResumeInstanceID != "inst-visible-1" || entry.ResumeThreadID != "thread-1" || entry.ResumeWorkspaceKey != "/data/dl/droid" || entry.ResumeRouteMode != "pinned" {
		t.Fatalf("expected startup materialization to preserve stored resume target, got %#v", entry)
	}
}

func TestDaemonAttachedVSCodeSurfacePersistsResumeTargetAndRecoversOnReconnect(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedVSCodeResumeInstance(app, "inst-vscode-1", "thread-1")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode-1",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected persisted surface resume entry")
	}
	if entry.ProductMode != "vscode" || entry.ResumeInstanceID != "inst-vscode-1" || entry.ResumeThreadID != "thread-1" || entry.ResumeRouteMode != "follow_local" {
		t.Fatalf("unexpected persisted vscode resume target: %#v", entry)
	}

	gateway := newLifecycleGateway()
	restarted := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	restarted.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	restarted.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface after restart")
	}
	if snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected detached vscode snapshot after restart, got %#v", snapshot)
	}
	reloaded := restarted.SurfaceResumeState("surface-1")
	if reloaded == nil || !surfaceresume.SameEntryContent(*entry, *reloaded) {
		t.Fatalf("expected restart to preserve stored resume target: want=%#v got=%#v", entry, reloaded)
	}

	restarted.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})

	waitForDaemonCondition(t, 2*time.Second, func() bool {
		snapshot = restarted.service.SurfaceSnapshot("surface-1")
		return snapshot != nil && snapshot.Attachment.InstanceID == "inst-vscode-1"
	})
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "inst-vscode-1" {
		t.Fatalf("expected vscode resume to reattach target instance, got %#v", snapshot)
	}
	if snapshot.Attachment.SelectedThreadID != "" || snapshot.Attachment.RouteMode != "follow_local" {
		t.Fatalf("expected vscode resume to re-enter follow waiting, got %#v", snapshot)
	}
	waitForDaemonCondition(t, 2*time.Second, func() bool {
		for _, operation := range gateway.snapshotOperations() {
			text := operationCardText(operation)
			if strings.Contains(text, "已恢复到 VS Code 实例") && strings.Contains(text, "再说一句话") {
				return true
			}
		}
		return false
	})
}

func TestDaemonVSCodeResumeWaitsForExactInstanceAndNeverUsesHeadless(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "vscode",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "follow_local",
	})

	gateway := newLifecycleGateway()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	headlessStarted := false
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		headlessStarted = true
		return 1, nil
	}

	app.onTick(context.Background(), time.Now().UTC())
	if headlessStarted {
		t.Fatal("expected vscode resume path to avoid starting headless")
	}
	waitForDaemonCondition(t, 2*time.Second, func() bool {
		ops := gateway.snapshotOperations()
		return len(ops) == 1 && strings.Contains(operationCardText(ops[0]), "请先打开 VS Code 中的 Codex")
	})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-other-1",
			DisplayName:   "other",
			WorkspaceRoot: "/data/dl/other",
			WorkspaceKey:  "/data/dl/other",
			ShortName:     "other",
			Source:        "vscode",
		},
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected vscode resume to wait for exact instance, got %#v", snapshot)
	}
	if len(gateway.snapshotOperations()) != 1 {
		t.Fatalf("expected open VS Code guidance to stay one-shot before exact instance reconnects, got %#v", gateway.snapshotOperations())
	}
	if headlessStarted {
		t.Fatal("expected unrelated instance hello to keep headless disabled")
	}
}

func TestDaemonDetachedVSCodeModePromptsOpenVSCodeAfterRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
	})

	gateway := newLifecycleGateway()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	app.onTick(context.Background(), time.Now().UTC())

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected detached vscode surface after restart, got %#v", snapshot)
	}
	waitForDaemonCondition(t, 2*time.Second, func() bool {
		ops := gateway.snapshotOperations()
		return len(ops) == 1 && strings.Contains(operationCardText(ops[0]), "请先打开 VS Code 中的 Codex")
	})

	app.onTick(context.Background(), time.Now().UTC().Add(time.Second))
	if len(gateway.snapshotOperations()) != 1 {
		t.Fatalf("expected detached vscode guidance to stay one-shot, got %#v", gateway.snapshotOperations())
	}
}

func TestDaemonVSCodeResumeOpenPromptPatchesIntoSameCardOnExactReconnect(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
		ResumeInstanceID: "inst-vscode-1",
	})

	gateway := newLifecycleGateway()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	app.onTick(context.Background(), time.Now().UTC())
	initial := waitForLifecycleOperationTitle(t, gateway, "请先打开 VS Code")

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})

	waitForDaemonCondition(t, 2*time.Second, func() bool {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationUpdateCard &&
				op.MessageID == initial.MessageID &&
				strings.Contains(operationCardText(op), "已恢复到 VS Code 实例") {
				return true
			}
		}
		return false
	})
}

func TestDaemonTickDoesNotRewriteSurfaceResumeStateWithoutStateChange(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	path := surfaceresume.StatePath(stateDir)
	base := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, base, base); err != nil {
		t.Fatalf("Chtimes(surface resume state): %v", err)
	}

	app.onTick(context.Background(), base.Add(time.Second))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(surface resume state): %v", err)
	}
	if !info.ModTime().Equal(base) {
		t.Fatalf("expected idle tick to avoid rewriting surface resume state, modtime=%s want=%s", info.ModTime(), base)
	}
}

func TestDaemonHeadlessRecoveryTickPersistsUpdatedResumeStateForRecoveredSurface(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    "/data/dl/droid",
		ResumeWorkspaceKey: filepath.Join("/data/dl/.local/state", "codex-remote"),
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})

	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-pool",
			DisplayName:   "headless",
			WorkspaceRoot: filepath.Join("/data/dl/.local/state", "codex-remote"),
			WorkspaceKey:  filepath.Join("/data/dl/.local/state", "codex-remote"),
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})
	app.onEvents(context.Background(), "inst-headless-pool", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	}})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected surface resume entry after headless recovery attempt")
	}
	if !entry.ResumeHeadless || entry.ResumeInstanceID != "inst-headless-pool" || entry.ResumeThreadID != "thread-1" || entry.ResumeThreadCWD != "/data/dl/droid" || entry.ResumeWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected persisted headless resume metadata after targeted sync, got %#v", entry)
	}
	if entry.ResumeThreadTitle != "修复登录流程" {
		t.Fatalf("expected persisted thread title to keep recovered thread context, got %#v", entry)
	}
}

func TestDaemonRestartRecoversPreparedWorkspaceResumeState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		Backend:            "claude",
		ResumeWorkspaceKey: "/data/dl/repo",
		ResumeRouteMode:    "new_thread_ready",
	})

	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}
	recovery := app.surfaceResumeRuntime.recovery["surface-1"]
	if recovery == nil || recovery.Entry.ResumeWorkspaceKey != "/data/dl/repo" || recovery.Entry.ResumeRouteMode != "new_thread_ready" || recovery.Entry.ResumeHeadless {
		t.Fatalf("expected startup recovery state to keep prepared workspace semantics, got %#v", recovery)
	}
	recoveryEvents, result := app.service.TryAutoResumeHeadlessSurface("surface-1", orchestrator.SurfaceResumeAttempt{
		WorkspaceKey:     recovery.Entry.ResumeWorkspaceKey,
		Backend:          agentproto.Backend(recovery.Entry.Backend),
		PrepareNewThread: recovery.Entry.ResumeRouteMode == "new_thread_ready",
	}, true)
	if result.Status != orchestrator.SurfaceResumeStatusStarting {
		t.Fatalf("expected prepared workspace recovery to start pending workspace launch, got result=%#v events=%#v", result, recoveryEvents)
	}
	if len(recoveryEvents) == 0 || recoveryEvents[len(recoveryEvents)-1].DaemonCommand == nil || recoveryEvents[len(recoveryEvents)-1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected prepared workspace recovery to emit start headless command, got %#v", recoveryEvents)
	}
	app.mu.Lock()
	app.handleUIEventsLocked(context.Background(), recoveryEvents)
	app.mu.Unlock()

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID == "" {
		t.Fatalf("expected prepared workspace resume to restart pending workspace launch, got %#v", snapshot)
	}
	if snapshot.PendingHeadless.ThreadCWD != "/data/dl/repo" {
		t.Fatalf("expected prepared workspace resume to preserve new-thread-ready intent, got %#v", snapshot.PendingHeadless)
	}
	if snapshot.ProductMode != "normal" || snapshot.Backend != agentproto.BackendClaude {
		t.Fatalf("expected prepared workspace resume to keep normal claude mode, got %#v", snapshot)
	}
	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.ResumeRouteMode != "new_thread_ready" || entry.ResumeHeadless || entry.ResumeWorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected persisted prepared workspace state to stay workspace-owned during restart recovery, got %#v", entry)
	}
}

func TestDaemonNormalResumePrefersVisibleThreadOverHeadlessFallback(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    "/data/dl/droid",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})
	app.onEvents(context.Background(), "inst-vscode-1", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-1",
			Name:     "修复登录流程",
			CWD:      "/data/dl/droid",
			Loaded:   true,
		}},
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-vscode-1" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected headless resume to reattach visible thread, got %#v", snapshot)
	}
	if snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected visible resume to avoid headless fallback, got %#v", snapshot)
	}
	if len(gateway.operations) == 0 || !strings.Contains(gateway.operations[len(gateway.operations)-1].CardBody, "已恢复到之前会话") {
		t.Fatalf("expected recovery notice after visible resume, got %#v", gateway.operations)
	}
}

func TestDaemonHeadlessResumeFallsBackToWorkspace(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-missing",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
	})

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})
	app.onEvents(context.Background(), "inst-vscode-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "normal" || snapshot.Attachment.InstanceID != "inst-vscode-1" {
		t.Fatalf("expected workspace fallback to attach workspace instance, got %#v", snapshot)
	}
	if snapshot.Attachment.SelectedThreadID != "" || snapshot.Attachment.RouteMode != "unbound" {
		t.Fatalf("expected workspace fallback to stay unbound, got %#v", snapshot)
	}
	var sawFallbackNotice bool
	for _, op := range gateway.operations {
		if strings.Contains(op.CardBody, "已先回到工作区") {
			sawFallbackNotice = true
			break
		}
	}
	if !sawFallbackNotice {
		t.Fatalf("expected workspace fallback notice, got %#v", gateway.operations)
	}
}

func TestDaemonNormalResumeHeadlessTargetSkipsWorkspaceFallback(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    "/data/dl/droid",
		ResumeWorkspaceKey: filepath.Join("/data/dl/.local/state", "codex-remote"),
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-pool",
			DisplayName:   "headless",
			WorkspaceRoot: filepath.Join("/data/dl/.local/state", "codex-remote"),
			WorkspaceKey:  filepath.Join("/data/dl/.local/state", "codex-remote"),
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})
	app.onEvents(context.Background(), "inst-headless-pool", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface snapshot after recovery attempt")
	}
	if snapshot.Attachment.InstanceID != "" {
		if snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.Attachment.RouteMode != "pinned" {
			t.Fatalf("expected headless resume target to reattach a concrete thread instead of workspace fallback, got %#v", snapshot)
		}
		if snapshot.WorkspaceKey == filepath.Join("/data/dl/.local/state", "codex-remote") {
			t.Fatalf("expected headless resume to avoid state-dir workspace fallback, got %#v", snapshot)
		}
	} else if snapshot.PendingHeadless.InstanceID == "" || snapshot.PendingHeadless.ThreadID != "thread-1" || snapshot.PendingHeadless.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected headless recovery to start directly from persisted resume target, got %#v", snapshot)
	}
	for _, operation := range gateway.operations {
		if strings.Contains(operation.CardBody, "已先回到工作区") {
			t.Fatalf("expected ResumeHeadless target to skip workspace fallback notice, got %#v", gateway.operations)
		}
	}
}

func TestDaemonNormalResumeFailureEmitsNoticeAfterFirstRefresh(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-missing",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
	})

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-other-1",
			DisplayName:   "other",
			WorkspaceRoot: "/data/dl/other",
			WorkspaceKey:  "/data/dl/other",
			ShortName:     "other",
			Source:        "vscode",
		},
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no failure notice before first refresh completes, got %#v", gateway.operations)
	}

	app.onEvents(context.Background(), "inst-other-1", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-other",
			Name:     "其他会话",
			CWD:      "/data/dl/other",
			Loaded:   true,
		}},
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected failed resume surface to remain detached, got %#v", snapshot)
	}
	if len(gateway.operations) != 2 {
		t.Fatalf("expected workspace prepare start + failure notices after first refresh, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardBody, "接入为可用工作区") {
		t.Fatalf("expected workspace prepare starting notice first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].CardTitle != "工作区准备失败" {
		t.Fatalf("expected workspace prepare failure notice second, got %#v", gateway.operations[1])
	}
}

func seedVSCodeResumeInstance(app *App, instanceID, threadID string) {
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              instanceID,
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: threadID,
		Threads: map[string]*state.ThreadRecord{
			threadID: {ThreadID: threadID, Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
}

func putSurfaceResumeStateForTest(t *testing.T, stateDir string, entry surfaceresume.Entry) {
	t.Helper()
	store, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load surface resume store: %v", err)
	}
	if err := store.Put(entry); err != nil {
		t.Fatalf("put surface resume entry: %v", err)
	}
}

func writeSurfaceResumeStateForTest(t *testing.T, stateDir string, persisted surfaceresume.StateFile) {
	t.Helper()
	path := surfaceresume.StatePath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		t.Fatalf("marshal surface resume state: %v", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write surface resume state: %v", err)
	}
}
