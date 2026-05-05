package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectPlanUpdateCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindPlanUpdate,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om_1",
		PlanUpdate: &control.PlanUpdate{
			ThreadID:              "thread-1",
			TurnID:                "turn-1",
			TemporarySessionLabel: "临时会话 · 分支",
			Explanation:           "先把协议和去重打通。",
			Steps: []control.PlanUpdateStep{
				{Step: "接入结构化 plan", Status: agentproto.TurnPlanStepStatusCompleted},
				{Step: "做 orchestrator 去重", Status: agentproto.TurnPlanStepStatusInProgress},
				{Step: "接入飞书卡片投影", Status: agentproto.TurnPlanStepStatusPending},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("expected one card operation, got %#v", ops)
	}
	op := ops[0]
	if op.CardTitle != "当前计划" || op.ReplyToMessageID != "" {
		t.Fatalf("unexpected plan update card envelope: %#v", op)
	}
	header := renderedV2CardHeader(t, op)
	if got := headerTextContent(header, "subtitle"); got != "**临时会话 · 分支**" {
		t.Fatalf("expected detour subtitle on plan update card, got %#v", header)
	}
	if op.CardThemeKey != cardThemePlan {
		t.Fatalf("expected plan card theme key %q, got %#v", cardThemePlan, op.CardThemeKey)
	}
	if op.CardBody != "" {
		t.Fatalf("expected plan update card body to stay empty, got %#v", op)
	}
	if len(op.CardElements) != 3 {
		t.Fatalf("expected explanation block plus step section, got %#v", op.CardElements)
	}
	joined := renderedV2CardText(t, op)
	if !containsAll(joined, "先把协议和去重打通。", "**步骤**") {
		t.Fatalf("expected explanation and step heading in rendered card, got %q", joined)
	}
	if !strings.Contains(joined, "☑ 接入结构化 plan") {
		t.Fatalf("expected completed step row, got %q", joined)
	}
	if !strings.Contains(joined, "◐ 做 orchestrator 去重") {
		t.Fatalf("expected in-progress step row, got %q", joined)
	}
	if !strings.Contains(joined, "○ 接入飞书卡片投影") {
		t.Fatalf("expected pending step row, got %q", joined)
	}
}

func TestProjectPlanUpdateWithoutStepsShowsFallback(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindPlanUpdate,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		PlanUpdate:       &control.PlanUpdate{},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("expected one card operation, got %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected empty body without explanation, got %#v", ops[0])
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected step heading plus fallback block, got %#v", ops[0].CardElements)
	}
	joined := renderedV2CardText(t, ops[0])
	if !containsAll(joined, "**步骤**", "○ 待补充步骤") {
		t.Fatalf("unexpected fallback rendering: %q", joined)
	}
}

func TestProjectPlanUpdateUsesExplicitReplyLane(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindPlanUpdate,
		SourceMessageID: "msg-plan-1",
		PlanUpdate: &control.PlanUpdate{
			Explanation: "继续处理",
		},
		Meta: eventcontract.EventMeta{
			MessageDelivery: eventcontract.MessageDelivery{
				FirstSendLane: eventcontract.MessageLaneReplyThread,
				Mutation:      eventcontract.MessageMutationAppendOnly,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("expected one card operation, got %#v", ops)
	}
	if ops[0].ReplyToMessageID != "msg-plan-1" {
		t.Fatalf("expected explicit reply lane to reach projector, got %#v", ops[0])
	}
}
