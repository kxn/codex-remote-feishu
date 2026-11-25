package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) requireGoalVerification(lease *GoalInterlockLease, text string) []eventcontract.Event {
	if lease == nil {
		return nil
	}
	delete(s.goalInterlockByCommand, lease.PauseCommandID)
	delete(s.goalInterlockByCommand, lease.ResumeCommandID)
	delete(s.goalGetByCommand, lease.GetCommandID)
	delete(s.goalProbeByCommand, lease.ProbeCommandID)
	lease.PauseCommandID = ""
	lease.ResumeCommandID = ""
	lease.GetCommandID = ""
	lease.ProbeCommandID = ""
	lease.Phase = GoalInterlockVerificationRequired
	lease.ExternalMutationSeen = true
	lease.UpdatedAt = s.now()
	return goalInterlockDiagnosticNotice(lease.TriggerSurfaceID, "goal_verification_required", "等待确认 Goal 状态", text)
}

func goalStatusKnownInactive(status string) bool {
	switch strings.TrimSpace(status) {
	case "paused", "blocked", "complete", "usageLimited", "budgetLimited", "usage_limited", "budget_limited":
		return true
	default:
		return false
	}
}
