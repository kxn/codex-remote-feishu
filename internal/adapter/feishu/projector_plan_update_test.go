package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectPlanUpdateCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventPlanUpdated,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om_1",
		PlanUpdate: &control.PlanUpdate{
			ThreadID:    "thread-1",
			TurnID:      "turn-1",
			Explanation: "先把协议和去重打通。",
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
	if op.CardThemeKey != cardThemePlan {
		t.Fatalf("expected plan card theme key %q, got %#v", cardThemePlan, op.CardThemeKey)
	}
	if op.CardBody != "先把协议和去重打通。" {
		t.Fatalf("expected explanation in card body, got %#v", op)
	}
	if len(op.CardElements) != 3 {
		t.Fatalf("expected three step rows, got %#v", op.CardElements)
	}
	rendered := make([]string, 0, len(op.CardElements))
	for _, element := range op.CardElements {
		if content, _ := element["content"].(string); content != "" {
			rendered = append(rendered, content)
		}
	}
	joined := strings.Join(rendered, "\n")
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
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventPlanUpdated,
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
	if len(ops[0].CardElements) != 1 {
		t.Fatalf("expected one fallback row, got %#v", ops[0].CardElements)
	}
	if content, _ := ops[0].CardElements[0]["content"].(string); content != "○ 待补充步骤" {
		t.Fatalf("unexpected fallback row: %#v", ops[0].CardElements)
	}
}
