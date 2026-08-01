package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type codexPromptRouteAdjustment struct {
	DispatchPlan   agentproto.PromptDispatchPlan
	RouteMode      state.RouteMode
	FrozenOverride state.ModelConfigRecord
	Events         []eventcontract.Event
	Applied        bool
}

func (s *Service) maybeStartNewThreadForCodexModelGroupSwitch(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, dispatchPlan agentproto.PromptDispatchPlan, routeMode state.RouteMode, requestedOverride state.ModelConfigRecord, frozenOverride state.ModelConfigRecord, threadPolicy *state.CodexThreadPolicy) codexPromptRouteAdjustment {
	dispatchPlan = agentproto.NormalizePromptDispatchPlan(dispatchPlan)
	adjustment := codexPromptRouteAdjustment{
		DispatchPlan:   dispatchPlan,
		RouteMode:      routeMode,
		FrozenOverride: frozenOverride,
	}
	if surface == nil || inst == nil || !s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex {
		return adjustment
	}
	if dispatchPlan.ExecutionMode != agentproto.PromptExecutionModeResumeExisting ||
		dispatchPlan.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyFollowExecutionThread ||
		dispatchPlan.SourceThreadID != "" ||
		dispatchPlan.InternalHelper {
		return adjustment
	}
	threadID := dispatchPlan.ExecutionThreadID
	if threadID == "" {
		return adjustment
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return adjustment
	}
	sourceGroup := codexModelGroup(codexThreadModelForGroupSwitch(thread))
	targetModel, targetSource := s.codexTargetModelForGroupSwitch(inst, surface, threadID, dispatchPlan.CWD, requestedOverride, threadPolicy)
	targetGroup := codexModelGroup(targetModel)
	if sourceGroup == "" || targetGroup == "" || sourceGroup == targetGroup {
		return adjustment
	}
	cwd := strings.TrimSpace(dispatchPlan.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(thread.CWD)
	}
	if cwd == "" {
		cwd = strings.TrimSpace(inst.WorkspaceRoot)
	}
	if cwd == "" {
		return adjustment
	}
	if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID:   inst.InstanceID,
		RouteMode:            state.RouteModeNewThreadReady,
		PreparedThreadCWD:    cwd,
		PreparedFromThreadID: threadID,
	}) {
		return adjustment
	}
	surface.PreparedAt = s.now()
	dispatchPlan.ExecutionMode = agentproto.PromptExecutionModeStartNew
	dispatchPlan.ExecutionThreadID = ""
	dispatchPlan.SourceThreadID = ""
	dispatchPlan.CWD = cwd
	dispatchPlan.SurfaceBindingPolicy = agentproto.SurfaceBindingPolicyFollowExecutionThread
	frozenOverride = compactPromptOverride(frozenOverride)
	if targetSource == "codex_thread_policy" {
		frozenOverride.Model = ""
	}
	adjustment.DispatchPlan = agentproto.NormalizePromptDispatchPlan(dispatchPlan)
	adjustment.RouteMode = state.RouteModeNewThreadReady
	adjustment.FrozenOverride = compactPromptOverride(frozenOverride)
	adjustment.Events = append(adjustment.Events, s.threadSelectionEvents(surface, "", string(state.RouteModeNewThreadReady), preparedNewThreadSelectionTitle())...)
	adjustment.Events = append(adjustment.Events, codexModelGroupNewThreadNoticeEvent(surface))
	adjustment.Applied = true
	return adjustment
}

func codexThreadModelForGroupSwitch(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	if effective := thread.CodexEffectiveThread; effective != nil && strings.TrimSpace(effective.Model) != "" {
		return strings.TrimSpace(effective.Model)
	}
	if strings.TrimSpace(thread.ExplicitModel) != "" {
		return strings.TrimSpace(thread.ExplicitModel)
	}
	if settings := thread.ThreadSettings; settings != nil && strings.TrimSpace(settings.Model) != "" {
		return strings.TrimSpace(settings.Model)
	}
	if reroute := thread.LastModelReroute; reroute != nil && strings.TrimSpace(reroute.ToModel) != "" {
		return strings.TrimSpace(reroute.ToModel)
	}
	return ""
}

func (s *Service) codexProfileModelGroup(profile state.CodexProfileSummary) (string, bool) {
	profile = normalizeCodexProfileSummary(profile)
	if strings.TrimSpace(profile.ID) == "" {
		return "", false
	}
	if model, fixed := fixedCodexAPIProfileModel(profile); fixed {
		group := codexModelGroup(model)
		return group, group != ""
	}
	switch profile.Kind {
	case state.CodexProfileKindNative, state.CodexProfileKindOAuth, state.CodexProfileKindAPI:
		return "gpt", true
	default:
		return "", false
	}
}

func (s *Service) codexCurrentThreadModelGroupForSwitch(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) (string, bool) {
	threadID = strings.TrimSpace(threadID)
	if threadID != "" && inst != nil {
		if group := codexModelGroup(codexThreadModelForGroupSwitch(inst.Threads[threadID])); group != "" {
			return group, true
		}
	}
	if inst != nil {
		if group := codexThreadPolicyModelGroup(inst.CodexThreadPolicy); group != "" {
			return group, true
		}
		if profile, ok := s.codexProfileSummaryByID(instCodexProfileID(inst)); ok {
			if group, known := s.codexProfileModelGroup(profile); known {
				return group, true
			}
		}
	}
	if surface != nil {
		if group := codexThreadPolicyModelGroup(surface.CodexThreadPolicy); group != "" {
			return group, true
		}
		if profile, ok := s.surfaceCodexProfileSummary(surface); ok {
			return s.codexProfileModelGroup(profile)
		}
	}
	return "", false
}

func (s *Service) codexTargetModelForGroupSwitch(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, requestedOverride state.ModelConfigRecord, threadPolicy *state.CodexThreadPolicy) (string, string) {
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, requestedOverride)
	threadPolicy = state.CloneCodexThreadPolicy(threadPolicy)
	if threadPolicy != nil && strings.TrimSpace(threadPolicy.ModelMode) == state.CodexThreadValueExplicit && strings.TrimSpace(threadPolicy.Model) != "" {
		return strings.TrimSpace(threadPolicy.Model), "codex_thread_policy"
	}
	if resolution.EffectiveModel.Source != "thread" && strings.TrimSpace(resolution.EffectiveModel.Value) != "" {
		return strings.TrimSpace(resolution.EffectiveModel.Value), resolution.EffectiveModel.Source
	}
	if threadPolicy != nil {
		if strings.TrimSpace(threadPolicy.ModelMode) == state.CodexThreadValueDefault {
			return defaultPromptModelForBackend(agentproto.BackendCodex), "codex_thread_policy"
		}
	}
	return "", ""
}

func codexThreadPolicyModelGroup(policy *state.CodexThreadPolicy) string {
	policy = state.CloneCodexThreadPolicy(policy)
	if policy == nil {
		return ""
	}
	switch strings.TrimSpace(policy.ModelMode) {
	case state.CodexThreadValueExplicit:
		return codexModelGroup(policy.Model)
	case state.CodexThreadValueDefault:
		return codexModelGroup(defaultPromptModelForBackend(agentproto.BackendCodex))
	default:
		return ""
	}
}

func instCodexProfileID(inst *state.InstanceRecord) string {
	if inst == nil {
		return ""
	}
	if contract := state.CloneCodexConnectionContract(inst.CodexConnectionContract); contract != nil && strings.TrimSpace(contract.ProfileRef.ID) != "" {
		return strings.TrimSpace(contract.ProfileRef.ID)
	}
	return state.CodexProfileIDFromLegacyProviderID(inst.CodexProviderID)
}

func codexModelGroup(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return ""
	}
	for _, prefix := range []string{"gpt", "o1", "o3", "o4", "codex-"} {
		if strings.HasPrefix(model, prefix) {
			return "gpt"
		}
	}
	return "non_gpt"
}

func (s *Service) codexProfileSwitchStartsNewThread(surface *state.SurfaceConsoleRecord, continuation headlessContractSwitchContinuation, target state.CodexProfileSummary) bool {
	threadID := strings.TrimSpace(continuation.Attempt.ThreadID)
	if surface == nil || threadID == "" {
		return false
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	sourceGroup, sourceKnown := s.codexCurrentThreadModelGroupForSwitch(surface, inst, threadID)
	targetGroup, targetKnown := s.codexProfileModelGroup(target)
	return sourceKnown && targetKnown && sourceGroup != "" && targetGroup != "" && sourceGroup != targetGroup
}

func (s *Service) codexProfileSwitchNewThreadContinuation(surface *state.SurfaceConsoleRecord, workspaceKey string, previous headlessContractSwitchContinuation) headlessContractSwitchContinuation {
	next := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, agentproto.BackendCodex, true)
	next.RestartManagedNow = previous.RestartManagedNow
	next.RestartInstanceID = previous.RestartInstanceID
	if next.Attempt.ThreadCWD == "" {
		next.Attempt.ThreadCWD = strings.TrimSpace(firstNonEmpty(previous.Attempt.ThreadCWD, next.Attempt.WorkspaceKey))
	}
	return next
}

func codexModelGroupNewThreadNoticeEvent(surface *state.SurfaceConsoleRecord) eventcontract.Event {
	return surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: control.Notice{
			Code: "codex_model_group_new_thread",
			Text: "当前 Profile 与原会话属于不同模型组，已自动新建会话，避免旧会话历史导致上游请求失败。",
		}},
		eventcontract.EventMeta{},
	)
}
