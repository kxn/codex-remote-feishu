package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// Grants belong to the selected original and exact audience. Past project
// creation and roster reuse never authorize a newly selected original.
func (t *Tx) TeamMaterialsProjectAllowed(team work.Team, channel work.ProjectChannelBinding) (bool, error) {
	if team.Materials == nil || !channel.Active {
		return false, nil
	}
	project, err := t.GetProject(channel.ProjectID)
	if err != nil {
		return false, err
	}
	if project.TeamID != team.ID {
		return false, nil
	}
	grant, ok := team.Materials.ProjectGrants[project.ID]
	return ok && grant.Tenant == channel.Tenant && grant.ChatID == channel.ChatID && grant.ChannelGeneration == channel.BindingGeneration, nil
}

func (t *Tx) ShareTeamMaterials(teamID string, expected int64, channel work.ProjectChannelBinding, source work.SourceRef) (work.Team, error) {
	team, err := t.GetTeam(teamID)
	if err != nil {
		return team, err
	}
	if team.Revision != expected || team.Materials == nil {
		return team, ErrConflict
	}
	project, err := t.GetProject(channel.ProjectID)
	if err != nil {
		return team, err
	}
	if project.TeamID != teamID || !channel.Active {
		return team, ErrScopeDenied
	}
	home := *team.Materials
	grants := map[string]work.TeamMaterialsGrant{}
	for id, grant := range home.ProjectGrants {
		grants[id] = grant
	}
	grants[project.ID] = work.TeamMaterialsGrant{Tenant: channel.Tenant, ChatID: channel.ChatID, ChannelGeneration: channel.BindingGeneration, Source: source}
	home.ProjectGrants = grants
	team.Materials = &home
	team.Revision++
	return team, t.PutTeam(team, expected)
}
