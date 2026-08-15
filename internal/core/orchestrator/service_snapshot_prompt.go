package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) resolveNextPromptSummary(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, frozenThreadID, frozenCWD string, override state.ModelConfigRecord) control.PromptRouteSummary {
	if surface == nil {
		return control.PromptRouteSummary{}
	}
	threadID := frozenThreadID
	cwd := frozenCWD
	routeMode := surface.RouteMode
	createThread := false
	if inst != nil {
		if threadID == "" && cwd == "" {
			threadID, cwd, routeMode, createThread = freezeRoute(inst, surface)
		} else {
			createThread = threadID == ""
		}
	}
	settings := state.EffectiveSurfaceCapabilitySettings(s.root, surface)
	if promptOverrideIsEmpty(override) {
		override = settings.PromptOverride
	}
	threadTitle := ""
	observedThreadAccessMode := ""
	observedThreadPlanMode := ""
	var observedThreadPermission *agentproto.ObservedPermissionState
	if inst != nil && threadID != "" {
		thread := inst.Threads[threadID]
		threadTitle = displayThreadTitle(inst, thread)
		if thread != nil && thread.ObservedPermission != nil {
			observedThreadPermission = agentproto.CloneObservedPermissionState(thread.ObservedPermission)
			if observed := agentproto.NormalizeAccessMode(observedThreadPermission.ProjectedAccessMode); observed != "" {
				observedThreadAccessMode = observed
			}
			if plan := strings.TrimSpace(observedThreadPermission.ProjectedPlanMode); plan != "" {
				observedThreadPlanMode = plan
			}
		}
		if thread != nil && observedThreadAccessMode == "" && agentproto.NormalizeAccessMode(thread.ObservedAccessMode) != "" {
			observedThreadAccessMode = agentproto.NormalizeAccessMode(thread.ObservedAccessMode)
		}
		if thread != nil && observedThreadPlanMode == "" && strings.TrimSpace(thread.ObservedPlanModeRaw) != "" {
			observedThreadPlanMode = strings.TrimSpace(thread.ObservedPlanModeRaw)
		}
		if thread != nil && observedThreadPlanMode == "" && strings.TrimSpace(string(thread.ObservedPlanMode)) != "" {
			observedThreadPlanMode = string(state.NormalizePlanModeSetting(thread.ObservedPlanMode))
		}
	}
	usesLocalRequestedOverrides := s.surfaceUsesLocalRequestedPromptOverrides(surface)
	planModeUsesLocalRequested := surfaceUsesLocalRequestedPlanMode(settings.Contract)
	planModeOverrideSet := settings.PlanModeOverrideSet
	effectivePlanMode := string(state.NormalizePlanModeSetting(settings.PlanMode))
	overridePlanMode := effectivePlanMode
	if planModeUsesLocalRequested && !planModeOverrideSet {
		overridePlanMode = ""
		effectivePlanMode = ""
	}
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return control.PromptRouteSummary{
		RouteMode:                      string(routeMode),
		ThreadID:                       threadID,
		ThreadTitle:                    threadTitle,
		CWD:                            cwd,
		CreateThread:                   createThread,
		BaseModel:                      resolution.BaseModel.Value,
		BaseReasoningEffort:            resolution.BaseReasoningEffort.Value,
		BaseModelSource:                resolution.BaseModel.Source,
		BaseReasoningEffortSource:      resolution.BaseReasoningEffort.Source,
		OverrideModel:                  resolution.Override.Model,
		OverrideReasoningEffort:        resolution.Override.ReasoningEffort,
		OverrideAccessMode:             resolution.Override.AccessMode,
		OverridePlanMode:               overridePlanMode,
		PlanModeOverrideSet:            planModeOverrideSet,
		PlanModeUsesLocalRequested:     planModeUsesLocalRequested,
		UsesLocalRequestedOverrides:    usesLocalRequestedOverrides,
		EffectivePlanMode:              effectivePlanMode,
		ObservedThreadPermission:       observedThreadPermission,
		ObservedThreadAccessMode:       observedThreadAccessMode,
		ObservedThreadPlanMode:         observedThreadPlanMode,
		EffectiveModel:                 resolution.EffectiveModel.Value,
		EffectiveReasoningEffort:       resolution.EffectiveReasoningEffort.Value,
		EffectiveAccessMode:            resolution.EffectiveAccessMode,
		EffectiveModelSource:           resolution.EffectiveModel.Source,
		EffectiveReasoningEffortSource: resolution.EffectiveReasoningEffort.Source,
		EffectiveAccessModeSource:      resolution.EffectiveAccessModeSource,
	}
}

type configValue struct {
	Value  string
	Source string
}

type promptConfigResolution struct {
	Override                  state.ModelConfigRecord
	BaseModel                 configValue
	BaseReasoningEffort       configValue
	EffectiveModel            configValue
	EffectiveReasoningEffort  configValue
	EffectiveAccessMode       string
	EffectiveAccessModeSource string
}

func promptOverrideIsEmpty(value state.ModelConfigRecord) bool {
	return modelConfigRecordEmpty(value)
}

func modelConfigRecordEmpty(value state.ModelConfigRecord) bool {
	return strings.TrimSpace(value.Model) == "" &&
		strings.TrimSpace(value.ReasoningEffort) == "" &&
		strings.TrimSpace(value.AccessMode) == ""
}

func compactModelConfig(value state.ModelConfigRecord) state.ModelConfigRecord {
	value.AccessMode = agentproto.NormalizeAccessMode(value.AccessMode)
	if modelConfigRecordEmpty(value) {
		return state.ModelConfigRecord{}
	}
	return value
}

func compactPromptOverride(value state.ModelConfigRecord) state.ModelConfigRecord {
	return compactModelConfig(value)
}

func (s *Service) resolveFrozenPromptOverride(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) state.ModelConfigRecord {
	settings := state.EffectiveSurfaceCapabilitySettings(s.root, surface)
	backend := s.promptConfigBackend(inst, surface)
	if !state.BackendAcceptsFeishuPromptOverrides(backend) {
		if agentproto.NormalizeBackend(backend) == agentproto.BackendOpenCode {
			if promptOverrideIsEmpty(override) && surface != nil {
				override = settings.PromptOverride
			}
			return state.NormalizePromptOverrideForBackend(backend, compactPromptOverride(override))
		}
		return state.ModelConfigRecord{}
	}
	if s.surfaceUsesLocalRequestedPromptOverrides(surface) {
		if promptOverrideIsEmpty(override) && surface != nil {
			override = settings.PromptOverride
		}
		return state.NormalizePromptOverrideForBackend(backend, compactPromptOverride(override))
	}
	requestedOverride := compactPromptOverride(override)
	if promptOverrideIsEmpty(requestedOverride) && surface != nil {
		requestedOverride = compactPromptOverride(settings.PromptOverride)
	}
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return state.NormalizePromptOverrideForBackend(backend, state.ModelConfigRecord{
		Model:           resolution.EffectiveModel.Value,
		ReasoningEffort: requestedOverride.ReasoningEffort,
		AccessMode:      resolution.EffectiveAccessMode,
	})
}

func (s *Service) surfaceUsesLocalRequestedPromptOverrides(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	return state.IsVSCodeProductMode(state.EffectiveSurfaceCapabilitySettings(s.root, surface).Contract.ProductMode)
}

func surfaceUsesLocalRequestedPlanMode(contract state.SurfaceBackendContract) bool {
	contract = state.NormalizeSurfaceBackendContract(contract)
	return state.IsVSCodeProductMode(contract.ProductMode) || agentproto.NormalizeBackend(contract.Backend) == agentproto.BackendOpenCode
}

func (s *Service) freezePlanModeForPrompt(surface *state.SurfaceConsoleRecord) state.PlanModeSetting {
	if surface == nil {
		return ""
	}
	settings := state.EffectiveSurfaceCapabilitySettings(s.root, surface)
	if !state.BackendAcceptsFeishuPlanOverride(settings.Contract.Backend) {
		return ""
	}
	if surfaceUsesLocalRequestedPlanMode(settings.Contract) && !settings.PlanModeOverrideSet {
		return ""
	}
	return state.NormalizePlanModeSetting(settings.PlanMode)
}

func (s *Service) resolvePromptConfig(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) promptConfigResolution {
	backend := s.promptConfigBackend(inst, surface)
	settings := state.EffectiveSurfaceCapabilitySettings(s.root, surface)
	if surface != nil && promptOverrideIsEmpty(override) {
		override = settings.PromptOverride
	}
	override = state.NormalizePromptOverrideForBackend(backend, compactPromptOverride(override))
	baseModel, baseEffort, baseAccess := s.resolveBasePromptConfig(inst, surface, threadID, cwd)
	if agentproto.NormalizeBackend(backend) == agentproto.BackendClaude {
		override.Model = ""
		baseModel = configValue{Source: "profile"}
		baseEffort = configValue{
			Value:  s.claudeProfileReasoningEffort(s.promptConfigClaudeProfileID(inst, surface)),
			Source: "profile",
		}
	} else if agentproto.NormalizeBackend(backend) == agentproto.BackendCodex {
		if profile, ok := s.surfaceCodexProfileSummary(surface); ok {
			profile = normalizeCodexProfileSummary(profile)
			if profile.Kind == state.CodexProfileKindAPI {
				if model := strings.TrimSpace(profile.Model); model != "" {
					baseModel = configValue{Value: model, Source: "profile"}
				}
				if effort := normalizeModelReasoningEffort(profile.ReasoningEffort); effort != "" {
					baseEffort = configValue{Value: effort, Source: "profile"}
				}
			}
			if fixedModel, fixed := fixedCodexAPIProfileModel(profile); fixed {
				override.Model = ""
				baseModel = configValue{Value: fixedModel, Source: "profile"}
				baseEffort = configValue{Value: fixedCodexAPIProfileReasoning(profile), Source: "profile"}
			}
		}
	}
	effectiveModel := baseModel
	if override.Model != "" {
		effectiveModel = configValue{Value: override.Model, Source: "surface_override"}
	} else if effectiveModel.Value == "" {
		if defaultValue := defaultPromptModelForBackend(backend); defaultValue != "" {
			effectiveModel = configValue{Value: defaultValue, Source: "surface_default"}
		}
	}
	effectiveEffort := baseEffort
	if override.ReasoningEffort != "" {
		effectiveEffort = configValue{Value: override.ReasoningEffort, Source: "surface_override"}
	} else if effectiveEffort.Value == "" {
		if defaultValue := defaultPromptReasoningEffortForBackend(backend); defaultValue != "" {
			effectiveEffort = configValue{Value: defaultValue, Source: "surface_default"}
		}
	}
	effectiveAccessModeSource := ""
	effectiveAccessMode := ""
	if state.BackendAcceptsFeishuPromptOverrides(backend) {
		effectiveAccessModeSource = "surface_default"
		effectiveAccessMode = agentproto.AccessModeFullAccess
	}
	if agentproto.NormalizeAccessMode(override.AccessMode) != "" {
		effectiveAccessMode = override.AccessMode
		effectiveAccessModeSource = "surface_override"
	} else if agentproto.NormalizeAccessMode(baseAccess.Value) != "" {
		effectiveAccessMode = baseAccess.Value
		effectiveAccessModeSource = baseAccess.Source
	}
	return promptConfigResolution{
		Override:                  override,
		BaseModel:                 baseModel,
		BaseReasoningEffort:       baseEffort,
		EffectiveModel:            effectiveModel,
		EffectiveReasoningEffort:  effectiveEffort,
		EffectiveAccessMode:       effectiveAccessMode,
		EffectiveAccessModeSource: effectiveAccessModeSource,
	}
}

func (s *Service) promptConfigBackend(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord) agentproto.Backend {
	if surface != nil {
		return s.surfaceWorkspaceDefaultsBackend(surface, inst)
	}
	if inst != nil {
		return state.EffectiveInstanceBackend(inst)
	}
	return agentproto.BackendCodex
}

func (s *Service) promptConfigClaudeProfileID(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord) string {
	if surface != nil {
		return s.surfaceClaudeProfileID(surface)
	}
	if inst != nil {
		return inst.ClaudeProfileID
	}
	return state.DefaultClaudeProfileID
}

func defaultPromptModelForBackend(backend agentproto.Backend) string {
	if agentproto.NormalizeBackend(backend) == agentproto.BackendCodex {
		return defaultModel
	}
	return ""
}

func defaultPromptReasoningEffortForBackend(backend agentproto.Backend) string {
	if agentproto.NormalizeBackend(backend) == agentproto.BackendCodex {
		return defaultReasoningEffort
	}
	return ""
}

func (s *Service) resolveBasePromptConfig(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string) (configValue, configValue, configValue) {
	model := configValue{Source: "unknown"}
	effort := configValue{Source: "unknown"}
	access := configValue{Source: "unknown"}
	if inst == nil {
		return model, effort, access
	}
	backend := s.promptConfigBackend(inst, surface)
	claudeHeadless := agentproto.NormalizeBackend(backend) == agentproto.BackendClaude
	if thread := inst.Threads[threadID]; thread != nil {
		if cwd == "" {
			cwd = thread.CWD
		}
		if thread.ExplicitModel != "" {
			model = configValue{Value: thread.ExplicitModel, Source: "thread"}
		} else if thread.ThreadSettings != nil && strings.TrimSpace(thread.ThreadSettings.Model) != "" {
			model = configValue{Value: strings.TrimSpace(thread.ThreadSettings.Model), Source: "thread"}
		}
		if thread.ExplicitReasoningEffort != "" {
			effort = configValue{Value: thread.ExplicitReasoningEffort, Source: "thread"}
		} else if thread.ThreadSettings != nil && strings.TrimSpace(thread.ThreadSettings.ReasoningEffort) != "" {
			effort = configValue{Value: strings.TrimSpace(thread.ThreadSettings.ReasoningEffort), Source: "thread"}
		}
		if claudeHeadless {
			if observed := agentproto.NormalizeAccessMode(thread.ObservedAccessMode); observed != "" {
				access = configValue{Value: observed, Source: "thread"}
			}
		}
	}
	if defaults, ok := s.resolveWorkspaceDefaults(inst, surface, cwd); ok {
		if model.Value == "" && defaults.Model != "" {
			model = configValue{Value: defaults.Model, Source: "workspace_default"}
		}
		if effort.Value == "" && defaults.ReasoningEffort != "" {
			effort = configValue{Value: defaults.ReasoningEffort, Source: "workspace_default"}
		}
		if !claudeHeadless && defaults.AccessMode != "" {
			access = configValue{Value: defaults.AccessMode, Source: "workspace_default"}
		}
	}
	cwd = state.NormalizeWorkspaceKey(cwd)
	if cwd != "" && s.surfaceUsesLocalRequestedPromptOverrides(surface) {
		if defaults, ok := inst.CWDDefaults[cwd]; ok {
			if model.Value == "" && defaults.Model != "" {
				model = configValue{Value: defaults.Model, Source: "cwd_default"}
			}
			if effort.Value == "" && defaults.ReasoningEffort != "" {
				effort = configValue{Value: defaults.ReasoningEffort, Source: "cwd_default"}
			}
			if !claudeHeadless && access.Value == "" && defaults.AccessMode != "" {
				access = configValue{Value: defaults.AccessMode, Source: "cwd_default"}
			}
		}
	}
	return model, effort, access
}

func (s *Service) resolveWorkspaceDefaults(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, cwd string) (state.ModelConfigRecord, bool) {
	if inst == nil || surface == nil || !state.IsHeadlessProductMode(s.normalizeSurfaceProductMode(surface)) {
		return state.ModelConfigRecord{}, false
	}
	workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
	if workspaceKey == "" {
		workspaceKey = state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot, cwd)
	}
	if workspaceKey == "" || s.root == nil || len(s.root.WorkspaceDefaults) == 0 {
		return state.ModelConfigRecord{}, false
	}
	contract := s.surfaceWorkspaceDefaultsContract(surface, inst)
	defaultsKey := s.workspaceDefaultsStorageKey(workspaceKey, contract)
	if defaultsKey == "" {
		return state.ModelConfigRecord{}, false
	}
	defaults, ok := s.root.WorkspaceDefaults[defaultsKey]
	defaults = compactModelConfig(defaults)
	if ok && !modelConfigRecordEmpty(defaults) {
		return defaults, true
	}
	return state.ModelConfigRecord{}, false
}

func (s *Service) updateWorkspaceDefaults(workspaceKey string, contract state.InstanceBackendContract, apply func(*state.ModelConfigRecord)) {
	workspaceKey = state.ResolveWorkspaceKey(workspaceKey)
	contract = state.NormalizeObservedInstanceBackendContract(contract)
	defaultsKey := s.workspaceDefaultsStorageKey(workspaceKey, contract)
	if defaultsKey == "" || apply == nil || s.root == nil {
		return
	}
	if s.root.WorkspaceDefaults == nil {
		s.root.WorkspaceDefaults = map[string]state.ModelConfigRecord{}
	}
	current := compactModelConfig(s.root.WorkspaceDefaults[defaultsKey])
	apply(&current)
	current = compactModelConfig(current)
	if modelConfigRecordEmpty(current) {
		delete(s.root.WorkspaceDefaults, defaultsKey)
		return
	}
	s.root.WorkspaceDefaults[defaultsKey] = current
}
