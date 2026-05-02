package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func prepareRemotePlanTurnForTest(t *testing.T) *Service {
	t.Helper()
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续处理",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	if svc.turns.activeRemote["inst-1"] == nil {
		t.Fatalf("expected active remote binding after remote turn start, got %#v", svc.turns.activeRemote)
	}
	return svc
}

func TestTurnPlanUpdateEmitsNeutralEventAndDedupesPerSurface(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "先打通协议。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "接入结构化 plan", Status: agentproto.TurnPlanStepStatusCompleted},
				{Step: "做 orchestrator 去重", Status: agentproto.TurnPlanStepStatusInProgress},
			},
		},
	})
	if len(first) != 1 || first[0].Kind != eventcontract.KindPlanUpdate || first[0].PlanUpdate == nil {
		t.Fatalf("expected one neutral plan update event, got %#v", first)
	}
	if first[0].PlanUpdate.Explanation != "先打通协议。" || len(first[0].PlanUpdate.Steps) != 2 {
		t.Fatalf("unexpected plan update payload: %#v", first[0].PlanUpdate)
	}

	duplicate := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "先打通协议。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "接入结构化 plan", Status: agentproto.TurnPlanStepStatusCompleted},
				{Step: "做 orchestrator 去重", Status: agentproto.TurnPlanStepStatusInProgress},
			},
		},
	})
	if len(duplicate) != 0 {
		t.Fatalf("expected duplicate snapshot to be suppressed, got %#v", duplicate)
	}

	changed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "先打通协议。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "接入结构化 plan", Status: agentproto.TurnPlanStepStatusCompleted},
				{Step: "做 orchestrator 去重", Status: agentproto.TurnPlanStepStatusCompleted},
			},
		},
	})
	if len(changed) != 1 || changed[0].Kind != eventcontract.KindPlanUpdate || changed[0].PlanUpdate == nil {
		t.Fatalf("expected changed snapshot to emit a new plan event, got %#v", changed)
	}
	if changed[0].PlanUpdate.Steps[1].Status != agentproto.TurnPlanStepStatusCompleted {
		t.Fatalf("expected updated status in plan event, got %#v", changed[0].PlanUpdate)
	}
}

func TestTurnPlanUpdateFlushesPendingTextBeforePlanEvent(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Delta:    "我先把协议接通。",
	})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(completed) != 0 {
		t.Fatalf("expected assistant text to stay pending until next event, got %#v", completed)
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "开始推进。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "接入结构化 plan", Status: agentproto.TurnPlanStepStatusInProgress},
			},
		},
	})
	if len(events) != 2 {
		t.Fatalf("expected pending text flush plus plan event, got %#v", events)
	}
	if events[0].Kind != eventcontract.KindBlockCommitted || events[0].Block == nil || events[0].Block.Text != "我先把协议接通。" {
		t.Fatalf("expected first event to flush pending assistant text, got %#v", events)
	}
	if events[1].Kind != eventcontract.KindPlanUpdate || events[1].PlanUpdate == nil {
		t.Fatalf("expected second event to be neutral plan update, got %#v", events)
	}
}

func TestTurnPlanUpdateCutsSharedProgressSegment(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)
	surface := svc.root.Surfaces["surface-1"]
	surface.Verbosity = state.SurfaceVerbosityVerbose

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial shared progress event, got %#v", started)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

	planEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnPlanUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		PlanSnapshot: &agentproto.TurnPlanSnapshot{
			Explanation: "改按新的检查顺序推进。",
			Steps: []agentproto.TurnPlanStep{
				{Step: "重新确认入口", Status: agentproto.TurnPlanStepStatusCompleted},
				{Step: "调整实现", Status: agentproto.TurnPlanStepStatusInProgress},
			},
		},
	})
	if len(planEvents) != 1 || planEvents[0].Kind != eventcontract.KindPlanUpdate {
		t.Fatalf("expected plan update event, got %#v", planEvents)
	}
	if surface.ActiveExecProgress != nil {
		t.Fatalf("expected plan update to terminate active shared progress segment, got %#v", surface.ActiveExecProgress)
	}

	next := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "go test ./internal/core/orchestrator",
		},
	})
	if len(next) != 1 || next[0].ExecCommandProgress == nil {
		t.Fatalf("expected new shared progress event after plan update, got %#v", next)
	}
	progress := next[0].ExecCommandProgress
	if activeProgressMessageID(progress) != "" {
		t.Fatalf("expected new shared progress to start a fresh card instead of patching old card, got %#v", progress)
	}
	if len(progress.Entries) != 1 || progress.Entries[0].ItemID != "cmd-2" {
		t.Fatalf("expected fresh shared progress state after plan boundary, got %#v", progress)
	}
}
