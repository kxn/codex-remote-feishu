package control

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

type PlanUpdateStep struct {
	Step   string
	Status agentproto.TurnPlanStepStatus
}

type PlanUpdate struct {
	ThreadID    string
	TurnID      string
	DetourLabel string
	Explanation string
	Steps       []PlanUpdateStep
}
