package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerThreadLifecycleNotificationsProduceStateEvents(t *testing.T) {
	tr := NewTranslator("inst-1")

	cases := []struct {
		name   string
		raw    string
		action agentproto.ThreadLifecycleAction
	}{
		{name: "archived", raw: `{"method":"thread/archived","params":{"threadId":"thread-1"}}`, action: agentproto.ThreadLifecycleArchived},
		{name: "unarchived", raw: `{"method":"thread/unarchived","params":{"threadId":"thread-1"}}`, action: agentproto.ThreadLifecycleUnarchived},
		{name: "deleted", raw: `{"method":"thread/deleted","params":{"threadId":"thread-1"}}`, action: agentproto.ThreadLifecycleDeleted},
		{name: "closed", raw: `{"method":"thread/closed","params":{"threadId":"thread-1"}}`, action: agentproto.ThreadLifecycleClosed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tr.ObserveServer([]byte(tc.raw))
			if err != nil {
				t.Fatalf("observe lifecycle: %v", err)
			}
			if len(result.Events) != 1 {
				t.Fatalf("expected one lifecycle event, got %#v", result.Events)
			}
			event := result.Events[0]
			if event.Kind != agentproto.EventThreadLifecycleUpdated {
				t.Fatalf("unexpected event kind: %#v", event)
			}
			if event.ThreadID != "thread-1" || event.ThreadLifecycle == nil || event.ThreadLifecycle.Action != tc.action {
				t.Fatalf("unexpected lifecycle event: %#v", event)
			}
		})
	}
}

func TestObserveServerThreadGoalAndSettingsNotificationsProduceStateEvents(t *testing.T) {
	tr := NewTranslator("inst-1")

	goalResult, err := tr.ObserveServer([]byte(`{"method":"thread/goal/updated","params":{"threadId":"thread-1","turnId":"turn-1","goal":{"objective":"ship it","status":"active","tokenBudget":1200,"tokensUsed":345,"timeUsedSeconds":67}}}`))
	if err != nil {
		t.Fatalf("observe goal updated: %v", err)
	}
	if len(goalResult.Events) != 1 {
		t.Fatalf("expected one goal event, got %#v", goalResult.Events)
	}
	goalEvent := goalResult.Events[0]
	if goalEvent.Kind != agentproto.EventThreadGoalUpdated || goalEvent.ThreadGoal == nil {
		t.Fatalf("unexpected goal event: %#v", goalEvent)
	}
	if goalEvent.ThreadGoal.Objective != "ship it" || goalEvent.ThreadGoal.Status != "active" || goalEvent.ThreadGoal.TokenBudget != 1200 || goalEvent.ThreadGoal.TokensUsed != 345 || goalEvent.ThreadGoal.TimeUsedSeconds != 67 {
		t.Fatalf("unexpected goal payload: %#v", goalEvent.ThreadGoal)
	}

	clearedResult, err := tr.ObserveServer([]byte(`{"method":"thread/goal/cleared","params":{"threadId":"thread-1"}}`))
	if err != nil {
		t.Fatalf("observe goal cleared: %v", err)
	}
	if len(clearedResult.Events) != 1 || clearedResult.Events[0].ThreadGoal == nil || !clearedResult.Events[0].ThreadGoal.Cleared {
		t.Fatalf("expected cleared goal event, got %#v", clearedResult.Events)
	}

	settingsResult, err := tr.ObserveServer([]byte(`{"method":"thread/settings/updated","params":{"threadId":"thread-1","settings":{"model":"gpt-5.4","reasoning_effort":"high","approvalPolicy":"on-request","sandbox":"workspace-write"}}}`))
	if err != nil {
		t.Fatalf("observe settings updated: %v", err)
	}
	if len(settingsResult.Events) != 1 {
		t.Fatalf("expected one settings event, got %#v", settingsResult.Events)
	}
	settingsEvent := settingsResult.Events[0]
	if settingsEvent.Kind != agentproto.EventThreadSettingsUpdated || settingsEvent.ThreadSettings == nil {
		t.Fatalf("unexpected settings event: %#v", settingsEvent)
	}
	if settingsEvent.ThreadSettings.Model != "gpt-5.4" || settingsEvent.ThreadSettings.ReasoningEffort != "high" || settingsEvent.ThreadSettings.ApprovalPolicy != "on-request" || settingsEvent.ThreadSettings.Sandbox != "workspace-write" {
		t.Fatalf("unexpected settings payload: %#v", settingsEvent.ThreadSettings)
	}
}
