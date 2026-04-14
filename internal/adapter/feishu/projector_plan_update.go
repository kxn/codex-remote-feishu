package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func planUpdateBody(update control.PlanUpdate) string {
	return strings.TrimSpace(update.Explanation)
}

func planUpdateElements(update control.PlanUpdate) []map[string]any {
	elements := make([]map[string]any, 0, len(update.Steps))
	if len(update.Steps) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "○ 待补充步骤",
		})
		return elements
	}
	for _, step := range update.Steps {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": planUpdateStatusLabel(step.Status) + " " + strings.TrimSpace(step.Step),
		})
	}
	return elements
}

func planUpdateStatusLabel(status agentproto.TurnPlanStepStatus) string {
	switch status {
	case agentproto.TurnPlanStepStatusCompleted:
		return "☑"
	case agentproto.TurnPlanStepStatusInProgress:
		return "◐"
	case agentproto.TurnPlanStepStatusPending:
		return "○"
	default:
		return "○"
	}
}
