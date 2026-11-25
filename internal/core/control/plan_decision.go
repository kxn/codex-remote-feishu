package control

import "strings"

const (
	PlanProposalExecute    = "execute"
	PlanProposalExecuteNew = "execute_new"
)

// PlanProposalExecutes reports whether this action authorizes a prompt.
func PlanProposalExecutes(option string) bool {
	switch strings.TrimSpace(option) {
	case PlanProposalExecute, PlanProposalExecuteNew:
		return true
	default:
		return false
	}
}
