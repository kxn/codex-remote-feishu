package workservice

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PrivateNavigateRequest struct {
	Key              string `json:"key"`
	Action           string `json:"action"`
	ProjectScopeID   string `json:"project_scope_id,omitempty"`
	ContextID        string `json:"context_id,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
	SourceMessageID  string `json:"source_message_id"`
}
type TrustedPrivateNavigateRequest struct {
	Key              string
	Source           workstore.PrivateActionSource
	Action           string
	ProjectScopeID   string
	ContextID        string
	ExpectedRevision int64
}
type PrivateNavigationView struct {
	Result            Result                 `json:"result"`
	Navigation        work.PrivateNavigation `json:"navigation"`
	RunContextID      string                 `json:"run_context_id,omitempty"`
	RunProjectScopeID string                 `json:"run_project_scope_id,omitempty"`
	Project           *work.Project          `json:"project,omitempty"`
	ProjectAccess     bool                   `json:"project_access"`
}

func (s *Service) PrivateView(ctx context.Context, token, runRef string) (PrivateNavigationView, error) {
	var view PrivateNavigationView
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal {
			return workauth.ErrUnauthorized
		}
		view, err = s.privateView(tx, scope.ID)
		if err != nil {
			return err
		}
		context, err := tx.GetContext(c.Worker.ContextID)
		if err != nil {
			return err
		}
		view.RunContextID = context.ID
		view.RunProjectScopeID = context.ProjectViewScopeID
		return nil
	})
	return view, err
}
func (s *Service) privateView(tx *workstore.Tx, scopeID string) (PrivateNavigationView, error) {
	var view PrivateNavigationView
	navigation, err := tx.GetPrivateNavigation(scopeID)
	if err != nil {
		return view, err
	}
	view.Navigation = navigation
	if navigation.Mode == work.PrivateProject && tx.RequireProjectPrincipalAccess(navigation.ProjectScopeID, navigation.PrincipalID, s.now()) == nil && ensureMember(tx, navigation.ProjectScopeID, navigation.RoleID) == nil {
		scope, err := tx.GetScope(navigation.ProjectScopeID)
		if err != nil {
			return view, err
		}
		project, err := tx.GetProject(scope.ProjectID)
		if err != nil {
			return view, err
		}
		view.Project = &project
		view.ProjectAccess = true
	}
	return view, nil
}
func (s *Service) PrivateNavigate(ctx context.Context, token, runRef string, req PrivateNavigateRequest) (PrivateNavigationView, error) {
	var result PrivateNavigationView
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.privateNavigateTx(tx, TrustedPrivateNavigateRequest{Key: req.Key, Source: workstore.PrivateActionSource{PersonalScopeID: c.Worker.ScopeID, MessageID: req.SourceMessageID, RunRef: c.Attempt.RunRef}, Action: req.Action, ProjectScopeID: req.ProjectScopeID, ContextID: req.ContextID, ExpectedRevision: req.ExpectedRevision})
		return err
	})
	return result, err
}
func (s *Service) TrustedPrivateNavigate(ctx context.Context, req TrustedPrivateNavigateRequest) (PrivateNavigationView, error) {
	var result PrivateNavigationView
	err := s.store.Within(ctx, func(tx *workstore.Tx) error { var err error; result, err = s.privateNavigateTx(tx, req); return err })
	return result, err
}
func (s *Service) privateNavigateTx(tx *workstore.Tx, req TrustedPrivateNavigateRequest) (PrivateNavigationView, error) {
	scope, _, err := tx.ValidatePrivateAction(req.Source)
	if err != nil {
		return PrivateNavigationView{}, err
	}
	result, err := s.privateMutate(tx, scope, "private.navigation."+req.Action, req.Key, scope.ID, req, func() (string, int64, error) {
		old, err := tx.GetPrivateNavigation(scope.ID)
		if err != nil {
			return "", 0, err
		}
		if old.Revision != req.ExpectedRevision {
			return "", 0, workstore.ErrConflict
		}
		mode := work.PrivatePersonal
		projectScopeID, projectContextID := "", ""
		var front work.WorkContext
		switch req.Action {
		case "select_context":
			front, err = tx.GetContext(req.ContextID)
			if err != nil {
				return "", 0, err
			}
			if front.Purpose == work.PurposeReview || front.Purpose == work.PurposeControl || front.ScopeID != scope.ID || front.RoleID != scope.RoleID {
				return "", 0, workauth.ErrUnauthorized
			}
			projectScopeID = front.ProjectViewScopeID
			projectContextID = front.ProjectContextID
			if projectScopeID != "" {
				mode = work.PrivateProject
			}
		case "return_personal":
			contexts, e := tx.ListContexts(scope.ID)
			if e != nil {
				return "", 0, e
			}
			for _, candidate := range contexts {
				if candidate.RoleID == scope.RoleID && candidate.ProjectViewScopeID == "" && candidate.TaskID == "" {
					front = candidate
					break
				}
			}
			if front.ID == "" {
				front, err = s.newPrivateFront(tx, scope, req.Key, "", "")
			}
		case "enter_project", "new":
			projectScopeID = req.ProjectScopeID
			if req.Action == "new" {
				if target, quoted, e := quotedPrivateCommandContext(tx, req.Source); e != nil {
					return "", 0, e
				} else if quoted {
					projectScopeID = target.ProjectViewScopeID
				} else if old.Mode == work.PrivateProject {
					projectScopeID = old.ProjectScopeID
				}
			}
			if projectScopeID != "" {
				mode = work.PrivateProject
				if err = tx.RequireProjectPrincipalAccess(projectScopeID, scope.PrincipalID, s.now()); err != nil {
					return "", 0, err
				}
				if err = ensureMember(tx, projectScopeID, scope.RoleID); err != nil {
					return "", 0, err
				}
				contexts, e := tx.ListContexts(projectScopeID)
				if e != nil {
					return "", 0, e
				}
				if req.Action != "new" {
					for _, candidate := range contexts {
						if candidate.RoleID == scope.RoleID && candidate.TaskID == "" && candidate.Purpose != work.PurposeControl {
							projectContextID = candidate.ID
							break
						}
					}
				}
				if projectContextID == "" {
					context := work.WorkContext{ID: stableID("project_context", scope.ID, req.Key), ScopeID: projectScopeID, RoleID: scope.RoleID, Workstream: stableID("private_project_stream", scope.PrincipalID, scope.RoleID, req.Key), Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
					if err = tx.PutContext(context, 0); err != nil {
						return "", 0, err
					}
					projectContextID = context.ID
				}
			} else if req.Action == "enter_project" {
				return "", 0, fmt.Errorf("project scope is required")
			}
			front, err = s.newPrivateFront(tx, scope, req.Key, projectScopeID, projectContextID)
		default:
			return "", 0, fmt.Errorf("unknown private navigation action")
		}
		if err != nil {
			return "", 0, err
		}
		navigation, err := tx.ChangePrivateNavigation(workstore.ChangePrivateNavigationRequest{Source: req.Source, ExpectedRevision: old.Revision, Mode: mode, ProjectScopeID: projectScopeID, ProjectContextID: projectContextID, FrontContextID: front.ID, Now: s.now()})
		if err != nil {
			return "", 0, err
		}
		if req.Source.MessageID != "" && front.ID != old.FrontContextID {
			input := work.Inbox{ID: stableID("navigation_input", scope.ID, req.Key), ScopeID: scope.ID, RecipientRoleID: scope.RoleID, RecipientContextID: front.ID, MessageID: req.Source.MessageID, EventID: stableID("navigation_event", req.Source.MessageID, front.ID), State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: s.now(), Revision: 1}
			if err = tx.PutInbox(input, 0); err != nil {
				return "", 0, err
			}
		}
		return front.ID, navigation.Revision, nil
	})
	if err != nil {
		return PrivateNavigationView{}, err
	}
	view, err := s.privateView(tx, scope.ID)
	view.Result = result
	return view, err
}
func (s *Service) newPrivateFront(tx *workstore.Tx, scope work.WorkScope, key, projectScopeID, projectContextID string) (work.WorkContext, error) {
	context := work.WorkContext{ID: stableID("private_front", scope.ID, key), ScopeID: scope.ID, RoleID: scope.RoleID, Workstream: stableID("private_stream", scope.ID, key), ProjectViewScopeID: projectScopeID, ProjectContextID: projectContextID, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	err := tx.PutContext(context, 0)
	return context, err
}
