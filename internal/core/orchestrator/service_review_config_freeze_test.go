package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexReviewCommandFreezesParentWorkspaceAndConfiguration(t *testing.T) {
	s, surface, cwd := newReviewSessionService(t)
	surface.PromptOverride = state.ModelConfigRecord{Model: "main-model", ReasoningEffort: "high", AccessMode: "confirm"}
	surface.CodexConnectionContract = &state.CodexConnectionContract{ConnectionContractID: "contract", ModelProviderID: "provider"}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{ThreadPolicyID: "policy", ModelMode: agentproto.CodexThreadValueExplicit, Model: "main-model", ReviewModelMode: agentproto.CodexReviewModelExplicit, ReviewModel: "review-model"}
	events := s.startReview(surface, reviewStartState{Ready: true, ParentThreadID: "thread-main", ThreadCWD: cwd, SourceMessageID: "review-source", Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("review did not start: %+v", events)
	}
	cmd := events[1].Command
	if cmd.Target.ThreadID != "thread-main" || cmd.Target.CWD != cwd || cmd.Overrides.Model != "main-model" || cmd.Overrides.ReasoningEffort != "high" || cmd.Overrides.AccessMode != "confirm" {
		t.Fatalf("review start was not frozen: %+v", cmd)
	}
	if cmd.CodexResume == nil || cmd.CodexResume.ReviewModelMode != agentproto.CodexReviewModelExplicit || cmd.CodexResume.ReviewModel != "review-model" || cmd.CodexResume.ModelProviderID != "provider" {
		t.Fatalf("review profile policy missing: %+v", cmd.CodexResume)
	}
	surface.PromptOverride.Model = "later"
	if cmd.Overrides.Model != "main-model" {
		t.Fatal("review intent changed after configuration update")
	}
}

func TestReviewFollowupKeepsIndependentThreadSourceWithoutInventingFork(t *testing.T) {
	s, _, cwd := newReviewSessionService(t)
	inst := s.Instance("inst-1")
	delete(inst.Threads, "independent-review")
	binding := &remoteTurnBinding{ThreadID: "independent-review", DispatchPlan: agentproto.PromptDispatchPlan{ExecutionMode: agentproto.PromptExecutionModeResumeExisting, ExecutionThreadID: "independent-review", SourceThreadID: "thread-main", Purpose: agentproto.PromptPurposeReview}}
	thread := s.materializeRemoteTurnThread(inst, "independent-review", cwd, binding, nil)
	if thread.ForkedFromID != "" || thread.Source == nil || thread.Source.Kind != agentproto.ThreadSourceKindReview || thread.Source.ParentThreadID != "thread-main" {
		t.Fatalf("incorrect independent source: %+v", thread)
	}
}
