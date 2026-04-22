package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) buildCommandMenuView(surface *state.SurfaceConsoleRecord, raw string) control.FeishuCatalogView {
	return control.FeishuCatalogView{
		Menu: &control.FeishuCatalogMenuView{
			Stage:   string(s.commandMenuStage(surface)),
			GroupID: parseCommandMenuView(raw),
		},
	}
}

func (s *Service) buildModeCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildModeCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildModeCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	return control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{
			CommandID:    control.FeishuCommandMode,
			CurrentValue: string(s.normalizeSurfaceProductMode(surface)),
		}, cardState),
	}
}

func (s *Service) buildAutoContinueCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildAutoContinueCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildAutoContinueCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	current := "off"
	if surface != nil && surface.AutoContinue.Enabled {
		current = "on"
	}
	return control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{
			CommandID:    control.FeishuCommandAutoContinue,
			CurrentValue: current,
		}, cardState),
	}
}

func (s *Service) buildReasoningCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildReasoningCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildReasoningCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	view := control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{CommandID: control.FeishuCommandReasoning}, cardState),
	}
	attachedInstanceID := ""
	if surface != nil {
		attachedInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	inst := s.root.Instances[attachedInstanceID]
	if inst == nil {
		view.Config.RequiresAttachment = true
		return view
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	view.Config.EffectiveValue = strings.TrimSpace(summary.EffectiveReasoningEffort)
	view.Config.OverrideValue = strings.TrimSpace(summary.OverrideReasoningEffort)
	return view
}

func (s *Service) buildAccessCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildAccessCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildAccessCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	view := control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{CommandID: control.FeishuCommandAccess}, cardState),
	}
	attachedInstanceID := ""
	if surface != nil {
		attachedInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	inst := s.root.Instances[attachedInstanceID]
	if inst == nil {
		view.Config.RequiresAttachment = true
		return view
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	view.Config.EffectiveValue = strings.TrimSpace(summary.EffectiveAccessMode)
	view.Config.OverrideValue = strings.TrimSpace(summary.OverrideAccessMode)
	return view
}

func (s *Service) buildPlanCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildPlanCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildPlanCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	current := state.PlanModeSettingOff
	if surface != nil {
		current = state.NormalizePlanModeSetting(surface.PlanMode)
	}
	view := control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{
			CommandID:    control.FeishuCommandPlan,
			CurrentValue: string(current),
		}, cardState),
	}
	attachedInstanceID := ""
	if surface != nil {
		attachedInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	inst := s.root.Instances[attachedInstanceID]
	if inst == nil || surface == nil {
		return view
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	view.Config.EffectiveValue = strings.TrimSpace(summary.ObservedThreadPlanMode)
	return view
}

func (s *Service) buildModelCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildModelCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildModelCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	view := control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{CommandID: control.FeishuCommandModel}, cardState),
	}
	attachedInstanceID := ""
	if surface != nil {
		attachedInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	inst := s.root.Instances[attachedInstanceID]
	if inst == nil {
		view.Config.RequiresAttachment = true
		return view
	}
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	view.Config.EffectiveValue = strings.TrimSpace(summary.EffectiveModel)
	view.Config.OverrideValue = strings.TrimSpace(summary.OverrideModel)
	view.Config.OverrideExtraValue = strings.TrimSpace(summary.OverrideReasoningEffort)
	return view
}

func (s *Service) buildVerboseCommandView(surface *state.SurfaceConsoleRecord) control.FeishuCatalogView {
	return s.buildVerboseCommandViewState(surface, control.FeishuCatalogConfigView{})
}

func (s *Service) buildVerboseCommandViewState(surface *state.SurfaceConsoleRecord, cardState control.FeishuCatalogConfigView) control.FeishuCatalogView {
	current := state.SurfaceVerbosityNormal
	if surface != nil {
		current = state.NormalizeSurfaceVerbosity(surface.Verbosity)
	}
	return control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&control.FeishuCatalogConfigView{
			CommandID:    control.FeishuCommandVerbose,
			CurrentValue: string(current),
		}, cardState),
	}
}

func (s *Service) applyCommandConfigCardState(base *control.FeishuCatalogConfigView, cardState control.FeishuCatalogConfigView) *control.FeishuCatalogConfigView {
	if base == nil {
		base = &control.FeishuCatalogConfigView{}
	}
	if strings.TrimSpace(cardState.FormDefaultValue) != "" {
		base.FormDefaultValue = strings.TrimSpace(cardState.FormDefaultValue)
	}
	if strings.TrimSpace(cardState.StatusKind) != "" {
		base.StatusKind = strings.TrimSpace(cardState.StatusKind)
	}
	if strings.TrimSpace(cardState.StatusText) != "" {
		base.StatusText = strings.TrimSpace(cardState.StatusText)
	}
	if cardState.Sealed {
		base.Sealed = true
	}
	return base
}

func (s *Service) commandPageFromView(surface *state.SurfaceConsoleRecord, view control.FeishuCatalogView) control.FeishuPageView {
	productMode := ""
	stage := ""
	if surface != nil {
		productMode = string(s.normalizeSurfaceProductMode(surface))
		stage = string(s.commandMenuStage(surface))
	}
	page, ok := control.FeishuPageViewFromView(view, productMode, stage)
	if !ok {
		return control.FeishuPageView{}
	}
	return page
}
