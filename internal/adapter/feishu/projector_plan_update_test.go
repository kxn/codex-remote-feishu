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
	if op.CardTitle != "计划更新" || op.ReplyToMessageID != "om_1" {
		t.Fatalf("unexpected plan update card envelope: %#v", op)
	}
	if len(op.CardElements) != 4 {
		t.Fatalf("expected explanation plus three step rows, got %#v", op.CardElements)
	}
	rendered := make([]string, 0, len(op.CardElements))
	for _, element := range op.CardElements {
		if content, _ := element["content"].(string); content != "" {
			rendered = append(rendered, content)
		}
	}
	joined := strings.Join(rendered, "\n")
	if !strings.Contains(joined, "**说明** 先把协议和去重打通。") {
		t.Fatalf("expected explanation row, got %q", joined)
	}
	if !strings.Contains(joined, "☑ 已完成 接入结构化 plan") {
		t.Fatalf("expected completed step row, got %q", joined)
	}
	if !strings.Contains(joined, "◐ 进行中 做 orchestrator 去重") {
		t.Fatalf("expected in-progress step row, got %q", joined)
	}
	if !strings.Contains(joined, "○ 待处理 接入飞书卡片投影") {
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
	if len(ops[0].CardElements) != 1 {
		t.Fatalf("expected one fallback row, got %#v", ops[0].CardElements)
	}
	if content, _ := ops[0].CardElements[0]["content"].(string); content != "○ 待补充步骤" {
		t.Fatalf("unexpected fallback row: %#v", ops[0].CardElements)
	}
}
