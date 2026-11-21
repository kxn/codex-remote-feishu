package workservice

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func teamViewForScope(tx *workstore.Tx, team work.Team, scope work.WorkScope) (work.Team, error) {
	if team.Materials == nil {
		return team, nil
	}
	if scope.PrincipalID != "" {
		owner, err := tx.GetTeamOwnership(team.ID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return team, err
		}
		if err == nil && owner.PrincipalID == scope.PrincipalID {
			return team, nil
		}
	}
	if scope.Kind == work.ScopeProject && scope.TeamID == team.ID {
		channels, err := tx.ListProjectChannelBindings(scope.ProjectID)
		if err != nil {
			return team, err
		}
		for _, channel := range channels {
			if channel.Active && channel.Purpose == "main" {
				allowed, err := tx.TeamMaterialsProjectAllowed(team, channel)
				if err != nil {
					return team, err
				}
				if allowed {
					// A shared document does not expose the owner's private
					// selection source, parent folder, or other project audiences.
					home := *team.Materials
					home.ScopeID = ""
					home.Source = work.SourceRef{}
					home.Selection.URL = ""
					home.ProjectGrants = nil
					team.Materials = &home
					return team, nil
				}
			}
		}
	}
	// A reusable role is not a principal's consent to expose team material links.
	team.Materials = nil
	return team, nil
}
