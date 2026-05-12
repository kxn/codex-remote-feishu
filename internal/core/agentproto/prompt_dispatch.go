package agentproto

import "strings"

type PromptDispatchPlan struct {
	ExecutionMode        PromptExecutionMode
	ExecutionThreadID    string
	SourceThreadID       string
	SurfaceBindingPolicy SurfaceBindingPolicy
	CWD                  string
	InternalHelper       bool
}

func NormalizePromptDispatchPlan(plan PromptDispatchPlan) PromptDispatchPlan {
	plan.ExecutionMode = NormalizePromptExecutionMode(plan.ExecutionMode)
	plan.ExecutionThreadID = strings.TrimSpace(plan.ExecutionThreadID)
	plan.SourceThreadID = strings.TrimSpace(plan.SourceThreadID)
	plan.SurfaceBindingPolicy = EffectiveSurfaceBindingPolicy(plan.SurfaceBindingPolicy)
	plan.CWD = strings.TrimSpace(plan.CWD)
	if plan.ExecutionMode == "" {
		switch {
		case plan.SourceThreadID != "":
			plan.ExecutionMode = PromptExecutionModeForkEphemeral
		case plan.ExecutionThreadID != "":
			plan.ExecutionMode = PromptExecutionModeResumeExisting
		case plan.InternalHelper:
			plan.ExecutionMode = PromptExecutionModeStartEphemeral
		default:
			plan.ExecutionMode = PromptExecutionModeStartNew
		}
	}
	return plan
}

func PromptDispatchPlanFromTarget(target Target) PromptDispatchPlan {
	return NormalizePromptDispatchPlan(PromptDispatchPlan{
		ExecutionMode:        target.ExecutionMode,
		ExecutionThreadID:    target.ThreadID,
		SourceThreadID:       target.SourceThreadID,
		SurfaceBindingPolicy: target.SurfaceBindingPolicy,
		CWD:                  target.CWD,
		InternalHelper:       target.InternalHelper,
	})
}

func DefaultPromptDispatchPlanForExecutionThread(threadID string) PromptDispatchPlan {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return NormalizePromptDispatchPlan(PromptDispatchPlan{
			ExecutionMode:        PromptExecutionModeStartNew,
			SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
		})
	}
	return NormalizePromptDispatchPlan(PromptDispatchPlan{
		ExecutionMode:        PromptExecutionModeResumeExisting,
		ExecutionThreadID:    threadID,
		SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
	})
}

func (plan PromptDispatchPlan) LegacyTarget() Target {
	normalized := NormalizePromptDispatchPlan(plan)
	return Target{
		ExecutionMode:         normalized.ExecutionMode,
		SourceThreadID:        normalized.SourceThreadID,
		SurfaceBindingPolicy:  normalized.SurfaceBindingPolicy,
		ThreadID:              normalized.ExecutionThreadID,
		CreateThreadIfMissing: normalized.ExecutionMode == PromptExecutionModeStartNew,
		InternalHelper:        normalized.InternalHelper,
		CWD:                   normalized.CWD,
	}
}

func (plan PromptDispatchPlan) EffectiveExecutionThreadID() string {
	normalized := NormalizePromptDispatchPlan(plan)
	return firstNonEmptyDispatchString(normalized.ExecutionThreadID, normalized.SourceThreadID)
}

func (plan PromptDispatchPlan) EffectiveSourceThreadID() string {
	normalized := NormalizePromptDispatchPlan(plan)
	return firstNonEmptyDispatchString(normalized.SourceThreadID, normalized.ExecutionThreadID)
}

func (plan PromptDispatchPlan) EffectiveSurfaceThreadID() string {
	normalized := NormalizePromptDispatchPlan(plan)
	if normalized.SurfaceBindingPolicy == SurfaceBindingPolicyKeepSurfaceSelection {
		return normalized.EffectiveSourceThreadID()
	}
	return normalized.EffectiveExecutionThreadID()
}

func firstNonEmptyDispatchString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
