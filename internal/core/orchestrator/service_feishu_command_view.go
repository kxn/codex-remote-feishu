package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) buildCommandMenuView(surface *state.SurfaceConsoleRecord, raw string) control.FeishuCatalogView {
	ctx := s.buildCatalogContext(surface)
	return control.FeishuCatalogView{
		Menu: &control.FeishuCatalogMenuView{
			Stage:   ctx.MenuStage,
			GroupID: parseCommandMenuView(raw),
		},
	}
}

func (s *Service) buildConfigCommandView(surface *state.SurfaceConsoleRecord, commandID string) control.FeishuCatalogView {
	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(commandID)
	if !ok {
		return control.FeishuCatalogView{}
	}
	return s.buildConfigCommandViewState(surface, flow, control.FeishuCatalogConfigView{})
}

func (s *Service) buildConfigCommandViewState(
	surface *state.SurfaceConsoleRecord,
	flow control.FeishuConfigFlowDefinition,
	cardState control.FeishuCatalogConfigView,
) control.FeishuCatalogView {
	base := flow.BaseCatalogView()
	view := control.FeishuCatalogView{
		Config: s.applyCommandConfigCardState(&base, cardState),
	}

	ctx := s.buildCatalogContext(surface)
	if view.Config.CatalogBackend == "" {
		view.Config.CatalogBackend = ctx.Backend
	}
	if strings.TrimSpace(view.Config.CatalogFamilyID) != "" && (strings.TrimSpace(view.Config.CatalogVariantID) == "" || view.Config.CatalogVariantID == flow.DefaultVariantID()) {
		view.Config.CatalogVariantID = control.FeishuCommandVariantIDForContext(view.Config.CatalogFamilyID, ctx)
	}
	inst := s.root.Instances[ctx.InstanceID]
	if flow.RequiresAttachment && ctx.AttachedKind == string(control.CatalogAttachedKindDetached) {
		view.Config.RequiresAttachment = true
		return view
	}

	var summary control.PromptRouteSummary
	if flow.UsesPromptSummary() && inst != nil {
		summary = s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	}

	view.Config.CurrentValue = s.resolveConfigFlowValue(ctx, surface, summary, flow.CurrentValueKey)
	view.Config.EffectiveValue = s.resolveConfigFlowValue(ctx, surface, summary, flow.EffectiveValueKey)
	view.Config.EffectiveValueSource = resolveConfigFlowValueSource(summary, flow.EffectiveValueKey)
	view.Config.OverrideValue = s.resolveConfigFlowValue(ctx, surface, summary, flow.OverrideValueKey)
	view.Config.OverrideExtraValue = s.resolveConfigFlowValue(ctx, surface, summary, flow.OverrideExtraValueKey)
	view.Config.UsesLocalRequestedOverrides = summary.UsesLocalRequestedOverrides
	view.Config.PlanModeOverrideSet = summary.PlanModeOverrideSet
	switch flow.CommandID {
	case control.FeishuCommandCodexProvider:
		view.Config.FormOptions = s.codexProviderCommandOptions()
		if strings.TrimSpace(view.Config.FormDefaultValue) == "" {
			view.Config.FormDefaultValue = s.surfaceCodexProviderID(surface)
		}
	case control.FeishuCommandClaudeProfile:
		view.Config.FormOptions = s.claudeProfileCommandOptions()
		if strings.TrimSpace(view.Config.FormDefaultValue) == "" {
			view.Config.FormDefaultValue = s.surfaceClaudeProfileID(surface)
		}
	}
	return view
}

func resolveConfigFlowValueSource(summary control.PromptRouteSummary, key control.FeishuConfigFlowValueKey) string {
	switch key {
	case control.FeishuConfigFlowValuePromptEffectiveReasoning:
		return strings.TrimSpace(summary.EffectiveReasoningEffortSource)
	case control.FeishuConfigFlowValuePromptEffectiveAccess:
		return strings.TrimSpace(summary.EffectiveAccessModeSource)
	case control.FeishuConfigFlowValuePromptEffectiveModel:
		return strings.TrimSpace(summary.EffectiveModelSource)
	case control.FeishuConfigFlowValuePromptObservedThreadPlan:
		if strings.TrimSpace(summary.ObservedThreadPlanMode) != "" {
			return "thread"
		}
		return ""
	default:
		return ""
	}
}

func mergeConfigCardStateFromAction(
	flow control.FeishuConfigFlowDefinition,
	action control.Action,
	cardState control.FeishuCatalogConfigView,
) control.FeishuCatalogConfigView {
	if strings.TrimSpace(cardState.CommandID) == "" {
		cardState.CommandID = strings.TrimSpace(flow.CommandID)
	}
	if strings.TrimSpace(cardState.CatalogFamilyID) == "" {
		cardState.CatalogFamilyID = strings.TrimSpace(action.CatalogFamilyID)
		if cardState.CatalogFamilyID == "" {
			cardState.CatalogFamilyID = flow.CatalogFamilyID()
		}
	}
	if strings.TrimSpace(cardState.CatalogVariantID) == "" {
		cardState.CatalogVariantID = strings.TrimSpace(action.CatalogVariantID)
		if cardState.CatalogVariantID == "" {
			cardState.CatalogVariantID = flow.DefaultVariantID()
		}
	}
	if cardState.CatalogBackend == "" {
		cardState.CatalogBackend = action.CatalogBackend
	}
	return cardState
}

func (s *Service) resolveConfigFlowValue(
	ctx control.CatalogContext,
	surface *state.SurfaceConsoleRecord,
	summary control.PromptRouteSummary,
	key control.FeishuConfigFlowValueKey,
) string {
	switch key {
	case control.FeishuConfigFlowValueSurfaceProductMode:
		normalized := control.NormalizeCatalogContext(ctx)
		return state.SurfaceModeAlias(state.ProductMode(normalized.ProductMode), normalized.Backend)
	case control.FeishuConfigFlowValueSurfaceCodexProvider:
		if surface != nil {
			return s.surfaceCodexProviderID(surface)
		}
		return state.DefaultCodexProviderID
	case control.FeishuConfigFlowValueSurfaceClaudeProfile:
		if surface != nil {
			return s.surfaceClaudeProfileID(surface)
		}
		return state.DefaultClaudeProfileID
	case control.FeishuConfigFlowValueSurfaceAutoWhip:
		if surface != nil && surface.AutoWhip.Enabled {
			return "on"
		}
		return "off"
	case control.FeishuConfigFlowValueSurfaceAutoContinue:
		if surface != nil && surface.AutoContinue.Enabled {
			return "on"
		}
		return "off"
	case control.FeishuConfigFlowValueSurfacePlanMode:
		current := state.PlanModeSettingOff
		if surface != nil {
			current = state.NormalizePlanModeSetting(surface.PlanMode)
		}
		return string(current)
	case control.FeishuConfigFlowValueSurfaceVerbosity:
		current := state.SurfaceVerbosityNormal
		if surface != nil {
			current = state.NormalizeSurfaceVerbosity(surface.Verbosity)
		}
		return string(current)
	case control.FeishuConfigFlowValuePromptEffectiveReasoning:
		return strings.TrimSpace(summary.EffectiveReasoningEffort)
	case control.FeishuConfigFlowValuePromptOverrideReasoning:
		return strings.TrimSpace(summary.OverrideReasoningEffort)
	case control.FeishuConfigFlowValuePromptEffectiveAccess:
		return strings.TrimSpace(summary.EffectiveAccessMode)
	case control.FeishuConfigFlowValuePromptOverrideAccess:
		return strings.TrimSpace(summary.OverrideAccessMode)
	case control.FeishuConfigFlowValuePromptObservedThreadPlan:
		return strings.TrimSpace(summary.ObservedThreadPlanMode)
	case control.FeishuConfigFlowValuePromptEffectiveModel:
		return strings.TrimSpace(summary.EffectiveModel)
	case control.FeishuConfigFlowValuePromptOverrideModel:
		return strings.TrimSpace(summary.OverrideModel)
	default:
		return ""
	}
}

func (s *Service) applyCommandConfigCardState(base *control.FeishuCatalogConfigView, cardState control.FeishuCatalogConfigView) *control.FeishuCatalogConfigView {
	if base == nil {
		base = &control.FeishuCatalogConfigView{}
	}
	if strings.TrimSpace(cardState.FormDefaultValue) != "" {
		base.FormDefaultValue = strings.TrimSpace(cardState.FormDefaultValue)
	}
	if len(cardState.FormOptions) != 0 {
		base.FormOptions = append([]control.CommandCatalogFormFieldOption(nil), cardState.FormOptions...)
	}
	if strings.TrimSpace(cardState.CatalogFamilyID) != "" {
		base.CatalogFamilyID = strings.TrimSpace(cardState.CatalogFamilyID)
	}
	if strings.TrimSpace(cardState.CatalogVariantID) != "" {
		base.CatalogVariantID = strings.TrimSpace(cardState.CatalogVariantID)
	}
	if cardState.CatalogBackend != "" {
		base.CatalogBackend = cardState.CatalogBackend
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

func (s *Service) codexProviderCommandOptions() []control.CommandCatalogFormFieldOption {
	providers := s.CodexProviders()
	if len(providers) == 0 {
		return []control.CommandCatalogFormFieldOption{{
			Label: state.DefaultCodexProviderName,
			Value: state.DefaultCodexProviderID,
		}}
	}
	labelCounts := map[string]int{}
	for _, provider := range providers {
		label := strings.TrimSpace(provider.Name)
		if label == "" {
			label = provider.ID
		}
		labelCounts[label]++
	}
	options := make([]control.CommandCatalogFormFieldOption, 0, len(providers))
	for _, provider := range providers {
		label := strings.TrimSpace(provider.Name)
		if label == "" {
			label = provider.ID
		}
		if labelCounts[label] > 1 && !strings.EqualFold(label, strings.TrimSpace(provider.ID)) {
			label += "（" + strings.TrimSpace(provider.ID) + "）"
		}
		options = append(options, control.CommandCatalogFormFieldOption{
			Label: label,
			Value: strings.TrimSpace(provider.ID),
		})
	}
	return options
}

func (s *Service) claudeProfileCommandOptions() []control.CommandCatalogFormFieldOption {
	profiles := s.ClaudeProfiles()
	if len(profiles) == 0 {
		return []control.CommandCatalogFormFieldOption{{
			Label: state.DefaultClaudeProfileName,
			Value: state.DefaultClaudeProfileID,
		}}
	}
	labelCounts := map[string]int{}
	for _, profile := range profiles {
		label := strings.TrimSpace(profile.Name)
		if label == "" {
			label = profile.ID
		}
		labelCounts[label]++
	}
	options := make([]control.CommandCatalogFormFieldOption, 0, len(profiles))
	for _, profile := range profiles {
		label := strings.TrimSpace(profile.Name)
		if label == "" {
			label = profile.ID
		}
		if labelCounts[label] > 1 && !strings.EqualFold(label, strings.TrimSpace(profile.ID)) {
			label += "（" + strings.TrimSpace(profile.ID) + "）"
		}
		options = append(options, control.CommandCatalogFormFieldOption{
			Label: label,
			Value: strings.TrimSpace(profile.ID),
		})
	}
	return options
}

func (s *Service) commandPageFromView(surface *state.SurfaceConsoleRecord, view control.FeishuCatalogView) control.FeishuPageView {
	page, ok := control.FeishuPageViewFromViewContext(view, s.buildCatalogContext(surface))
	if !ok {
		return control.FeishuPageView{}
	}
	return page
}
