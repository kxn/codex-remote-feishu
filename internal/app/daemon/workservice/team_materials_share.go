package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TeamMaterialsShareRequest struct {
	Key              string `json:"key"`
	TeamID           string `json:"team_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	SourceMessageID  string `json:"source_message_id"`
}

func (s *Service) TeamMaterialsShare(ctx context.Context, token, run string, req TeamMaterialsShareRequest) (Result, error) {
	var result Result
	err := s.withManagement(ctx, token, run, req.SourceMessageID, "team.manage", req.TeamID, func(tx *workstore.Tx, c Caller, message work.Message) error {
		var err error
		result, err = s.mutate(tx, c, "team.materials.share", req.Key, req.TeamID, req, func() (string, int64, error) {
			scope, e := tx.GetScope(c.Worker.ScopeID)
			if e != nil {
				return "", 0, e
			}
			if scope.Kind != work.ScopeProject || scope.TeamID != req.TeamID {
				return "", 0, workstore.ErrScopeDenied
			}
			channels, e := tx.ListProjectChannelBindings(scope.ProjectID)
			if e != nil {
				return "", 0, e
			}
			for _, channel := range channels {
				if channel.Active && channel.Purpose == "main" {
					team, e := tx.ShareTeamMaterials(req.TeamID, req.ExpectedRevision, channel, humanSource(message))
					return team.ID, team.Revision, e
				}
			}
			return "", 0, workstore.ErrScopeDenied
		})
		return err
	})
	return result, err
}
func (s *Service) ShareTeamMaterialsFromInteraction(ctx context.Context, interactionID, viewID string) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, e := tx.GetHumanInteraction(interactionID)
		if e != nil {
			return e
		}
		view, e := tx.GetUIView(viewID)
		if e != nil {
			return e
		}
		if view.Kind != "team.materials" || interaction.Envelope.Values["team_view"] != view.ID || interaction.Envelope.Values["action"] != "team.materials.share" {
			return workstore.ErrScopeDenied
		}
		if e = tx.ValidateViewInteraction(view, interaction); e != nil {
			return e
		}
		owner, e := tx.GetTeamOwnership(view.TargetID)
		if e != nil {
			return e
		}
		if owner.PrincipalID != interaction.PrincipalID {
			return workstore.ErrScopeDenied
		}
		if e = tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, s.now()); e != nil {
			return e
		}
		channel, e := tx.GetProjectChannelBinding(view.Tenant, view.ChatID)
		if e != nil {
			return e
		}
		if channel.BindingGeneration != view.ChannelGeneration {
			return workstore.ErrConflict
		}
		_, e = tx.ShareTeamMaterials(view.TargetID, view.TargetRevision, channel, work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: "1", EvidenceRef: view.BodyRef})
		if e != nil {
			return e
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "team-materials-shared")
	})
}
