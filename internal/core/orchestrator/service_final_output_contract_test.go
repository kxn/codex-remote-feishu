package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFailedTurnWithCompletedAssistantTextStillMaterializesFinalOutput(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	now = now.Add(2100 * time.Millisecond)

	events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "failed", "upstream stream closed after final result", "这是已经完整生成的答复。", &agentproto.ErrorInfo{
		Code:      "upstream_stream_closed",
		Layer:     "claude",
		Stage:     "runtime_error",
		Message:   "upstream stream closed after final result",
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Retryable: false,
	})

	var sawFinalBlock bool
	var sawFailureNotice bool
	for _, event := range events {
		if event.Block != nil && strings.TrimSpace(event.Block.Text) == "这是已经完整生成的答复。" {
			if !event.Block.Final {
				t.Fatalf("expected final block to be marked final, got %#v", event.Block)
			}
			sawFinalBlock = true
		}
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			sawFailureNotice = true
		}
	}
	if !sawFinalBlock || !sawFailureNotice {
		t.Fatalf("expected final block plus failure notice, got %#v", events)
	}
}
