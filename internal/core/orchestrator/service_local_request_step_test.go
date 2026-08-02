package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRespondRequestUserInputStepNavigationRefreshesPromptInline(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-nav-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.5"}}},
				{"id": "notes", "header": "备注", "question": "补充说明"},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request:          testRequestAction("req-ui-nav-1", "", "step_next", nil, 0),
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected next-step action to refresh current card inline, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.CurrentQuestionIndex != 1 || prompt.RequestRevision != 2 {
		t.Fatalf("expected next-step action to advance current question, got %#v", prompt)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request:          testRequestAction("req-ui-nav-1", "", "step_previous", nil, 0),
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected previous-step action to refresh current card inline, got %#v", events)
	}
	prompt = requestPromptFromEvent(t, events[0])
	if prompt.CurrentQuestionIndex != 0 || prompt.RequestRevision != 3 {
		t.Fatalf("expected previous-step action to move back, got %#v", prompt)
	}
}
