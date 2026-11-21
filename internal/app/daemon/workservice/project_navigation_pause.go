package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PauseFromProjectNavigation loads the persisted action, current fixed view and
// project together. Membership verification runs before this call, and its
// evidence is checked again here without leaving the lifecycle transaction.
func (s *Service) PauseFromProjectNavigation(ctx context.Context, interactionID, viewID string) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(interactionID)
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if view.Kind != "project.navigation" || interaction.Envelope.Values["team_view"] != view.ID ||
			interaction.Envelope.Values["action"] != "project.navigation.pause" {
			return workstore.ErrScopeDenied
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		home, err := tx.GetProjectHome(view.TargetID)
		if err != nil {
			return err
		}
		if home.NavigationCardViewID != view.ID || home.ScopeID != view.ScopeID ||
			home.Identity != view.Binding || home.ChannelGeneration != view.ChannelGeneration {
			return workstore.ErrConflict
		}
		if err = tx.RequireProjectPrincipalAccess(view.ScopeID, interaction.PrincipalID, s.now()); err != nil {
			return err
		}
		project, err := tx.GetProject(view.TargetID)
		if err != nil {
			return err
		}
		if project.State != work.ProjectActive && project.State != work.ProjectDegraded {
			return fmt.Errorf("项目当前无法暂停。请点击“查看工作”查看现有状态和可用操作。")
		}
		messageID, err := s.recordProjectNavigationPause(tx, view, interaction)
		if err != nil {
			return err
		}
		var result Result
		req := HumanProjectLifecycleRequest{Action: "pause", ProjectLifecycleRequest: ProjectLifecycleRequest{
			Key: interaction.ID, ProjectID: project.ID, ExpectedRevision: project.Revision, SourceMessageID: messageID,
		}}
		if err = s.applyHumanProjectLifecycleTx(tx, interaction.PrincipalID, req, &result); err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, result.OperationID)
	})
}

func (s *Service) recordProjectNavigationPause(tx *workstore.Tx, view workstore.UIView, interaction workstore.HumanInteraction) (string, error) {
	proof, err := json.Marshal(interaction)
	if err != nil {
		return "", err
	}
	now := s.now()
	messageID := interaction.ID + ":instruction"
	content := work.NewContent(interaction.ID, view.ScopeID, "application/json", proof, now)
	body := work.NewContent(messageID+":body", view.ScopeID, "text/plain", []byte("Human selected project action: pause"), now)
	if err = tx.PutContent(content); err != nil {
		return "", err
	}
	if err = tx.PutContent(body); err != nil {
		return "", err
	}
	_, _, err = tx.AcceptMessage(work.Message{
		ID: messageID, ScopeID: view.ScopeID, ConversationID: view.ChatID,
		Source:      work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID},
		DerivedFrom: &work.SourceRef{Kind: "human_interaction", ID: interaction.ID, Version: strconv.FormatInt(interaction.Revision, 10), EvidenceRef: content.ID},
		BodyRef:     body.ID, Revision: 1, CreatedAt: now,
	}, nil)
	return messageID, err
}
