package daemon

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func shouldResetOpenCodeResumeTarget(previous, current surfaceresume.Entry) bool {
	if state.NormalizeProductMode(state.ProductMode(previous.ProductMode)) != state.NormalizeProductMode(state.ProductMode(current.ProductMode)) ||
		state.NormalizeHeadlessBackend(agentproto.Backend(previous.Backend)) != agentproto.BackendOpenCode ||
		state.NormalizeHeadlessBackend(agentproto.Backend(current.Backend)) != agentproto.BackendOpenCode {
		return false
	}
	if state.NormalizeOpenCodeProfileID(previous.OpenCodeProfileID) != state.NormalizeOpenCodeProfileID(current.OpenCodeProfileID) {
		return true
	}
	previousRef := state.NormalizeOpenCodeAdmissionRef(previous.OpenCodeAdmissionRef)
	currentRef := state.NormalizeOpenCodeAdmissionRef(current.OpenCodeAdmissionRef)
	if previousRef == nil || currentRef == nil {
		return previousRef != nil || currentRef != nil
	}
	return *previousRef != *currentRef
}

func shouldPreserveOpenCodeAdmissionRef(previous, current surfaceresume.Entry, clearResumeTarget bool) bool {
	if clearResumeTarget || previous.OpenCodeAdmissionRef == nil || strings.TrimSpace(current.ResumeThreadID) == "" ||
		strings.TrimSpace(previous.ResumeThreadID) != strings.TrimSpace(current.ResumeThreadID) {
		return false
	}
	profileID := state.NormalizeOpenCodeProfileID(current.OpenCodeProfileID)
	return previous.OpenCodeAdmissionRef.ProfileRef.ID == profileID
}

func (a *App) seedSurfaceSessionSettingsFromBotRecordsLocked(entry *surfaceresume.Entry) bool {
	if entry == nil || strings.TrimSpace(entry.GatewayID) == "" {
		return false
	}
	var record state.BotCapabilitySettingsRecord
	found := false
	for _, candidate := range a.service.BotCapabilitySettings() {
		if strings.TrimSpace(candidate.GatewayID) != strings.TrimSpace(entry.GatewayID) {
			continue
		}
		record = candidate
		found = true
		break
	}
	if !found {
		return false
	}
	changed := false
	if strings.TrimSpace(entry.AccessMode) == "" && strings.TrimSpace(record.PromptOverride.AccessMode) != "" {
		entry.AccessMode = strings.TrimSpace(record.PromptOverride.AccessMode)
		changed = true
	}
	if !entry.PlanModeOverrideSet && record.PlanModeOverrideSet {
		entry.PlanMode = string(state.NormalizePlanModeSetting(record.PlanMode))
		entry.PlanModeOverrideSet = true
		changed = true
	}
	return changed
}

func (a *App) materializeSurfaceResumeEntryLocked(entry surfaceresume.Entry) {
	if a.seedSurfaceSessionSettingsFromBotRecordsLocked(&entry) {
		a.putSurfaceResumeEntryLocked(entry, time.Now())
	}
	a.service.MaterializeSurfaceResumeContractWithOpenCodeRef(
		entry.SurfaceSessionID,
		entry.GatewayID,
		entry.ChatID,
		entry.ActorUserID,
		state.PersistedSurfaceBackendContract(
			state.ProductMode(entry.ProductMode),
			agentproto.Backend(entry.Backend),
			entry.CodexProfileID,
			entry.ClaudeProfileID,
			entry.OpenCodeProfileID,
		),
		entry.OpenCodeAdmissionRef,
		state.SurfaceVerbosity(entry.Verbosity),
		state.PlanModeSettingOff,
	)
	a.service.RestoreSurfaceSessionSettings(
		entry.SurfaceSessionID,
		entry.AccessMode,
		state.PlanModeSetting(entry.PlanMode),
		entry.PlanModeOverrideSet,
	)
}

// seedSurfaceSessionSettingsFromBotRecordsLocked 把旧版机器人级 access/plan
// 设置一次性迁移为各已有 surface 的会话级初始值。迁移后读路径不再使用
// gateway record 中的这两个字段。
