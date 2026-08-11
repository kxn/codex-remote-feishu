package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) applyThreadLifecycleUpdate(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	update := agentproto.NormalizeThreadLifecycleUpdate(event.ThreadLifecycle)
	if update == nil {
		update = agentproto.NormalizeThreadLifecycleUpdate(&agentproto.ThreadLifecycleUpdate{
			ThreadID: event.ThreadID,
			Action:   agentproto.NormalizeThreadLifecycleAction(string(event.Action)),
		})
	}
	if update == nil {
		return nil
	}
	thread := s.ensureThread(inst, update.ThreadID)
	thread.LifecycleState = agentproto.CloneThreadLifecycleUpdate(update)
	switch update.Action {
	case agentproto.ThreadLifecycleArchived:
		thread.Archived = true
	case agentproto.ThreadLifecycleUnarchived:
		thread.Archived = false
	case agentproto.ThreadLifecycleDeleted:
		thread.Archived = true
		s.clearDeletedThreadSelection(instanceID, update.ThreadID)
	case agentproto.ThreadLifecycleClosed:
		markThreadNotLoaded(thread)
	}
	return nil
}

func (s *Service) clearDeletedThreadSelection(instanceID, threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	for _, surface := range s.root.Surfaces {
		if surface == nil || surface.AttachedInstanceID != instanceID || surface.SelectedThreadID != threadID {
			continue
		}
		inst := s.root.Instances[instanceID]
		s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
			AttachedInstanceID: instanceID,
			RouteMode:          state.RouteModeUnbound,
		})
	}
}

func (s *Service) applyThreadGoalUpdate(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	update := agentproto.NormalizeThreadGoalUpdate(event.ThreadGoal)
	if update == nil {
		update = agentproto.NormalizeThreadGoalUpdate(&agentproto.ThreadGoalUpdate{
			ThreadID:        event.ThreadID,
			TurnID:          event.TurnID,
			Objective:       event.Name,
			Status:          event.Status,
			TokenBudget:     lookupIntMetadata(event.Metadata, "tokenBudget"),
			TokensUsed:      lookupIntMetadata(event.Metadata, "tokensUsed"),
			TimeUsedSeconds: lookupIntMetadata(event.Metadata, "timeUsedSeconds"),
		})
	}
	if update == nil {
		return nil
	}
	thread := s.ensureThread(inst, update.ThreadID)
	thread.ThreadGoal = agentproto.CloneThreadGoalUpdate(update)
	return nil
}

func (s *Service) applyThreadSettingsUpdate(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	update := agentproto.NormalizeThreadSettingsUpdate(event.ThreadSettings)
	if update == nil {
		update = agentproto.NormalizeThreadSettingsUpdate(&agentproto.ThreadSettingsUpdate{
			ThreadID:        event.ThreadID,
			Model:           event.Model,
			ReasoningEffort: event.ReasoningEffort,
			ApprovalPolicy:  event.AccessMode,
			Sandbox:         event.PlanMode,
		})
	}
	if update == nil {
		return nil
	}
	thread := s.ensureThread(inst, update.ThreadID)
	thread.ThreadSettings = agentproto.CloneThreadSettingsUpdate(update)
	if strings.TrimSpace(event.PlanMode) != "" {
		applyObservedPlanMode(thread, event.PlanMode)
	}
	return nil
}

func applyObservedPlanMode(thread *state.ThreadRecord, value string) {
	if thread == nil {
		return
	}
	raw := normalizeObservedPlanModeValue(value)
	if raw == "" {
		return
	}
	thread.ObservedPlanModeRaw = raw
	if mode, ok := observedPlanModeSetting(raw); ok {
		thread.ObservedPlanMode = mode
		return
	}
	thread.ObservedPlanMode = ""
}

func normalizeObservedPlanModeValue(value string) string {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "on", "plan":
		return string(state.PlanModeSettingOn)
	case "off", "build":
		return string(state.PlanModeSettingOff)
	default:
		return trimmed
	}
}

func observedPlanModeSetting(value string) (state.PlanModeSetting, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(state.PlanModeSettingOn), "plan":
		return state.PlanModeSettingOn, true
	case string(state.PlanModeSettingOff), "build":
		return state.PlanModeSettingOff, true
	default:
		return "", false
	}
}

func lookupIntMetadata(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
