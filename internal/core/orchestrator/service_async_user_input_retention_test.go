package orchestrator

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestAsyncUserInputSubmissionRetainsOnlyReplayTombstone(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	event := asyncQuestionEvent("retired")
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	action := asyncAnswerAction(page, 0)
	svc.ApplySurfaceAction(action)
	record := svc.surfaceUIRuntimeState(surface).AsyncUserInputs[action.Request.RequestID]
	if record == nil || !record.Submitted || record.Questions != nil || record.Answers != nil || record.Context != "" {
		t.Fatalf("submitted question retained its full payload: %#v", record)
	}
	if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 0 {
		t.Fatalf("a replay must not reopen a submitted question: %#v", events)
	}
}

func TestAsyncUserInputExpiresOnTickAndStillRejectsReplay(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	event := asyncQuestionEvent("expired")
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	action := asyncAnswerAction(page, 0)
	now := svc.now().Add(asyncUserInputDraftTTL)
	svc.now = func() time.Time { return now }
	svc.Tick(now)
	runtime := svc.surfaceUIRuntimeState(surface)
	if record := runtime.AsyncUserInputs[action.Request.RequestID]; record == nil || !record.Submitted || record.Questions != nil {
		t.Fatalf("idle tick must release expired question payload: %#v", record)
	}
	if containsAsyncCommand(svc.ApplySurfaceAction(action)) {
		t.Fatal("expired question dispatched an answer")
	}
	if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 0 {
		t.Fatalf("expired question replay reopened its card: %#v", events)
	}
	svc.Tick(now.Add(asyncUserInputSeenTTL))
	if len(runtime.AsyncUserInputs) != 0 {
		t.Fatal("idle tick did not expire replay tombstones")
	}
}

func TestAsyncUserInputRetentionBoundsDraftsAndReplayRecords(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	var firstAction control.Action
	var lastAction control.Action
	for i := 0; i < asyncUserInputMaxRecords+20; i++ {
		page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent(fmt.Sprintf("bounded-%03d", i))))
		lastAction = asyncAnswerAction(page, 0)
		if i == 0 {
			firstAction = lastAction
		}
	}
	runtime := svc.surfaceUIRuntimeState(surface)
	drafts := 0
	for _, record := range runtime.AsyncUserInputs {
		if !record.Submitted {
			drafts++
		} else if record.Questions != nil || record.Answers != nil || record.Context != "" {
			t.Fatal("capacity retirement retained a full draft")
		}
	}
	if drafts != asyncUserInputMaxDrafts || len(runtime.AsyncUserInputs) != asyncUserInputMaxRecords {
		t.Fatalf("retention exceeded bounds: drafts=%d records=%d", drafts, len(runtime.AsyncUserInputs))
	}
	if containsAsyncCommand(svc.ApplySurfaceAction(firstAction)) {
		t.Fatal("evicted old card still dispatched")
	}
	if command := firstCodexCommand(svc.ApplySurfaceAction(lastAction), agentproto.CommandTurnSteer); command == nil {
		t.Fatal("capacity cleanup made newest question unanswerable")
	}
}
