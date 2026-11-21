package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PrivateProjectContextView struct {
	Project   work.Project     `json:"project"`
	Context   work.WorkContext `json:"context"`
	Tasks     []work.Task      `json:"tasks"`
	Resources []work.Resource  `json:"resources"`
}
type PrivateProjectReadContentRequest struct {
	ContentID string `json:"content_id"`
}

// Project reads use the current run's fixed front context, not the latest UI
// selection. A background private run cannot follow navigation into a new team.
func (s *Service) privateProjectForRun(tx *workstore.Tx, c Caller) (work.Project, work.WorkContext, error) {
	scope, err := tx.GetScope(c.Worker.ScopeID)
	if err != nil {
		return work.Project{}, work.WorkContext{}, err
	}
	if scope.Kind != work.ScopePersonal {
		return work.Project{}, work.WorkContext{}, workauth.ErrUnauthorized
	}
	front, err := tx.GetContext(c.Worker.ContextID)
	if err != nil {
		return work.Project{}, work.WorkContext{}, err
	}
	if front.ProjectViewScopeID == "" {
		return work.Project{}, work.WorkContext{}, workauth.ErrUnauthorized
	}
	if err = tx.RequireProjectPrincipalAccess(front.ProjectViewScopeID, scope.PrincipalID, s.now()); err != nil {
		return work.Project{}, work.WorkContext{}, err
	}
	if err = ensureMember(tx, front.ProjectViewScopeID, scope.RoleID); err != nil {
		return work.Project{}, work.WorkContext{}, err
	}
	projectScope, err := tx.GetScope(front.ProjectViewScopeID)
	if err != nil {
		return work.Project{}, work.WorkContext{}, err
	}
	project, err := tx.GetProject(projectScope.ProjectID)
	if err != nil {
		return project, work.WorkContext{}, err
	}
	selected, err := tx.GetContext(front.ProjectContextID)
	if err != nil {
		return project, selected, err
	}
	if selected.ScopeID != projectScope.ID || selected.RoleID != scope.RoleID || selected.Purpose == work.PurposeControl {
		return project, selected, workauth.ErrUnauthorized
	}
	return project, selected, nil
}
func (s *Service) PrivateProjectContext(ctx context.Context, token, runRef string) (view PrivateProjectContextView, err error) {
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var e error
		view.Project, view.Context, e = s.privateProjectForRun(tx, c)
		if e != nil {
			return e
		}
		view.Tasks, e = tx.ListTasks(view.Project.ScopeID)
		if e != nil {
			return e
		}
		view.Resources, e = tx.ListResources(view.Project.ScopeID)
		return e
	})
	return
}
func (s *Service) PrivateProjectReadContent(ctx context.Context, token, runRef string, req PrivateProjectReadContentRequest) (body work.Content, err error) {
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		project, _, e := s.privateProjectForRun(tx, c)
		if e != nil {
			return e
		}
		body, e = tx.GetContent(project.ScopeID, req.ContentID)
		return e
	})
	return
}
