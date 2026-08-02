package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPromptDispatchProjectsFrozenCodexResumePolicy(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendCodex
	surface.AttachedInstanceID = "inst-1"
	item := &state.QueueItemRecord{
		ID:               "queue-1",
		SurfaceSessionID: "surface-1",
		CodexConnectionContract: &state.CodexConnectionContract{
			ConnectionContractID: "conn-team-r4",
			ModelProviderID:      "codex_remote_profile_team",
		},
		CodexThreadPolicy: &state.CodexThreadPolicy{
			ThreadPolicyID:     "thread-policy-r7",
			ModelMode:          state.CodexThreadValueExplicit,
			Model:              "gpt-5.5",
			ReviewModelMode:    state.CodexReviewModelExplicit,
			ReviewModel:        "gpt-5.5-review",
			ReasoningMode:      state.CodexThreadValueExplicit,
			ReasoningEffort:    "high",
			ContextMode:        state.CodexContextModeExtended,
			ContextWindow:      1000000,
			AutoCompactLimit:   900000,
			PreferenceRevision: 7,
		},
	}

	command := svc.promptSendCommandFromQueueItem(surface, item, "user-1", "msg-1")
	if command == nil || command.CodexResume == nil {
		t.Fatalf("expected codex resume policy on prompt command, got %#v", command)
	}
	if command.CodexResume.Mode != agentproto.CodexResumeApplyTargetProfile ||
		command.CodexResume.ConnectionContractID != "conn-team-r4" ||
		command.CodexResume.ThreadPolicyID != "thread-policy-r7" ||
		command.CodexResume.ModelProviderID != "codex_remote_profile_team" ||
		command.CodexResume.Model != "gpt-5.5" ||
		command.CodexResume.ReasoningEffort != "high" ||
		command.CodexResume.ContextWindow != 1000000 {
		t.Fatalf("unexpected codex resume policy: %#v", command.CodexResume)
	}
}

func TestPromptDispatchPreservesObservedThreadSettingsForSameCodexConnection(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		Backend:       agentproto.BackendCodex,
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CodexEffectiveThread: &agentproto.CodexEffectiveThreadContract{
					ResumeMode:           agentproto.CodexResumeApplyTargetProfile,
					ConnectionContractID: "conn-team-r4",
					ModelProviderID:      "codex_remote_profile_team",
					ModelMode:            agentproto.CodexThreadValueExplicit,
					Model:                "observed-model",
					ReasoningMode:        agentproto.CodexThreadValueExplicit,
					ReasoningEffort:      "medium",
				},
			},
		},
	})
	surface := svc.ensureSurface(control.Action{SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendCodex
	surface.AttachedInstanceID = "inst-1"
	item := &state.QueueItemRecord{
		ID:               "queue-1",
		SurfaceSessionID: "surface-1",
		CodexConnectionContract: &state.CodexConnectionContract{
			ConnectionContractID: "conn-team-r4",
			ModelProviderID:      "codex_remote_profile_team",
		},
		CodexThreadPolicy: &state.CodexThreadPolicy{
			ThreadPolicyID:     "thread-policy-r7",
			ModelMode:          state.CodexThreadValueExplicit,
			Model:              "target-profile-model",
			ReasoningMode:      state.CodexThreadValueExplicit,
			ReasoningEffort:    "high",
			ContextMode:        state.CodexContextModeExtended,
			ContextWindow:      1000000,
			AutoCompactLimit:   900000,
			PreferenceRevision: 7,
		},
		FrozenDispatchPlan: agentproto.PromptDispatchPlan{
			ExecutionThreadID: "thread-1",
			CWD:               "/data/dl/repo",
		},
	}

	command := svc.promptSendCommandFromQueueItem(surface, item, "user-1", "msg-1")
	if command == nil || command.CodexResume == nil {
		t.Fatalf("expected codex resume policy on prompt command, got %#v", command)
	}
	if command.CodexResume.Mode != agentproto.CodexResumePreserveThreadSettings {
		t.Fatalf("expected preserve policy for same connection, got %#v", command.CodexResume)
	}
	if command.CodexResume.ModelMode != agentproto.CodexThreadValuePreservedObserved ||
		command.CodexResume.Model != "observed-model" ||
		command.CodexResume.ReasoningMode != agentproto.CodexThreadValuePreservedObserved ||
		command.CodexResume.ReasoningEffort != "medium" {
		t.Fatalf("expected observed model/reasoning to be preserved, got %#v", command.CodexResume)
	}
	if command.CodexResume.ContextWindow != 1000000 || command.CodexResume.AutoCompactLimit != 900000 {
		t.Fatalf("expected current profile context preference to remain requested, got %#v", command.CodexResume)
	}
}

func TestCompactCommandProjectsSurfaceCodexResumePolicy(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 10, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendCodex
	surface.SelectedThreadID = "thread-1"
	surface.CodexConnectionContract = &state.CodexConnectionContract{
		ConnectionContractID: "conn-team-r4",
		ModelProviderID:      "codex_remote_profile_team",
	}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:  "thread-policy-r7",
		ModelMode:       state.CodexThreadValueDefault,
		ReviewModelMode: state.CodexReviewModelConfig,
		ReasoningMode:   state.CodexThreadValueDefault,
		ContextMode:     state.CodexContextModeDefault,
	}

	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionCompact, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	command := firstCodexCommand(events, agentproto.CommandThreadCompactStart)
	if command == nil || command.CodexResume == nil {
		t.Fatalf("expected codex resume policy on compact command, got %#v", events)
	}
	if command.CodexResume.ModelProviderID != "codex_remote_profile_team" || command.CodexResume.ModelMode != state.CodexThreadValueDefault {
		t.Fatalf("unexpected compact codex resume policy: %#v", command.CodexResume)
	}
}

func TestCompactCommandPreservesObservedThreadSettingsForSameCodexConnection(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 15, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendCodex
	surface.SelectedThreadID = "thread-1"
	surface.CodexConnectionContract = &state.CodexConnectionContract{
		ConnectionContractID: "conn-team-r4",
		ModelProviderID:      "codex_remote_profile_team",
	}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID:     "thread-policy-r7",
		ModelMode:          state.CodexThreadValueExplicit,
		Model:              "target-profile-model",
		ReasoningMode:      state.CodexThreadValueExplicit,
		ReasoningEffort:    "high",
		ContextMode:        state.CodexContextModeExtended,
		ContextWindow:      1000000,
		AutoCompactLimit:   900000,
		PreferenceRevision: 7,
	}
	inst := svc.root.Instances[surface.AttachedInstanceID]
	inst.Threads["thread-1"].CodexEffectiveThread = &agentproto.CodexEffectiveThreadContract{
		ConnectionContractID: "conn-team-r4",
		ModelProviderID:      "codex_remote_profile_team",
		Model:                "observed-model",
		ReasoningEffort:      "medium",
	}

	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionCompact, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	command := firstCodexCommand(events, agentproto.CommandThreadCompactStart)
	if command == nil || command.CodexResume == nil {
		t.Fatalf("expected codex resume policy on compact command, got %#v", events)
	}
	if command.CodexResume.Mode != agentproto.CodexResumePreserveThreadSettings ||
		command.CodexResume.Model != "observed-model" ||
		command.CodexResume.ReasoningEffort != "medium" ||
		command.CodexResume.ContextWindow != 1000000 {
		t.Fatalf("expected compact preserve policy with current context preference, got %#v", command.CodexResume)
	}
}

func TestTurnStartedStoresCodexEffectiveThreadContract(t *testing.T) {
	now := time.Date(2026, 8, 1, 10, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		Backend:       agentproto.BackendCodex,
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		CodexEffectiveThread: &agentproto.CodexEffectiveThreadContract{
			ResumeMode:             agentproto.CodexResumeApplyTargetProfile,
			ModelProviderID:        "codex_remote_profile_team",
			ModelMode:              agentproto.CodexThreadValueExplicit,
			Model:                  "gpt-5.5",
			ReasoningMode:          agentproto.CodexThreadValueExplicit,
			ReasoningEffort:        "high",
			RequestedContextWindow: 1000000,
			EffectiveContextWindow: 272000,
			ContextStatus:          agentproto.CodexContextPreferenceClamped,
		},
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil || thread.CodexEffectiveThread == nil {
		t.Fatalf("expected stored codex effective thread contract, got %#v", thread)
	}
	if thread.CodexEffectiveThread.ModelProviderID != "codex_remote_profile_team" ||
		thread.CodexEffectiveThread.EffectiveContextWindow != 272000 ||
		thread.CodexEffectiveThread.ContextStatus != agentproto.CodexContextPreferenceClamped {
		t.Fatalf("unexpected stored codex effective thread contract: %#v", thread.CodexEffectiveThread)
	}
}

func firstCodexCommand(events []eventcontract.Event, kind agentproto.CommandKind) *agentproto.Command {
	for i := range events {
		if events[i].Command != nil && events[i].Command.Kind == kind {
			return events[i].Command
		}
	}
	return nil
}
