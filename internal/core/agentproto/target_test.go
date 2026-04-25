package agentproto

import "testing"

func TestTargetEffectivePromptExecutionModePrefersExplicitMode(t *testing.T) {
	target := Target{
		ExecutionMode:         PromptExecutionModeForkEphemeral,
		ThreadID:              "thread-main",
		SourceThreadID:        "",
		CreateThreadIfMissing: true,
		InternalHelper:        true,
	}
	if got := target.EffectivePromptExecutionMode(); got != PromptExecutionModeForkEphemeral {
		t.Fatalf("expected explicit mode to win, got %q", got)
	}
}

func TestTargetEffectivePromptExecutionModeLegacyFallback(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		want   PromptExecutionMode
	}{
		{
			name:   "resume-existing-from-thread",
			target: Target{ThreadID: "thread-1"},
			want:   PromptExecutionModeResumeExisting,
		},
		{
			name:   "start-new-from-create-flag",
			target: Target{CreateThreadIfMissing: true},
			want:   PromptExecutionModeStartNew,
		},
		{
			name:   "start-ephemeral-from-internal-helper",
			target: Target{InternalHelper: true},
			want:   PromptExecutionModeStartEphemeral,
		},
		{
			name:   "fork-ephemeral-from-source-thread",
			target: Target{SourceThreadID: "thread-source"},
			want:   PromptExecutionModeForkEphemeral,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.target.EffectivePromptExecutionMode(); got != tt.want {
				t.Fatalf("unexpected mode: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestEffectiveSurfaceBindingPolicyDefaultsFollowExecution(t *testing.T) {
	if got := EffectiveSurfaceBindingPolicy(""); got != SurfaceBindingPolicyFollowExecutionThread {
		t.Fatalf("expected follow_execution_thread default, got %q", got)
	}
	if got := EffectiveSurfaceBindingPolicy("keep_surface_selection"); got != SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("expected keep_surface_selection, got %q", got)
	}
}
