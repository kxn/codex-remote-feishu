package state

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestNormalizeBotCapabilitySettingsRecord(t *testing.T) {
	updatedAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	record, ok := NormalizeBotCapabilitySettingsRecord(BotCapabilitySettingsRecord{
		GatewayID:       " app-1 ",
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		CodexProfileID:  " team-proxy ",
		ClaudeProfileID: " devseek ",
		PromptOverride: ModelConfigRecord{
			Model:           " gpt-5.5 ",
			ReasoningEffort: " HIGH ",
			AccessMode:      " confirm ",
		},
		PlanMode:            PlanModeSettingOn,
		PlanModeOverrideSet: true,
		UpdatedBy:           " user-1 ",
		UpdatedAt:           updatedAt,
	})
	if !ok {
		t.Fatalf("expected normalized record")
	}
	if record.GatewayID != "app-1" {
		t.Fatalf("GatewayID = %q, want app-1", record.GatewayID)
	}
	if record.ProductMode != ProductModeNormal || record.Backend != agentproto.BackendClaude {
		t.Fatalf("contract = %s/%s, want normal/claude", record.ProductMode, record.Backend)
	}
	if record.CodexProfileID != "team-proxy" || record.ClaudeProfileID != "devseek" {
		t.Fatalf("profiles = %q/%q, want team-proxy/devseek", record.CodexProfileID, record.ClaudeProfileID)
	}
	if record.PromptOverride.Model != "gpt-5.5" || record.PromptOverride.ReasoningEffort != "high" || record.PromptOverride.AccessMode != "confirm" {
		t.Fatalf("PromptOverride = %#v, want compact normalized values", record.PromptOverride)
	}
	if record.PlanMode != PlanModeSettingOn || !record.PlanModeOverrideSet {
		t.Fatalf("plan override = %s/%v, want on/true", record.PlanMode, record.PlanModeOverrideSet)
	}
	if record.UpdatedBy != "user-1" {
		t.Fatalf("UpdatedBy = %q, want user-1", record.UpdatedBy)
	}
	if record.UpdatedAt.Location() != time.UTC {
		t.Fatalf("UpdatedAt location = %v, want UTC", record.UpdatedAt.Location())
	}
}

func TestBotCapabilitySettingsRecordWritesOnlyCodexProfileSelection(t *testing.T) {
	record, ok := NormalizeBotCapabilitySettingsRecord(BotCapabilitySettingsRecord{
		GatewayID:      "app-1",
		ProductMode:    ProductModeNormal,
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "team-proxy",
	})
	if !ok {
		t.Fatal("expected record to normalize")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(raw)
	if strings.Contains(payload, "CodexProviderID") || strings.Contains(payload, "codexProviderID") {
		t.Fatalf("bot capability settings wrote legacy provider field: %s", payload)
	}
	if !strings.Contains(payload, "CodexProfileID") {
		t.Fatalf("bot capability settings did not write canonical profile field: %s", payload)
	}
}

func TestNormalizeBotCapabilitySettingsRecordCarriesOpenCodeProfile(t *testing.T) {
	record, ok := NormalizeBotCapabilitySettingsRecord(BotCapabilitySettingsRecord{
		GatewayID:         " app-1 ",
		ProductMode:       ProductModeNormal,
		Backend:           agentproto.BackendOpenCode,
		CodexProfileID:    " team-proxy ",
		ClaudeProfileID:   " devseek ",
		OpenCodeProfileID: " op_team ",
	})
	if !ok {
		t.Fatalf("expected normalized record")
	}
	if record.Backend != agentproto.BackendOpenCode || record.OpenCodeProfileID != "op_team" {
		t.Fatalf("opencode settings normalized to %#v, want backend opencode profile op_team", record)
	}
	contract := BotCapabilitySettingsContract(record)
	if contract.Backend != agentproto.BackendOpenCode || contract.OpenCodeProfileID != "op_team" {
		t.Fatalf("opencode bot capability contract = %#v, want opencode/op_team", contract)
	}
	if contract.CodexProfileID != "" || contract.ClaudeProfileID != "" {
		t.Fatalf("opencode bot capability contract retained inactive profile fields: %#v", contract)
	}
}

func TestNormalizeBotCapabilitySettingsRecordKeepsOpenCodeRuntimeReasoningAccessAndPlan(t *testing.T) {
	record, ok := NormalizeBotCapabilitySettingsRecord(BotCapabilitySettingsRecord{
		GatewayID:         "app-1",
		ProductMode:       ProductModeNormal,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		PromptOverride: ModelConfigRecord{
			Model:           " gpt-5.5 ",
			ReasoningEffort: " high ",
			AccessMode:      agentproto.AccessModeConfirm,
		},
		PlanMode:            PlanModeSettingOn,
		PlanModeOverrideSet: true,
	})
	if !ok {
		t.Fatal("expected normalized record")
	}
	if record.PromptOverride != (ModelConfigRecord{ReasoningEffort: "high", AccessMode: agentproto.AccessModeConfirm}) {
		t.Fatalf("opencode prompt override = %#v, want runtime reasoning/access only", record.PromptOverride)
	}
	if record.PlanMode != PlanModeSettingOn || !record.PlanModeOverrideSet {
		t.Fatalf("opencode plan override = %s/%v, want on/true", record.PlanMode, record.PlanModeOverrideSet)
	}
	if record.Backend != agentproto.BackendOpenCode || record.OpenCodeProfileID != "op_team" {
		t.Fatalf("opencode contract fields changed unexpectedly: %#v", record)
	}
}

func TestBotCapabilitySettingsKeyRequiresGateway(t *testing.T) {
	if key := BotCapabilitySettingsKey(" app-1 "); key != "feishu:gateway:app-1" {
		t.Fatalf("key = %q, want feishu:gateway:app-1", key)
	}
	if _, ok := NormalizeBotCapabilitySettingsRecord(BotCapabilitySettingsRecord{}); ok {
		t.Fatalf("expected empty gateway record to be rejected")
	}
}

func TestEffectiveSurfaceCapabilitySettingsUsesBotRecordForFeishuRoom(t *testing.T) {
	root := NewRoot()
	root.BotCapabilitySettings["feishu:gateway:app-1"] = BotCapabilitySettingsRecord{
		GatewayID:           "app-1",
		ProductMode:         ProductModeNormal,
		Backend:             agentproto.BackendClaude,
		ClaudeProfileID:     "devseek",
		PromptOverride:      ModelConfigRecord{Model: "claude-sonnet", ReasoningEffort: "max"},
		PlanMode:            PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}
	surface := &SurfaceConsoleRecord{
		SurfaceSessionID:    "feishu:app-1:chat:oc_room",
		Platform:            "feishu",
		GatewayID:           "app-1",
		ChatID:              "oc_room",
		ProductMode:         ProductModeNormal,
		Backend:             agentproto.BackendCodex,
		CodexProfileID:      "team-proxy",
		PromptOverride:      ModelConfigRecord{Model: "gpt-5.5", AccessMode: agentproto.AccessModeConfirm},
		PlanMode:            PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}

	effective := EffectiveSurfaceCapabilitySettings(root, surface)
	if effective.Contract.Backend != agentproto.BackendClaude || effective.Contract.ClaudeProfileID != "devseek" {
		t.Fatalf("effective contract = %#v, want bot claude profile", effective.Contract)
	}
	if effective.PromptOverride.Model != "claude-sonnet" || effective.PromptOverride.ReasoningEffort != "max" {
		t.Fatalf("effective prompt = %#v, want bot prompt", effective.PromptOverride)
	}
	if effective.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("effective access = %q, want session confirm", effective.PromptOverride.AccessMode)
	}
	if effective.PlanMode != PlanModeSettingOn || !effective.PlanModeOverrideSet {
		t.Fatalf("effective plan = %s/%v, want session on/true", effective.PlanMode, effective.PlanModeOverrideSet)
	}
}

func TestEffectiveSurfaceCapabilitySettingsKeepsOpenCodeRuntimeAccessAndPlan(t *testing.T) {
	root := NewRoot()
	root.BotCapabilitySettings["feishu:gateway:app-1"] = BotCapabilitySettingsRecord{
		GatewayID:         "app-1",
		ProductMode:       ProductModeNormal,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		PromptOverride: ModelConfigRecord{
			Model:           "gpt-5.5",
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeFullAccess,
		},
		PlanMode:            PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}
	surface := &SurfaceConsoleRecord{
		SurfaceSessionID:    "feishu:app-1:user:ou_user",
		Platform:            "feishu",
		GatewayID:           "app-1",
		ChatID:              "ou_user",
		ActorUserID:         "ou_user",
		ProductMode:         ProductModeNormal,
		Backend:             agentproto.BackendCodex,
		CodexProfileID:      "team-proxy",
		PromptOverride:      ModelConfigRecord{AccessMode: agentproto.AccessModeConfirm},
		PlanMode:            PlanModeSettingOff,
		PlanModeOverrideSet: false,
	}

	effective := EffectiveSurfaceCapabilitySettings(root, surface)
	if effective.Source != SurfaceCapabilitySettingsSourceBot {
		t.Fatalf("source = %q, want bot settings", effective.Source)
	}
	if effective.Contract.Backend != agentproto.BackendOpenCode || effective.Contract.OpenCodeProfileID != "op_team" {
		t.Fatalf("effective contract = %#v, want opencode profile", effective.Contract)
	}
	if effective.PromptOverride != (ModelConfigRecord{ReasoningEffort: "high", AccessMode: agentproto.AccessModeConfirm}) {
		t.Fatalf("effective opencode prompt override = %#v, want bot reasoning + session access", effective.PromptOverride)
	}
	if effective.PlanMode != PlanModeSettingOff || effective.PlanModeOverrideSet {
		t.Fatalf("effective opencode plan = %s/%v, want session off/false", effective.PlanMode, effective.PlanModeOverrideSet)
	}
}

func TestEffectiveSurfaceCapabilitySettingsUsesBotRecordForFeishuPrivate(t *testing.T) {
	root := NewRoot()
	root.BotCapabilitySettings["feishu:gateway:app-1"] = BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	}
	surface := &SurfaceConsoleRecord{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		Platform:         "feishu",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		ProductMode:      ProductModeNormal,
		Backend:          agentproto.BackendCodex,
		CodexProfileID:   "team-proxy",
	}

	effective := EffectiveSurfaceCapabilitySettings(root, surface)
	if effective.Source != SurfaceCapabilitySettingsSourceBot {
		t.Fatalf("source = %q, want bot settings", effective.Source)
	}
	if effective.Contract.Backend != agentproto.BackendClaude || effective.Contract.ClaudeProfileID != "devseek" {
		t.Fatalf("effective contract = %#v, want bot claude profile", effective.Contract)
	}
}

func TestEffectiveSurfaceCapabilitySettingsRejectsMalformedFeishuRoomIdentity(t *testing.T) {
	root := NewRoot()
	root.BotCapabilitySettings["feishu:gateway:app-1"] = BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	}
	surface := &SurfaceConsoleRecord{
		SurfaceSessionID: "feishu:app-1:unknown:chat:oc_room",
		Platform:         "feishu",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ProductMode:      ProductModeNormal,
		Backend:          agentproto.BackendCodex,
		CodexProfileID:   "team-proxy",
	}

	effective := EffectiveSurfaceCapabilitySettings(root, surface)
	if effective.Source != SurfaceCapabilitySettingsSourceSurface {
		t.Fatalf("source = %q, want local surface settings", effective.Source)
	}
	if effective.Contract.Backend != agentproto.BackendCodex || effective.Contract.CodexProfileID != "team-proxy" {
		t.Fatalf("effective contract = %#v, want malformed identity to stay local", effective.Contract)
	}
}

func TestEffectiveSurfaceCapabilitySettingsRejectsGatewayIdentityMismatch(t *testing.T) {
	root := NewRoot()
	root.BotCapabilitySettings["feishu:gateway:app-2"] = BotCapabilitySettingsRecord{
		GatewayID:       "app-2",
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	}
	surface := &SurfaceConsoleRecord{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		Platform:         "feishu",
		GatewayID:        "app-2",
		ChatID:           "ou_user",
		ProductMode:      ProductModeNormal,
		Backend:          agentproto.BackendCodex,
		CodexProfileID:   "team-proxy",
	}

	effective := EffectiveSurfaceCapabilitySettings(root, surface)
	if effective.Source != SurfaceCapabilitySettingsSourceSurface {
		t.Fatalf("source = %q, want gateway-mismatched identity to stay local", effective.Source)
	}
	if effective.Contract.Backend != agentproto.BackendCodex || effective.Contract.CodexProfileID != "team-proxy" {
		t.Fatalf("effective contract = %#v, want gateway-mismatched identity to stay local", effective.Contract)
	}
}

func TestLookupSurfaceBotCapabilitySettingsDistinguishesAbsentAndInvalid(t *testing.T) {
	root := NewRoot()
	surface := &SurfaceConsoleRecord{
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		Platform:         "feishu",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ProductMode:      ProductModeNormal,
		Backend:          agentproto.BackendClaude,
		ClaudeProfileID:  "stale-local",
		PromptOverride:   ModelConfigRecord{ReasoningEffort: "high"},
	}

	if _, status := LookupSurfaceBotCapabilitySettings(root, surface); status != BotCapabilitySettingsLookupAbsent {
		t.Fatalf("missing lookup status = %q, want absent", status)
	}

	key := BotCapabilitySettingsKey("app-1")
	root.BotCapabilitySettings[key] = BotCapabilitySettingsRecord{}
	if _, status := LookupSurfaceBotCapabilitySettings(root, surface); status != BotCapabilitySettingsLookupInvalid {
		t.Fatalf("malformed lookup status = %q, want invalid", status)
	}
	effective := EffectiveSurfaceCapabilitySettings(root, surface)
	if effective.Source != SurfaceCapabilitySettingsSourceInvalid {
		t.Fatalf("malformed effective source = %q, want invalid", effective.Source)
	}
	if effective.Contract.ClaudeProfileID == "stale-local" || effective.PromptOverride.ReasoningEffort == "high" {
		t.Fatalf("invalid canonical record fell back to local surface values: %#v", effective)
	}

	root.BotCapabilitySettings[key] = BotCapabilitySettingsRecord{
		GatewayID:       "app-2",
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "other-gateway",
	}
	if _, status := LookupSurfaceBotCapabilitySettings(root, surface); status != BotCapabilitySettingsLookupInvalid {
		t.Fatalf("cross-gateway lookup status = %q, want invalid", status)
	}
	if effective := EffectiveSurfaceCapabilitySettings(root, surface); effective.Source != SurfaceCapabilitySettingsSourceInvalid {
		t.Fatalf("cross-gateway effective source = %q, want invalid", effective.Source)
	}
}
