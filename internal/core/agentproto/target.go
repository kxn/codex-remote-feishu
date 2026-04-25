package agentproto

import "strings"

type PromptExecutionMode string

const (
	PromptExecutionModeResumeExisting PromptExecutionMode = "resume_existing"
	PromptExecutionModeStartNew       PromptExecutionMode = "start_new"
	PromptExecutionModeStartEphemeral PromptExecutionMode = "start_ephemeral"
	PromptExecutionModeForkEphemeral  PromptExecutionMode = "fork_ephemeral"
)

func NormalizePromptExecutionMode(value PromptExecutionMode) PromptExecutionMode {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "resume_existing", "resume", "existing", "thread_resume":
		return PromptExecutionModeResumeExisting
	case "start_new", "start", "new", "new_thread", "thread_start":
		return PromptExecutionModeStartNew
	case "start_ephemeral", "ephemeral_start":
		return PromptExecutionModeStartEphemeral
	case "fork_ephemeral", "fork", "thread_fork":
		return PromptExecutionModeForkEphemeral
	default:
		return ""
	}
}

func (target Target) EffectivePromptExecutionMode() PromptExecutionMode {
	if mode := NormalizePromptExecutionMode(target.ExecutionMode); mode != "" {
		return mode
	}
	if strings.TrimSpace(target.SourceThreadID) != "" {
		return PromptExecutionModeForkEphemeral
	}
	if strings.TrimSpace(target.ThreadID) != "" {
		return PromptExecutionModeResumeExisting
	}
	if target.InternalHelper {
		return PromptExecutionModeStartEphemeral
	}
	if target.CreateThreadIfMissing {
		return PromptExecutionModeStartNew
	}
	return PromptExecutionModeStartNew
}

type SurfaceBindingPolicy string

const (
	SurfaceBindingPolicyFollowExecutionThread SurfaceBindingPolicy = "follow_execution_thread"
	SurfaceBindingPolicyKeepSurfaceSelection  SurfaceBindingPolicy = "keep_surface_selection"
)

func NormalizeSurfaceBindingPolicy(value SurfaceBindingPolicy) SurfaceBindingPolicy {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "follow_execution_thread", "follow_execution", "follow":
		return SurfaceBindingPolicyFollowExecutionThread
	case "keep_surface_selection", "keep_selection":
		return SurfaceBindingPolicyKeepSurfaceSelection
	default:
		return ""
	}
}

func EffectiveSurfaceBindingPolicy(value SurfaceBindingPolicy) SurfaceBindingPolicy {
	if normalized := NormalizeSurfaceBindingPolicy(value); normalized != "" {
		return normalized
	}
	return SurfaceBindingPolicyFollowExecutionThread
}
