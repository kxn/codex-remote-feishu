package agentproto

import "testing"

func TestPromptDispatchPlanFromTargetPrefersExplicitMode(t *testing.T) {
	plan := PromptDispatchPlanFromTarget(Target{
		ExecutionMode:         PromptExecutionModeForkEphemeral,
		ThreadID:              "thread-main",
		SourceThreadID:        "",
		CreateThreadIfMissing: true,
		InternalHelper:        true,
	})
	if plan.ExecutionMode != PromptExecutionModeForkEphemeral {
		t.Fatalf("expected explicit mode to win, got %#v", plan)
	}
	if plan.ExecutionThreadID != "thread-main" {
		t.Fatalf("expected explicit execution thread to be preserved, got %#v", plan)
	}
}

func TestTargetEffectivePromptExecutionModeLegacyFallback(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		want   PromptDispatchPlan
	}{
		{
			name:   "resume-existing-from-thread",
			target: Target{ThreadID: "thread-1"},
			want: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeResumeExisting,
				ExecutionThreadID:    "thread-1",
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
			},
		},
		{
			name:   "start-new-from-create-flag",
			target: Target{CreateThreadIfMissing: true},
			want: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeStartNew,
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
			},
		},
		{
			name:   "start-ephemeral-from-internal-helper",
			target: Target{InternalHelper: true},
			want: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeStartEphemeral,
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
				InternalHelper:       true,
			},
		},
		{
			name:   "fork-ephemeral-from-source-thread",
			target: Target{SourceThreadID: "thread-source"},
			want: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeForkEphemeral,
				SourceThreadID:       "thread-source",
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
			},
		},
		{
			name: "explicit-start-new-keeps-boundary-cleanup-in-plan-mode-only",
			target: Target{
				ExecutionMode:         PromptExecutionModeStartNew,
				ThreadID:              "thread-stale",
				CreateThreadIfMissing: true,
			},
			want: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeStartNew,
				ExecutionThreadID:    "thread-stale",
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PromptDispatchPlanFromTarget(tt.target)
			if got != tt.want {
				t.Fatalf("unexpected plan: got %#v want %#v", got, tt.want)
			}
			if mode := tt.target.EffectivePromptExecutionMode(); mode != tt.want.ExecutionMode {
				t.Fatalf("unexpected effective mode: got %q want %q", mode, tt.want.ExecutionMode)
			}
		})
	}
}

func TestPromptDispatchPlanLegacyTargetCarriesCanonicalSemantics(t *testing.T) {
	tests := []struct {
		name string
		plan PromptDispatchPlan
		want Target
	}{
		{
			name: "resume existing",
			plan: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeResumeExisting,
				ExecutionThreadID:    "thread-1",
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
				CWD:                  "/tmp/ws",
			},
			want: Target{
				ExecutionMode:         PromptExecutionModeResumeExisting,
				ThreadID:              "thread-1",
				SurfaceBindingPolicy:  SurfaceBindingPolicyFollowExecutionThread,
				CWD:                   "/tmp/ws",
				CreateThreadIfMissing: false,
			},
		},
		{
			name: "start new",
			plan: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeStartNew,
				SurfaceBindingPolicy: SurfaceBindingPolicyFollowExecutionThread,
			},
			want: Target{
				ExecutionMode:         PromptExecutionModeStartNew,
				SurfaceBindingPolicy:  SurfaceBindingPolicyFollowExecutionThread,
				CreateThreadIfMissing: true,
			},
		},
		{
			name: "fork retry keeps execution thread and source thread",
			plan: PromptDispatchPlan{
				ExecutionMode:        PromptExecutionModeForkEphemeral,
				ExecutionThreadID:    "thread-detour",
				SourceThreadID:       "thread-main",
				SurfaceBindingPolicy: SurfaceBindingPolicyKeepSurfaceSelection,
			},
			want: Target{
				ExecutionMode:         PromptExecutionModeForkEphemeral,
				ThreadID:              "thread-detour",
				SourceThreadID:        "thread-main",
				SurfaceBindingPolicy:  SurfaceBindingPolicyKeepSurfaceSelection,
				CreateThreadIfMissing: false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.plan.LegacyTarget(); got != tt.want {
				t.Fatalf("unexpected target: got %#v want %#v", got, tt.want)
			}
		})
	}
}

func TestPromptDispatchPlanEffectiveThreadHelpers(t *testing.T) {
	plan := NormalizePromptDispatchPlan(PromptDispatchPlan{
		ExecutionMode:        PromptExecutionModeForkEphemeral,
		ExecutionThreadID:    "thread-detour",
		SourceThreadID:       "thread-main",
		SurfaceBindingPolicy: SurfaceBindingPolicyKeepSurfaceSelection,
	})
	if got := plan.EffectiveExecutionThreadID(); got != "thread-detour" {
		t.Fatalf("EffectiveExecutionThreadID() = %q, want thread-detour", got)
	}
	if got := plan.EffectiveSourceThreadID(); got != "thread-main" {
		t.Fatalf("EffectiveSourceThreadID() = %q, want thread-main", got)
	}
	if got := plan.EffectiveSurfaceThreadID(); got != "thread-main" {
		t.Fatalf("EffectiveSurfaceThreadID() = %q, want thread-main", got)
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
