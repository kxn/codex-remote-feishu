package agentproto

import "strings"

type TurnPlanStepStatus string

const (
	TurnPlanStepStatusPending    TurnPlanStepStatus = "pending"
	TurnPlanStepStatusInProgress TurnPlanStepStatus = "in_progress"
	TurnPlanStepStatusCompleted  TurnPlanStepStatus = "completed"
)

type TurnPlanStep struct {
	Step   string             `json:"step,omitempty"`
	Status TurnPlanStepStatus `json:"status,omitempty"`
}

type TurnPlanSnapshot struct {
	Explanation string         `json:"explanation,omitempty"`
	Steps       []TurnPlanStep `json:"steps,omitempty"`
}

func NormalizeTurnPlanStepStatus(raw string) TurnPlanStepStatus {
	switch strings.TrimSpace(raw) {
	case "pending", "Pending":
		return TurnPlanStepStatusPending
	case "inProgress", "in_progress", "InProgress":
		return TurnPlanStepStatusInProgress
	case "completed", "Completed":
		return TurnPlanStepStatusCompleted
	default:
		raw = strings.TrimSpace(raw)
		raw = strings.ReplaceAll(raw, " ", "_")
		return TurnPlanStepStatus(strings.ToLower(raw))
	}
}

func CloneTurnPlanSnapshot(snapshot *TurnPlanSnapshot) *TurnPlanSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := &TurnPlanSnapshot{
		Explanation: snapshot.Explanation,
	}
	if len(snapshot.Steps) > 0 {
		cloned.Steps = append([]TurnPlanStep(nil), snapshot.Steps...)
	}
	return cloned
}
