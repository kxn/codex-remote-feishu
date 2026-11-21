package workingress

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (r *routeDecision) coordinateTask() {
	if r.TaskID != "" {
		r.RelatedTaskID = r.TaskID
		r.TaskID = ""
	}
}

// Unavailable/invited members retain their existing waiting semantics. Only
// absent/removed project membership or inactive team identity loses routing.
func recipientIsMember(tx *workstore.Tx, scope work.WorkScope, role string) (bool, error) {
	member, err := tx.GetProjectMember(scope.ProjectID, role)
	if errors.Is(err, workstore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if member.State == work.ProjectMemberRemoved {
		return false, nil
	}
	team, err := tx.GetTeamMember(scope.TeamID, role)
	if errors.Is(err, workstore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return team.Active, nil
}
