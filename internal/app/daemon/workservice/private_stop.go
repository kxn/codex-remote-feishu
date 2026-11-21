package workservice

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TrustedPrivateStopRequest struct {
	Key                 string
	Source              workstore.PrivateActionSource
	ExpectedNavRevision int64
}

func (s *Service) TrustedPrivateStop(ctx context.Context, req TrustedPrivateStopRequest) (Result, error) {
	return s.trustedPrivateControl(ctx, req, false)
}
func (s *Service) TrustedPrivateResume(ctx context.Context, req TrustedPrivateStopRequest) (Result, error) {
	return s.trustedPrivateControl(ctx, req, true)
}
func (s *Service) trustedPrivateControl(ctx context.Context, req TrustedPrivateStopRequest, resume bool) (result Result, err error) {
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, _, e := tx.ValidatePrivateAction(req.Source)
		if e != nil {
			return e
		}
		kind := "private.stop"
		if resume {
			kind = "private.resume"
		}
		result, e = s.privateMutate(tx, scope, kind, req.Key, scope.ID, req, func() (string, int64, error) {
			nav, e := tx.GetPrivateNavigation(scope.ID)
			if e != nil {
				return "", 0, e
			}
			if nav.Revision != req.ExpectedNavRevision {
				return "", 0, workstore.ErrConflict
			}
			if target, quoted, e := quotedPrivateCommandContext(tx, req.Source); e != nil {
				return "", 0, e
			} else if quoted {
				nav.FrontContextID = target.ID
				nav.ProjectScopeID = target.ProjectViewScopeID
				nav.ProjectContextID = target.ProjectContextID
				nav.Mode = work.PrivatePersonal
				if target.ProjectViewScopeID != "" {
					nav.Mode = work.PrivateProject
				}
			}
			if nav.Mode == work.PrivateProject {
				if e = tx.RequireProjectPrincipalAccess(nav.ProjectScopeID, scope.PrincipalID, s.now()); e != nil {
					return "", 0, e
				}
				if e = ensureMember(tx, nav.ProjectScopeID, scope.RoleID); e != nil {
					return "", 0, e
				}
				projectScope, e := tx.GetScope(nav.ProjectScopeID)
				if e != nil {
					return "", 0, e
				}
				project, e := tx.GetProject(projectScope.ProjectID)
				if e != nil {
					return "", 0, e
				}
				transition := "project.pause"
				if resume {
					transition = "project.resume"
				}
				if (!resume && (project.State == work.ProjectPaused || project.State == work.ProjectPausing)) || (resume && (project.State == work.ProjectActive || project.State == work.ProjectDegraded)) {
					return project.ID, project.Revision, nil
				}
				project, e = applyProjectTransition(tx, project, transition)
				return project.ID, project.Revision, e
			}
			id := stableID("private_context_gate", scope.ID, nav.FrontContextID)
			gate, e := tx.GetPauseGate(id)
			expected := int64(0)
			if errors.Is(e, workstore.ErrNotFound) {
				gate = work.PauseGate{ID: id, ScopeID: scope.ID, ContextID: nav.FrontContextID, Source: work.GateContext, SourceID: scope.PrincipalID, Revision: 1}
			} else if e != nil {
				return "", 0, e
			} else {
				expected = gate.Revision
				gate.Revision++
			}
			gate.State = work.GateClosed
			if resume {
				gate.State = work.GateOpen
			}
			if e = tx.PutPauseGate(gate, expected); e != nil {
				return "", 0, e
			}
			if !resume {
				inputs, e := tx.ListInbox(scope.ID)
				if e != nil {
					return "", 0, e
				}
				for _, input := range inputs {
					if input.State != work.InboxPending || input.RecipientContextID != nav.FrontContextID || input.MessageID == "" {
						continue
					}
					message, e := tx.GetMessage(input.MessageID)
					if e != nil {
						return "", 0, e
					}
					if message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) {
						continue
					}
					previous := input.Revision
					input.Revision++
					input.State = work.InboxCancelled
					if e = tx.PutInbox(input, previous); e != nil {
						return "", 0, e
					}
				}
			}
			return nav.FrontContextID, gate.Revision, nil
		})
		return e
	})
	return
}
