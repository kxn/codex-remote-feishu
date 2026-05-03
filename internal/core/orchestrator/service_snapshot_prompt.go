package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) resolveNextPromptSummary(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, frozenThreadID, frozenCWD string, override state.ModelConfigRecord) control.PromptRouteSummary {
	if inst == nil || surface == nil {
		return control.PromptRouteSummary{}
	}
	threadID := frozenThreadID
	cwd := frozenCWD
	routeMode := surface.RouteMode
	createThread := false
	if threadID == "" && cwd == "" {
		threadID, cwd, routeMode, createThread = freezeRoute(inst, surface)
	} else {
		createThread = threadID == ""
	}
	if promptOverrideIsEmpty(override) {
		override = surface.PromptOverride
	}
	threadTitle := ""
	observedThreadPlanMode := ""
	if threadID != "" {
		thread := inst.Threads[threadID]
		threadTitle = displayThreadTitle(inst, thread, threadID)
		if thread != nil && strings.TrimSpace(string(thread.ObservedPlanMode)) != "" {
			observedThreadPlanMode = string(state.NormalizePlanModeSetting(thread.ObservedPlanMode))
		}
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
		EffectivePlanMode:              string(state.NormalizePlanModeSetting(surface.PlanMode)),
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
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return state.ModelConfigRecord{
		Model:           resolution.EffectiveModel.Value,
		ReasoningEffort: resolution.EffectiveReasoningEffort.Value,
		AccessMode:      resolution.EffectiveAccessMode,
	}
}

func (s *Service) resolvePromptConfig(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) promptConfigResolution {
	if surface != nil && promptOverrideIsEmpty(override) {
		override = surface.PromptOverride
	}
	override = compactPromptOverride(override)
	baseModel, baseEffort, baseAccess := s.resolveBasePromptConfig(inst, surface, threadID, cwd)
	backend := s.promptConfigBackend(inst, surface)
	if agentproto.NormalizeBackend(backend) == agentproto.BackendClaude {
		override.Model = ""
		baseModel = configValue{Source: "profile"}
		baseEffort = configValue{
			Value:  s.claudeProfileReasoningEffort(s.promptConfigClaudeProfileID(inst, surface)),
			Source: "profile",
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
	effectiveAccessModeSource := "surface_default"
	effectiveAccessMode := agentproto.AccessModeFullAccess
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
	if agentproto.NormalizeBackend(backend) == agentproto.BackendClaude {
		return ""
	}
	return defaultModel
}

func defaultPromptReasoningEffortForBackend(backend agentproto.Backend) string {
	if agentproto.NormalizeBackend(backend) == agentproto.BackendClaude {
		return ""
	}
	return defaultReasoningEffort
}

func (s *Service) resolveBasePromptConfig(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string) (configValue, configValue, configValue) {
	model := configValue{Source: "unknown"}
	effort := configValue{Source: "unknown"}
	access := configValue{Source: "unknown"}
	if inst == nil {
		return model, effort, access
	}
	s.backfillLegacyWorkspaceDefaults(inst)
	if thread := inst.Threads[threadID]; thread != nil {
		if cwd == "" {
			cwd = thread.CWD
		}
		if thread.ExplicitModel != "" {
			model = configValue{Value: thread.ExplicitModel, Source: "thread"}
		}
		if thread.ExplicitReasoningEffort != "" {
			effort = configValue{Value: thread.ExplicitReasoningEffort, Source: "thread"}
		}
	}
	if defaults, ok := s.resolveWorkspaceDefaults(inst, surface, cwd); ok {
		if model.Value == "" && defaults.Model != "" {
			model = configValue{Value: defaults.Model, Source: "workspace_default"}
		}
		if effort.Value == "" && defaults.ReasoningEffort != "" {
			effort = configValue{Value: defaults.ReasoningEffort, Source: "workspace_default"}
		}
		if defaults.AccessMode != "" {
			access = configValue{Value: defaults.AccessMode, Source: "workspace_default"}
		}
	}
	cwd = state.NormalizeWorkspaceKey(cwd)
	if cwd != "" {
		if defaults, ok := inst.CWDDefaults[cwd]; ok {
			if model.Value == "" && defaults.Model != "" {
				model = configValue{Value: defaults.Model, Source: "cwd_default"}
			}
			if effort.Value == "" && defaults.ReasoningEffort != "" {
				effort = configValue{Value: defaults.ReasoningEffort, Source: "cwd_default"}
			}
			if access.Value == "" && defaults.AccessMode != "" {
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
	legacyKey := s.legacyWorkspaceDefaultsStorageKey(workspaceKey, contract.Backend)
	if legacyKey == "" || legacyKey == defaultsKey {
		return state.ModelConfigRecord{}, false
	}
	defaults, ok = s.root.WorkspaceDefaults[legacyKey]
	defaults = compactModelConfig(defaults)
	if !ok || modelConfigRecordEmpty(defaults) {
		return state.ModelConfigRecord{}, false
	}
	return defaults, true
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
	if modelConfigRecordEmpty(current) {
		legacyKey := s.legacyWorkspaceDefaultsStorageKey(workspaceKey, contract.Backend)
		if legacyKey != "" && legacyKey != defaultsKey {
			current = compactModelConfig(s.root.WorkspaceDefaults[legacyKey])
		}
	}
	apply(&current)
	current = compactModelConfig(current)
	if modelConfigRecordEmpty(current) {
		delete(s.root.WorkspaceDefaults, defaultsKey)
		return
	}
	s.root.WorkspaceDefaults[defaultsKey] = current
}

func (s *Service) backfillLegacyWorkspaceDefaults(inst *state.InstanceRecord) {
	if inst == nil || len(inst.CWDDefaults) == 0 {
		return
	}
	workspaceKey := state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot)
	if workspaceKey == "" {
		return
	}
	candidates := legacyWorkspaceDefaultCandidates(workspaceKey, inst.CWDDefaults)
	if len(candidates) == 0 {
		return
	}
	s.updateWorkspaceDefaults(workspaceKey, state.ObservedInstanceBackendContract(inst), func(current *state.ModelConfigRecord) {
		for _, candidate := range candidates {
			if current.Model == "" && candidate.Model != "" {
				current.Model = candidate.Model
			}
			if current.ReasoningEffort == "" && candidate.ReasoningEffort != "" {
				current.ReasoningEffort = candidate.ReasoningEffort
			}
			if current.AccessMode == "" && candidate.AccessMode != "" {
				current.AccessMode = candidate.AccessMode
			}
		}
	})
}

func legacyWorkspaceDefaultCandidates(workspaceKey string, legacy map[string]state.ModelConfigRecord) []state.ModelConfigRecord {
	type candidate struct {
		key    string
		config state.ModelConfigRecord
		exact  bool
	}

	var candidates []candidate
	for key, config := range legacy {
		normalizedKey := state.ResolveWorkspaceKey(key)
		config = compactModelConfig(config)
		if normalizedKey == "" || modelConfigRecordEmpty(config) {
			continue
		}
		if normalizedKey != workspaceKey &&
			!strings.HasPrefix(normalizedKey, workspaceKey+"/") &&
			!strings.HasPrefix(workspaceKey, normalizedKey+"/") {
			continue
		}
		candidates = append(candidates, candidate{
			key:    normalizedKey,
			config: config,
			exact:  normalizedKey == workspaceKey,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].exact != candidates[j].exact {
			return candidates[i].exact
		}
		if len(candidates[i].key) != len(candidates[j].key) {
			return len(candidates[i].key) < len(candidates[j].key)
		}
		return candidates[i].key < candidates[j].key
	})
	merged := make([]state.ModelConfigRecord, 0, len(candidates))
	for _, candidate := range candidates {
		merged = append(merged, candidate.config)
	}
	return merged
}
