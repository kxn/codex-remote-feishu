package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceSettingFeedback struct {
	NoticeCode     string
	NoticeText     string
	CardStatusText string
}

func (s *Service) surfaceSettingFeedbackEvents(surface *state.SurfaceConsoleRecord, action control.Action, feedback surfaceSettingFeedback) []eventcontract.Event {
	if commandCardOwnsInlineResult(action) {
		return s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			Sealed:     true,
			StatusKind: "success",
			StatusText: feedback.CardStatusText,
		})
	}
	return notice(surface, feedback.NoticeCode, feedback.NoticeText)
}

func (s *Service) applyPromptOverrideChange(surface *state.SurfaceConsoleRecord, action control.Action, inst *state.InstanceRecord, mutate func(*state.ModelConfigRecord), build func(control.PromptRouteSummary) surfaceSettingFeedback) []eventcontract.Event {
	s.applySurfaceCapabilitySettingsMutation(surface, func(record *state.BotCapabilitySettingsRecord) {
		override := record.PromptOverride
		mutate(&override)
		record.PromptOverride = state.NormalizePromptOverrideForBackend(record.Backend, compactPromptOverride(override))
	}, func(local *state.SurfaceConsoleRecord) {
		override := local.PromptOverride
		mutate(&override)
		backend := agentproto.NormalizeBackend(state.SurfaceDesiredBackendContract(local).Backend)
		local.PromptOverride = state.NormalizePromptOverrideForBackend(backend, compactPromptOverride(override))
	})
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return s.surfaceSettingFeedbackEvents(surface, action, build(summary))
}

func (s *Service) attachedInstanceForPromptSettingCommand(surface *state.SurfaceConsoleRecord, action control.Action) (*state.InstanceRecord, []eventcontract.Event) {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst != nil {
		return inst, nil
	}
	text := s.notAttachedText(surface)
	if commandCardOwnsInlineResult(action) {
		return nil, s.inlineCommandCardEvents(surface, action, control.FeishuCatalogConfigView{
			StatusKind: "error",
			StatusText: text,
		})
	}
	return nil, notice(surface, "not_attached", text)
}
