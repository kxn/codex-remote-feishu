package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

func (s *Service) applySurfaceCapabilitySettingsMutation(
	surface *state.SurfaceConsoleRecord,
	mutateBot func(*state.BotCapabilitySettingsRecord),
	mutateLocal func(*state.SurfaceConsoleRecord),
) bool {
	if state.SurfaceUsesBotCapabilitySettings(surface) {
		if !s.surfaceCanWriteBotCapabilitySettings(surface) || mutateBot == nil {
			return false
		}
		return s.updateBotCapabilitySettings(surface, mutateBot)
	}
	if surface == nil || mutateLocal == nil {
		return false
	}
	mutateLocal(surface)
	return true
}

func (s *Service) applySurfaceCapabilityLifecycleMutation(
	surface *state.SurfaceConsoleRecord,
	mutateBot func(*state.BotCapabilitySettingsRecord),
	mutateLocal func(*state.SurfaceConsoleRecord),
) bool {
	if !state.SurfaceUsesBotCapabilitySettings(surface) {
		if surface == nil || mutateLocal == nil {
			return false
		}
		mutateLocal(surface)
		return true
	}
	if s == nil || s.root == nil || mutateBot == nil {
		return false
	}
	switch _, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface); status {
	case state.BotCapabilitySettingsLookupValid:
		return s.updateExistingBotCapabilitySettings(surface, mutateBot)
	case state.BotCapabilitySettingsLookupAbsent:
		if mutateLocal == nil {
			return false
		}
		mutateLocal(surface)
		return true
	default:
		return false
	}
}

func (s *Service) updateBotCapabilitySettings(surface *state.SurfaceConsoleRecord, mutate func(*state.BotCapabilitySettingsRecord)) bool {
	if s == nil || s.root == nil || surface == nil || mutate == nil || !s.surfaceCanWriteBotCapabilitySettings(surface) {
		return false
	}
	key := state.BotCapabilitySettingsKey(surface.GatewayID)
	if key == "" {
		return false
	}
	record, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface)
	switch status {
	case state.BotCapabilitySettingsLookupValid:
	case state.BotCapabilitySettingsLookupAbsent:
		var ok bool
		record, ok = state.NormalizeBotCapabilitySettingsRecord(botCapabilitySettingsFromSurface(surface))
		if !ok {
			return false
		}
	default:
		return false
	}
	if s.root.BotCapabilitySettings == nil {
		s.root.BotCapabilitySettings = map[string]state.BotCapabilitySettingsRecord{}
	}
	return s.commitBotCapabilitySettingsMutation(surface, key, record, mutate)
}

func (s *Service) updateExistingBotCapabilitySettings(surface *state.SurfaceConsoleRecord, mutate func(*state.BotCapabilitySettingsRecord)) bool {
	if s == nil || s.root == nil || surface == nil || mutate == nil || !state.SurfaceUsesBotCapabilitySettings(surface) {
		return false
	}
	key := state.BotCapabilitySettingsKey(surface.GatewayID)
	record, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface)
	if key == "" || status != state.BotCapabilitySettingsLookupValid {
		return false
	}
	return s.commitBotCapabilitySettingsMutation(surface, key, record, mutate)
}

func (s *Service) commitBotCapabilitySettingsMutation(surface *state.SurfaceConsoleRecord, key string, record state.BotCapabilitySettingsRecord, mutate func(*state.BotCapabilitySettingsRecord)) bool {
	mutate(&record)
	record.UpdatedBy = strings.TrimSpace(surface.ActorUserID)
	record.UpdatedAt = s.now()
	record, ok := state.NormalizeBotCapabilitySettingsRecord(record)
	if !ok || state.BotCapabilitySettingsKey(record.GatewayID) != key {
		return false
	}
	s.root.BotCapabilitySettings[key] = record
	s.projectBotCapabilitySettingsToGatewaySurfaces(surface, record)
	return true
}

func botCapabilitySettingsFromSurface(surface *state.SurfaceConsoleRecord) state.BotCapabilitySettingsRecord {
	contract := state.SurfaceDesiredBackendContract(surface)
	return state.BotCapabilitySettingsRecord{
		GatewayID:           strings.TrimSpace(surface.GatewayID),
		ProductMode:         contract.ProductMode,
		Backend:             contract.Backend,
		CodexProviderID:     surface.CodexProviderID,
		ClaudeProfileID:     surface.ClaudeProfileID,
		PromptOverride:      surface.PromptOverride,
		PlanMode:            surface.PlanMode,
		PlanModeOverrideSet: surface.PlanModeOverrideSet,
	}
}

func (s *Service) projectBotCapabilitySettingsToSurface(surface *state.SurfaceConsoleRecord, record state.BotCapabilitySettingsRecord) {
	if surface == nil {
		return
	}
	normalized, ok := state.NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		return
	}
	previousProviderID := state.NormalizeCodexProviderID(surface.CodexProviderID)
	s.setSurfaceDesiredContract(surface, state.BotCapabilitySettingsContract(normalized))
	surface.CodexProviderID = normalized.CodexProviderID
	surface.ClaudeProfileID = normalized.ClaudeProfileID
	surface.PromptOverride = normalized.PromptOverride
	surface.PlanMode = normalized.PlanMode
	surface.PlanModeOverrideSet = normalized.PlanModeOverrideSet
	if state.NormalizeCodexProviderID(normalized.CodexProviderID) != previousProviderID {
		surface.CodexAdmissionRef = nil
		surface.CodexConnectionContract = nil
		surface.CodexThreadPolicy = nil
	}
}

func (s *Service) projectLatestBotCapabilitySettingsToSurface(surface *state.SurfaceConsoleRecord) bool {
	if s == nil || s.root == nil || surface == nil {
		return false
	}
	record, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface)
	if status != state.BotCapabilitySettingsLookupValid {
		return false
	}
	s.projectBotCapabilitySettingsToSurface(surface, record)
	return true
}

func (s *Service) botCapabilitySettingsInvalid(surface *state.SurfaceConsoleRecord) bool {
	if s == nil || s.root == nil {
		return false
	}
	_, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface)
	return status == state.BotCapabilitySettingsLookupInvalid
}

func (s *Service) botCapabilitySettingsInvalidEvents(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return notice(surface, "bot_capability_settings_invalid", "机器人设置当前不可用，本次操作已暂停。请联系管理员修复后重试。")
}

func (s *Service) invalidBotCapabilitySettingsDispatchGate(surface *state.SurfaceConsoleRecord) ([]eventcontract.Event, bool) {
	if !s.botCapabilitySettingsInvalid(surface) {
		return nil, false
	}
	if len(surface.QueuedQueueItemIDs) == 0 && activeAutoContinueEpisode(surface) == nil {
		return nil, true
	}
	if !s.allowActiveNotice("bot_capability_settings_invalid", surface.SurfaceSessionID, "", "", "", time.Minute) {
		return nil, true
	}
	return s.botCapabilitySettingsInvalidEvents(surface), true
}

func (s *Service) rejectInvalidBotCapabilitySettings(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if !s.botCapabilitySettingsInvalid(surface) {
		return nil
	}
	switch action.Kind {
	case control.ActionStop, control.ActionDetach, control.ActionWorkspaceDetach:
		return nil
	default:
		return s.botCapabilitySettingsInvalidEvents(surface)
	}
}

func (s *Service) projectBotCapabilitySettingsToGatewaySurfaces(current *state.SurfaceConsoleRecord, record state.BotCapabilitySettingsRecord) {
	key := state.BotCapabilitySettingsKey(record.GatewayID)
	projectedCurrent := false
	for _, surface := range s.root.Surfaces {
		if surface == nil || state.BotCapabilitySettingsKey(surface.GatewayID) != key {
			continue
		}
		if state.EffectiveSurfaceCapabilitySettings(s.root, surface).Source != state.SurfaceCapabilitySettingsSourceBot {
			continue
		}
		s.projectBotCapabilitySettingsToSurface(surface, record)
		projectedCurrent = projectedCurrent || surface == current
	}
	if !projectedCurrent {
		s.projectBotCapabilitySettingsToSurface(current, record)
	}
}

func (s *Service) surfaceCanWriteBotCapabilitySettings(surface *state.SurfaceConsoleRecord) bool {
	if !state.SurfaceUsesBotCapabilitySettings(surface) {
		return false
	}
	ref, ok := feishuidentity.ParseSurfaceRef(surface.SurfaceSessionID)
	return ok && ref.IsUser()
}

func (s *Service) rejectBotCapabilityMutationInReadOnlySurface(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || surfaceFeishuRoomID(surface) == "" || !isBotCapabilitySettingsAction(action.Kind) {
		return nil
	}
	text := "此设置请在和机器人的私聊中修改。群聊里只保留当前群会话设置。"
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "error",
			StatusText: text,
		})
	}
	return notice(surface, "bot_capability_private_required", text)
}

func isBotCapabilitySettingsAction(kind control.ActionKind) bool {
	switch kind {
	case control.ActionModeCommand,
		control.ActionCodexProviderCommand,
		control.ActionClaudeProfileCommand,
		control.ActionModelCommand,
		control.ActionReasoningCommand,
		control.ActionAccessCommand,
		control.ActionPlanCommand:
		return true
	default:
		return false
	}
}
