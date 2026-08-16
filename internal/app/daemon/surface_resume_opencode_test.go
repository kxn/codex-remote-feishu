package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

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
