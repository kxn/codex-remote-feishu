package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func planUpdateBody(update control.PlanUpdate) string {
	if text := strings.TrimSpace(update.Explanation); text != "" {
		return text
	}
	return "Codex 更新了当前执行计划。"
}

func planUpdateElements(update control.PlanUpdate) []map[string]any {
	elements := make([]map[string]any, 0, len(update.Steps)+1)
	if explanation := strings.TrimSpace(update.Explanation); explanation != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": fmt.Sprintf("**说明** %s", explanation),
		})
	}
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
			"content": fmt.Sprintf("%s %s", planUpdateStatusLabel(step.Status), strings.TrimSpace(step.Step)),
		})
	}
	return elements
}

func planUpdateStatusLabel(status agentproto.TurnPlanStepStatus) string {
	switch status {
	case agentproto.TurnPlanStepStatusCompleted:
		return "☑ 已完成"
	case agentproto.TurnPlanStepStatusInProgress:
		return "◐ 进行中"
	case agentproto.TurnPlanStepStatusPending:
		return "○ 待处理"
	default:
		return "○ 状态未知"
	}
}
