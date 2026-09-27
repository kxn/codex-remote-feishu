package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSurfaceResumeStateRoundTripsCodexPromptOverridesPerTopic(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	for _, surfaceID := range []string{"topic-a", "topic-b"} {
		app.service.MaterializeSurfaceResumeWithCodexProfile(
			surfaceID,
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
	}
	app.service.Surface("topic-a").CodexPromptOverride = state.CodexPromptOverrideRecord{
		Model:           " gpt-5.6-terra ",
		ReasoningEffort: " HIGH ",
	}

	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entryA := app.SurfaceResumeState("topic-a")
	entryB := app.SurfaceResumeState("topic-b")
	if entryA == nil || entryA.CodexModelOverride != "gpt-5.6-terra" || entryA.CodexReasoningEffortOverride != "high" {
		t.Fatalf("expected normalized topic-a override in persisted state, got %#v", entryA)
	}
	if entryB == nil || entryB.CodexModelOverride != "" || entryB.CodexReasoningEffortOverride != "" {
		t.Fatalf("expected topic-b to keep empty override, got %#v", entryB)
	}

	restarted := newRestoreHintTestApp(stateDir)
	if got := restarted.service.Surface("topic-a").CodexPromptOverride; got != (state.CodexPromptOverrideRecord{Model: "gpt-5.6-terra", ReasoningEffort: "high"}) {
		t.Fatalf("expected topic-a override restored after restart, got %#v", got)
	}
	if got := restarted.service.Surface("topic-b").CodexPromptOverride; got != (state.CodexPromptOverrideRecord{}) {
		t.Fatalf("expected topic-b override to stay empty after restart, got %#v", got)
	}
}

func TestSurfaceResumeStoreLoadsLegacyStateWithoutCodexPromptOverrides(t *testing.T) {
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
	if !ok || entry.CodexModelOverride != "" || entry.CodexReasoningEffortOverride != "" {
		t.Fatalf("expected legacy state to load with empty codex overrides, got %#v", entry)
	}
}

func TestSurfaceResumeStateRetainsDormantCodexPromptOverrideAcrossBackendSwitch(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:             "surface-1",
		GatewayID:                    "app-1",
		ChatID:                       "chat-1",
		ActorUserID:                  "user-1",
		ProductMode:                  string(state.ProductModeNormal),
		Backend:                      string(agentproto.BackendClaude),
		ClaudeProfileID:              "default",
		CodexModelOverride:           " gpt-5.6-terra ",
		CodexReasoningEffortOverride: " HIGH ",
	})

	app := newRestoreHintTestApp(stateDir)
	if got := app.service.Surface("surface-1").CodexPromptOverride; got != (state.CodexPromptOverrideRecord{Model: "gpt-5.6-terra", ReasoningEffort: "high"}) {
		t.Fatalf("expected dormant codex override to materialize on claude surface, got %#v", got)
	}

	app.service.MaterializeSurfaceResumeWithCodexProfile(
		"surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal,
		agentproto.BackendCodex, "team-proxy", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	seedHeadlessInstance(app, "inst-1", "thread-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-1"
	surface.SelectedThreadID = "thread-1"
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.NextPrompt.OverrideModel != "gpt-5.6-terra" || snapshot.NextPrompt.OverrideReasoningEffort != "high" {
		t.Fatalf("expected dormant override to reactivate for codex prompts, got %#v", snapshot)
	}
}

func TestSurfaceResumeP2PDedupesCodexPromptOverridesDeterministicallyOnTie(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	for range 100 {
		entries, _ := surfaceresume.CanonicalizeEntries(map[string]surfaceresume.Entry{
			"feishu:app-1:user:legacy": {
				SurfaceSessionID: "feishu:app-1:user:legacy", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "legacy",
				ProductMode: string(state.ProductModeNormal), Backend: string(agentproto.BackendCodex),
				CodexModelOverride: "gpt-5.6-terra", CodexReasoningEffortOverride: "high", UpdatedAt: updatedAt,
			},
			"feishu:app-1:user:ou_user": {
				SurfaceSessionID: "feishu:app-1:user:ou_user", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "ou_user",
				ProductMode: string(state.ProductModeNormal), Backend: string(agentproto.BackendCodex),
				CodexModelOverride: "gpt-5.6-luna", CodexReasoningEffortOverride: "medium", UpdatedAt: updatedAt,
			},
		})
		entry := entries["feishu:app-1:user:ou_user"]
		if entry.CodexModelOverride != "gpt-5.6-terra" || entry.CodexReasoningEffortOverride != "high" {
			t.Fatalf("expected surface id tie-break to select Terra/high, got %#v", entry)
		}
	}
}

func TestSurfaceResumeOpenCodeProfileContractControlsExactThreadFallback(t *testing.T) {
	tests := []struct {
		name             string
		currentProfileID string
		currentRevision  uint64
		wantReset        bool
	}{
		{name: "same profile and admission", wantReset: false},
		{name: "profile id changed", currentProfileID: "op_new", currentRevision: 2, wantReset: true},
		{name: "profile revision changed", currentProfileID: "op_old", currentRevision: 2, wantReset: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			surfaceID := "feishu:app-1:user:user-1"
			workspaceDir := evalSymlinkForTest(t, t.TempDir())
			putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
				SurfaceSessionID:  surfaceID,
				GatewayID:         "app-1",
				ChatID:            "chat-1",
				ActorUserID:       "user-1",
				ProductMode:       string(state.ProductModeNormal),
				Backend:           string(agentproto.BackendOpenCode),
				OpenCodeProfileID: "op_old",
				OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{
					ProfileRef: state.OpenCodeProfileRef{ID: "op_old", Revision: 1},
				},
				ResumeInstanceID:   "inst-old",
				ResumeThreadID:     "ses-old",
				ResumeThreadTitle:  "旧 OpenCode 会话",
				ResumeThreadCWD:    workspaceDir,
				ResumeWorkspaceKey: workspaceDir,
				ResumeRouteMode:    string(state.RouteModePinned),
				ResumeHeadless:     true,
			})

			if tt.currentProfileID != "" {
				store, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
				if err != nil {
					t.Fatalf("load bot capability store: %v", err)
				}
				if err := store.Put(state.BotCapabilitySettingsRecord{
					GatewayID:         "app-1",
					ProductMode:       state.ProductModeNormal,
					Backend:           agentproto.BackendOpenCode,
					OpenCodeProfileID: tt.currentProfileID,
					OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{
						ProfileRef: state.OpenCodeProfileRef{ID: tt.currentProfileID, Revision: tt.currentRevision},
					},
				}); err != nil {
					t.Fatalf("persist current bot capability: %v", err)
				}
			}

			app := newRestoreHintTestApp(stateDir)
			entry := app.SurfaceResumeState(surfaceID)
			if entry == nil {
				t.Fatal("expected surface resume entry")
			}
			if !tt.wantReset {
				if entry.ResumeThreadID != "ses-old" || entry.ResumeInstanceID != "inst-old" || entry.ResumeRouteMode != string(state.RouteModePinned) || !entry.ResumeHeadless {
					t.Fatalf("same OpenCode contract must preserve exact resume target, got %#v", entry)
				}
				return
			}

			if entry.OpenCodeProfileID != tt.currentProfileID || entry.OpenCodeAdmissionRef == nil ||
				entry.OpenCodeAdmissionRef.ProfileRef.ID != tt.currentProfileID ||
				entry.OpenCodeAdmissionRef.ProfileRef.Revision != tt.currentRevision {
				t.Fatalf("expected current OpenCode contract after materialization, got %#v", entry)
			}
			if entry.ResumeInstanceID != "" || entry.ResumeThreadID != "" || entry.ResumeThreadTitle != "" || entry.ResumeThreadCWD != "" || entry.ResumeHeadless {
				t.Fatalf("profile mismatch must clear exact resume target, got %#v", entry)
			}
			if tempDirSuffixForTest(t, entry.ResumeWorkspaceKey) != tempDirSuffixForTest(t, workspaceDir) || entry.ResumeRouteMode != string(state.RouteModeNewThreadReady) {
				t.Fatalf("profile mismatch must retain workspace new-thread route, got %#v", entry)
			}

			recovery := app.surfaceResumeRuntime.recovery[surfaceID]
			if recovery == nil || recovery.Entry.ResumeThreadID != "" || recovery.Entry.ResumeRouteMode != string(state.RouteModeNewThreadReady) {
				t.Fatalf("startup recovery retained stale exact-thread target, got %#v", recovery)
			}
			events, result := app.service.TryAutoResumeHeadlessSurface(surfaceID, orchestrator.SurfaceResumeAttempt{
				WorkspaceKey:     recovery.Entry.ResumeWorkspaceKey,
				Backend:          agentproto.BackendOpenCode,
				PrepareNewThread: true,
			}, true)
			if result.Status != orchestrator.SurfaceResumeStatusStarting {
				t.Fatalf("expected fresh OpenCode workspace start, got result=%#v events=%#v", result, events)
			}
			if len(events) == 0 || events[len(events)-1].DaemonCommand == nil || events[len(events)-1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
				t.Fatalf("expected fresh OpenCode start command, got %#v", events)
			}
			command := events[len(events)-1].DaemonCommand
			if command.ThreadID != "" || command.OpenCodeProfileID != tt.currentProfileID || command.OpenCodeAdmissionRef == nil ||
				command.OpenCodeAdmissionRef.ProfileRef.Revision != tt.currentRevision {
				t.Fatalf("fresh OpenCode command reused stale target or contract, got %#v", command)
			}
		})
	}
}

func TestSurfaceResumeStatePersistsOpenCodeProfileID(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeSurfaceResumeContract(
		"surface-1",
		"app-1",
		"chat-1",
		"user-1",
		state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)

	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.Backend != string(agentproto.BackendOpenCode) || entry.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected persisted opencode profile id, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	if got := restarted.service.SurfaceOpenCodeProfileID("surface-1"); got != "op_team" {
		t.Fatalf("expected opencode profile id restored after restart, got %q", got)
	}
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "normal" || snapshot.Backend != agentproto.BackendOpenCode {
		t.Fatalf("expected latent opencode surface after restart, got %#v", snapshot)
	}
}

func TestSurfaceResumeStatePersistsOpenCodeProfileAndAdmissionRef(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeSurfaceResumeWithOpenCodeProfile(
		"surface-1",
		"app-1",
		"chat-1",
		"user-1",
		state.ProductModeNormal,
		agentproto.BackendOpenCode,
		"op_team",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	app.mu.Lock()
	surface := app.service.Surface("surface-1")
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{
		ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7},
	}
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.OpenCodeProfileID != "op_team" || entry.OpenCodeAdmissionRef == nil ||
		entry.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected persisted opencode profile and admission ref, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	restored := restarted.service.Surface("surface-1")
	if restored == nil || restored.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected opencode profile restored after restart, got %#v", restored)
	}
	if restored.OpenCodeAdmissionRef == nil || restored.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected opencode admission ref restored after restart, got %#v", restored.OpenCodeAdmissionRef)
	}
}

func TestSurfaceResumeStatePersistsSessionAccessAndPlan(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeSurfaceResumeWithOpenCodeProfile(
		"surface-1",
		"app-1",
		"chat-1",
		"user-1",
		state.ProductModeNormal,
		agentproto.BackendOpenCode,
		"op_team",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	app.mu.Lock()
	surface := app.service.Surface("surface-1")
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
	surface.PlanMode = state.PlanModeSettingOn
	surface.PlanModeOverrideSet = true
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.AccessMode != agentproto.AccessModeConfirm ||
		entry.PlanMode != string(state.PlanModeSettingOn) || !entry.PlanModeOverrideSet {
		t.Fatalf("expected session access/plan persisted, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	restored := restarted.service.Surface("surface-1")
	if restored == nil || restored.PromptOverride.AccessMode != agentproto.AccessModeConfirm ||
		restored.PlanMode != state.PlanModeSettingOn || !restored.PlanModeOverrideSet {
		t.Fatalf("expected session access/plan restored after restart, got %#v", restored)
	}
}

func TestSurfaceResumeSeedSeedsLegacyBotAccessAndPlan(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
		GatewayID:           "app-1",
		ProductMode:         state.ProductModeNormal,
		Backend:             agentproto.BackendCodex,
		CodexProfileID:      "default",
		PromptOverride:      state.ModelConfigRecord{AccessMode: agentproto.AccessModeConfirm},
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}})

	entry := surfaceresume.Entry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ProductMode:      "normal",
		Backend:          "codex",
		CodexProfileID:   "default",
	}
	if !app.seedSurfaceSessionSettingsFromBotRecordsLocked(&entry) {
		t.Fatal("expected legacy bot access/plan to seed session entry")
	}
	if entry.AccessMode != agentproto.AccessModeConfirm ||
		entry.PlanMode != string(state.PlanModeSettingOn) || !entry.PlanModeOverrideSet {
		t.Fatalf("expected legacy bot access/plan seeded into entry, got %#v", entry)
	}

	// 二次调用不应重复写入（entry 已有值）。
	if app.seedSurfaceSessionSettingsFromBotRecordsLocked(&entry) {
		t.Fatalf("seed must be idempotent, got %#v", entry)
	}
}
