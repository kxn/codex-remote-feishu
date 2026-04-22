package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func planUpdateSections(update control.PlanUpdate) []control.FeishuCardTextSection {
	sections := make([]control.FeishuCardTextSection, 0, 2)
	if explanation := strings.TrimSpace(update.Explanation); explanation != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Lines: splitCommandCatalogPlainTextLines(explanation),
		})
	}
	capacity := len(update.Steps)
	if capacity == 0 {
		capacity = 1
	}
	stepLines := make([]string, 0, capacity)
	if len(update.Steps) == 0 {
		stepLines = append(stepLines, "○ 待补充步骤")
	} else {
		for _, step := range update.Steps {
			stepLines = append(stepLines, planUpdateStatusLabel(step.Status)+" "+strings.TrimSpace(step.Step))
		}
	}
	sections = append(sections, control.FeishuCardTextSection{
		Label: "步骤",
		Lines: stepLines,
	})
	return sections
}

func PlanUpdateElements(update control.PlanUpdate) []map[string]any {
	return appendCardTextSections(nil, planUpdateSections(update))
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
