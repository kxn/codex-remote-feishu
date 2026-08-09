package state

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

const (
	SurfaceCapabilitySettingsSourceSurface = "surface"
	SurfaceCapabilitySettingsSourceBot     = "bot"
	SurfaceCapabilitySettingsSourceInvalid = "invalid"
)

type BotCapabilitySettingsLookupStatus string

const (
	BotCapabilitySettingsLookupNotApplicable BotCapabilitySettingsLookupStatus = "not_applicable"
	BotCapabilitySettingsLookupAbsent        BotCapabilitySettingsLookupStatus = "absent"
	BotCapabilitySettingsLookupValid         BotCapabilitySettingsLookupStatus = "valid"
	BotCapabilitySettingsLookupInvalid       BotCapabilitySettingsLookupStatus = "invalid"
)

func BotCapabilitySettingsKey(gatewayID string) string {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return ""
	}
	return "feishu:gateway:" + gatewayID
}

func NormalizeBotCapabilitySettingsRecord(record BotCapabilitySettingsRecord) (BotCapabilitySettingsRecord, bool) {
	record.GatewayID = strings.TrimSpace(record.GatewayID)
	if record.GatewayID == "" {
		return BotCapabilitySettingsRecord{}, false
	}
	record.CodexProviderID = NormalizeDesiredCodexProviderID(record.CodexProviderID)
	record = CanonicalizeBotCapabilityProfileSelection(record)
	codexProviderID := record.CodexProviderID
	claudeProfileID := NormalizeDesiredClaudeProfileID(record.ClaudeProfileID)
	openCodeProfileID := NormalizeDesiredOpenCodeProfileID(record.OpenCodeProfileID)
	contract := NormalizeSurfaceBackendContract(SurfaceBackendContract{
		ProductMode:       record.ProductMode,
		Backend:           record.Backend,
		CodexProviderID:   codexProviderID,
		ClaudeProfileID:   claudeProfileID,
		OpenCodeProfileID: openCodeProfileID,
	})
	record.ProductMode = contract.ProductMode
	record.Backend = contract.Backend
	record.CodexProviderID = codexProviderID
	record.ClaudeProfileID = claudeProfileID
	record.OpenCodeProfileID = openCodeProfileID
	record.PromptOverride = NormalizeModelConfigRecord(record.PromptOverride)
	record.PlanMode = NormalizePlanModeSetting(record.PlanMode)
	record.UpdatedBy = strings.TrimSpace(record.UpdatedBy)
	if !record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	return record, true
}

func CanonicalizeBotCapabilityProfileSelection(record BotCapabilitySettingsRecord) BotCapabilitySettingsRecord {
	record.CodexProfileID = strings.TrimSpace(record.CodexProfileID)
	if record.CodexProfileID == "" {
		record.CodexProfileID = CodexProfileIDFromLegacyProviderID(record.CodexProviderID)
	}
	record.CodexProviderID = LegacyCodexProviderIDFromProfileID(record.CodexProfileID)
	return record
}

func NormalizeModelConfigRecord(record ModelConfigRecord) ModelConfigRecord {
	record.Model = strings.TrimSpace(record.Model)
	record.ReasoningEffort = NormalizeClaudeReasoningEffort(record.ReasoningEffort)
	record.AccessMode = agentproto.NormalizeAccessMode(record.AccessMode)
	return record
}

func BotCapabilitySettingsContract(record BotCapabilitySettingsRecord) SurfaceBackendContract {
	normalized, ok := NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		return HeadlessCodexSurfaceBackendContract("")
	}
	return NormalizeSurfaceBackendContract(SurfaceBackendContract{
		ProductMode:       normalized.ProductMode,
		Backend:           normalized.Backend,
		CodexProviderID:   normalized.CodexProviderID,
		ClaudeProfileID:   normalized.ClaudeProfileID,
		OpenCodeProfileID: normalized.OpenCodeProfileID,
	})
}

func EffectiveSurfaceCapabilitySettings(root *Root, surface *SurfaceConsoleRecord) SurfaceCapabilitySettings {
	record, status := LookupSurfaceBotCapabilitySettings(root, surface)
	if status == BotCapabilitySettingsLookupValid {
		return SurfaceCapabilitySettings{
			Contract:            BotCapabilitySettingsContract(record),
			PromptOverride:      NormalizeModelConfigRecord(record.PromptOverride),
			PlanMode:            NormalizePlanModeSetting(record.PlanMode),
			PlanModeOverrideSet: record.PlanModeOverrideSet,
			Source:              SurfaceCapabilitySettingsSourceBot,
		}
	}
	if status == BotCapabilitySettingsLookupInvalid {
		return SurfaceCapabilitySettings{Source: SurfaceCapabilitySettingsSourceInvalid}
	}
	if surface == nil {
		return SurfaceCapabilitySettings{
			Contract: HeadlessCodexSurfaceBackendContract(""),
			Source:   SurfaceCapabilitySettingsSourceSurface,
		}
	}
	return SurfaceCapabilitySettings{
		Contract:            SurfaceDesiredBackendContract(surface),
		PromptOverride:      NormalizeModelConfigRecord(surface.PromptOverride),
		PlanMode:            NormalizePlanModeSetting(surface.PlanMode),
		PlanModeOverrideSet: surface.PlanModeOverrideSet,
		Source:              SurfaceCapabilitySettingsSourceSurface,
	}
}

func LookupSurfaceBotCapabilitySettings(root *Root, surface *SurfaceConsoleRecord) (BotCapabilitySettingsRecord, BotCapabilitySettingsLookupStatus) {
	if root == nil || surface == nil || !SurfaceUsesBotCapabilitySettings(surface) {
		return BotCapabilitySettingsRecord{}, BotCapabilitySettingsLookupNotApplicable
	}
	key := BotCapabilitySettingsKey(surface.GatewayID)
	if key == "" {
		return BotCapabilitySettingsRecord{}, BotCapabilitySettingsLookupNotApplicable
	}
	record, ok := root.BotCapabilitySettings[key]
	if !ok {
		return BotCapabilitySettingsRecord{}, BotCapabilitySettingsLookupAbsent
	}
	record, ok = NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		return BotCapabilitySettingsRecord{}, BotCapabilitySettingsLookupInvalid
	}
	if BotCapabilitySettingsKey(record.GatewayID) != key {
		return BotCapabilitySettingsRecord{}, BotCapabilitySettingsLookupInvalid
	}
	return record, BotCapabilitySettingsLookupValid
}

func SurfaceUsesBotCapabilitySettings(surface *SurfaceConsoleRecord) bool {
	if surface == nil || strings.TrimSpace(surface.ChatID) == "" {
		return false
	}
	if surface.Platform != "" && surface.Platform != "feishu" {
		return false
	}
	ref, ok := feishuidentity.ParseSurfaceRef(surface.SurfaceSessionID)
	return ok && ref.GatewayID == strings.TrimSpace(surface.GatewayID) && (ref.IsUser() || ref.IsChat())
}
