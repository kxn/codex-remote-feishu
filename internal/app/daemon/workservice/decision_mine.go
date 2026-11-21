package workservice

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PersonalDecisionItem struct {
	RequestID string                `json:"request_id"`
	Revision  int64                 `json:"revision"`
	Project   string                `json:"project,omitempty"`
	TaskID    string                `json:"task_id,omitempty"`
	Title     string                `json:"title"`
	Question  string                `json:"question"`
	Choices   []work.DecisionChoice `json:"choices"`
	MessageID string                `json:"message_id,omitempty"`
	ChatID    string                `json:"chat_id,omitempty"`
}

func (s *Service) DecisionMine(ctx context.Context, token, runRef string, req KnowledgeListRequest) (KnowledgePage[PersonalDecisionItem], error) {
	var result KnowledgePage[PersonalDecisionItem]
	err := s.withManagementWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal {
			return workauth.ErrUnauthorized
		}
		items, err := s.appendPersonalDecisions(tx, nil, scope.ID, "", scope.PrincipalID)
		if err != nil {
			return err
		}
		projects, err := tx.ListProjects()
		if err != nil {
			return err
		}
		for _, project := range projects {
			if project.State == work.ProjectArchived {
				continue
			}
			if err = tx.RequireProjectPrincipalAccess(project.ScopeID, scope.PrincipalID, s.now()); errors.Is(err, workstore.ErrScopeDenied) || errors.Is(err, workstore.ErrNotFound) {
				continue
			} else if err != nil {
				return err
			}
			if items, err = s.appendPersonalDecisions(tx, items, project.ScopeID, project.Name, scope.PrincipalID); err != nil {
				return err
			}
		}
		result, err = knowledgePage(items, req, func(v PersonalDecisionItem) string { return v.RequestID })
		return err
	})
	return result, err
}

func (s *Service) appendPersonalDecisions(tx *workstore.Tx, items []PersonalDecisionItem, scopeID, project, principalID string) ([]PersonalDecisionItem, error) {
	requests, err := tx.ListDecisionRequests(scopeID)
	if err != nil {
		return nil, err
	}
	actor := work.Actor{Kind: work.ActorPrincipal, ID: principalID}
	views, err := tx.ListUIViews(scopeID)
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		if request.Kind != work.DecisionBusiness || request.State != work.DecisionOpen || request.Owner != actor || request.Presentation == nil {
			continue
		}
		prompt, err := tx.GetContent(scopeID, request.Presentation.PromptRef)
		if err != nil {
			return nil, err
		}
		text := []rune(string(prompt.Data))
		if len(text) > 1200 {
			text = append(text[:1200], '…')
		}
		item := PersonalDecisionItem{RequestID: request.ID, Revision: request.Revision, Project: project, TaskID: request.TaskID, Title: request.Presentation.Title, Question: string(text), Choices: request.Presentation.Choices}
		for _, view := range views {
			if view.Kind == "business.decision" && view.TargetID == request.ID && view.TargetRevision == request.Revision && view.PrincipalID == principalID && !view.Invalidated && view.ExternalMessageID != "" {
				item.MessageID, item.ChatID = view.ExternalMessageID, view.ChatID
				break
			}
		}
		items = append(items, item)
	}
	return items, nil
}
