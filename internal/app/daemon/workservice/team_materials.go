package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TeamMaterialsSelectRequest struct {
	Key              string                      `json:"key"`
	TeamID           string                      `json:"team_id"`
	ExpectedRevision int64                       `json:"expected_revision"`
	SourceMessageID  string                      `json:"source_message_id"`
	Selection        work.TeamMaterialsSelection `json:"selection"`
}

func (s *Service) TeamMaterialsSelect(ctx context.Context, token, run string, req TeamMaterialsSelectRequest) (Result, error) {
	var result Result
	err := s.withManagement(ctx, token, run, req.SourceMessageID, "team.manage", req.TeamID, func(tx *workstore.Tx, c Caller, message work.Message) error {
		var err error
		result, err = s.mutate(tx, c, "team.materials.select", req.Key, req.TeamID, req, func() (string, int64, error) {
			if req.Selection.Mode != "managed" {
				if err := directorySourceContainsURL(tx, message, req.Selection.URL); err != nil {
					return "", 0, err
				}
			}
			team, err := tx.GetTeam(req.TeamID)
			if err != nil {
				return "", 0, err
			}
			binding, err := tx.GetRoleBotBinding(team.LeaderRoleID)
			if err != nil {
				return "", 0, err
			}
			team, err = tx.SelectTeamMaterials(team.ID, req.ExpectedRevision, c.Worker.ScopeID, req.Selection, binding, humanSource(message))
			if err == nil {
				chatID := ""
				if message.PlatformKey != nil {
					chatID = message.PlatformKey.ChatID
				}
				team, err = shareSelectedTeamMaterials(tx, team, c.Worker.ScopeID, chatID, humanSource(message))
			}
			return team.ID, team.Revision, err
		})
		return err
	})
	return result, err
}

// The interaction path works while no model is available, using the same CAS
// and source contract. A project member alone cannot replace a team's original.
func (s *Service) SelectTeamMaterialsFromInteraction(ctx context.Context, interactionID, viewID string) (Result, error) {
	var result Result
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(interactionID)
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if view.Kind != "team.materials" || interaction.Envelope.Values["team_view"] != view.ID || interaction.Envelope.Values["action"] != "team.materials.select" {
			return workstore.ErrScopeDenied
		}
		mode, _ := interaction.Envelope.Form["materials_mode"].(string)
		link, _ := interaction.Envelope.Form["materials_url"].(string)
		selection := work.TeamMaterialsSelection{Mode: mode, URL: link}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		owner, err := tx.GetTeamOwnership(view.TargetID)
		if err != nil {
			return err
		}
		if owner.PrincipalID != interaction.PrincipalID {
			return workstore.ErrScopeDenied
		}
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil {
			return err
		}
		if scope.TeamID != view.TargetID {
			return workstore.ErrScopeDenied
		}
		if err = tx.RequireProjectPrincipalAccess(scope.ID, interaction.PrincipalID, s.now()); err != nil {
			return err
		}
		team, err := tx.GetTeam(view.TargetID)
		if err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(team.LeaderRoleID)
		if err != nil {
			return err
		}
		source := work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: "1", EvidenceRef: view.BodyRef}
		team, err = tx.SelectTeamMaterials(team.ID, view.TargetRevision, scope.ID, selection, binding, source)
		if err != nil {
			return err
		}
		team, err = shareSelectedTeamMaterials(tx, team, scope.ID, view.ChatID, source)
		if err != nil {
			return err
		}
		result = Result{Status: "accepted", ObjectID: team.ID, Revision: team.Revision}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "team-materials-selected")
	})
	return result, err
}

// Selecting a team original in a project explicitly connects this audience only.
func shareSelectedTeamMaterials(tx *workstore.Tx, team work.Team, scopeID, chatID string, source work.SourceRef) (work.Team, error) {
	scope, err := tx.GetScope(scopeID)
	if err != nil {
		return team, err
	}
	if scope.Kind != work.ScopeProject {
		return team, nil
	}
	if scope.TeamID != team.ID {
		return team, workstore.ErrScopeDenied
	}
	channels, err := tx.ListProjectChannelBindings(scope.ProjectID)
	if err != nil {
		return team, err
	}
	for _, channel := range channels {
		if channel.Active && channel.Purpose == "main" && channel.ChatID == chatID {
			return tx.ShareTeamMaterials(team.ID, team.Revision, channel, source)
		}
	}
	return team, workstore.ErrScopeDenied
}
