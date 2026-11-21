package workservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RoleInspectRequest struct {
	RoleID string `json:"role_id"`
}
type RoleDescriptionRequest struct {
	Key              string `json:"key"`
	RoleID           string `json:"role_id"`
	Scope            string `json:"scope"`
	ExpectedRevision int64  `json:"expected_revision"`
	Description      string `json:"description"`
	SourceMessageID  string `json:"source_message_id"`
}
type RoleDescriptionView struct {
	Role                 work.Role                    `json:"role"`
	ProjectOverride      *work.ProjectRoleDescription `json:"project_override,omitempty"`
	EffectiveDescription string                       `json:"effective_description"`
	EffectiveVersion     int64                        `json:"effective_version"`
}

func (s *Service) RoleInspect(ctx context.Context, token, run string, req RoleInspectRequest) (RoleDescriptionView, error) {
	var view RoleDescriptionView
	err := s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopePersonal && req.RoleID != c.Worker.RoleID {
			return workauth.ErrUnauthorized
		}
		if scope.Kind == work.ScopeProject {
			if _, err = tx.GetProjectMember(scope.ProjectID, req.RoleID); err != nil {
				return err
			}
		}
		if view.Role, err = tx.GetRole(req.RoleID); err != nil {
			return err
		}
		view.EffectiveDescription = view.Role.Description
		view.EffectiveVersion = view.Role.DescriptionVersion
		if scope.Kind == work.ScopeProject {
			override, err := tx.GetProjectRoleDescription(scope.ProjectID, req.RoleID)
			if err == nil {
				view.ProjectOverride = &override
				view.EffectiveDescription = override.Description
				view.EffectiveVersion = override.DescriptionVersion
			} else if !errors.Is(err, workstore.ErrNotFound) {
				return err
			}
		}
		return nil
	})
	return view, err
}
func (s *Service) RoleUpdateDescription(ctx context.Context, token, run string, req RoleDescriptionRequest) (Result, error) {
	if strings.TrimSpace(req.Description) == "" || len(req.Description) > 32768 {
		return Result{}, fmt.Errorf("role responsibility must contain 1..32768 bytes")
	}
	if req.Scope != "global" && req.Scope != "project" {
		return Result{}, fmt.Errorf("description scope must explicitly be project or global")
	}
	var result Result
	apply := func(tx *workstore.Tx, c Caller, message work.Message) error {
		var err error
		result, err = s.mutate(tx, c, "role.update-description", req.Key, req.RoleID, req, func() (string, int64, error) {
			if req.Scope == "global" {
				role, err := tx.GetRole(req.RoleID)
				if err != nil {
					return "", 0, err
				}
				if role.Revision != req.ExpectedRevision {
					return "", 0, workstore.ErrConflict
				}
				if role.Description != req.Description {
					role.DescriptionVersion++
				}
				role.Description = req.Description
				role.Revision++
				role.UpdatedAt = s.now()
				if err = tx.PutRole(role, req.ExpectedRevision); err != nil {
					return "", 0, err
				}
				return role.ID, role.Revision, nil
			}
			scope, err := tx.GetScope(c.Worker.ScopeID)
			if err != nil {
				return "", 0, err
			}
			if scope.Kind != work.ScopeProject {
				return "", 0, workauth.ErrUnauthorized
			}
			project, err := tx.GetProject(scope.ProjectID)
			if err != nil {
				return "", 0, err
			}
			if project.LeaderRoleID != c.Worker.RoleID && req.RoleID != c.Worker.RoleID {
				return "", 0, workauth.ErrUnauthorized
			}
			next := work.ProjectRoleDescription{ProjectID: project.ID, RoleID: req.RoleID, Description: req.Description, DescriptionVersion: 1, Source: humanSource(message), UpdatedBy: message.Source, Revision: req.ExpectedRevision + 1, UpdatedAt: s.now()}
			if req.ExpectedRevision > 0 {
				old, err := tx.GetProjectRoleDescription(project.ID, req.RoleID)
				if err != nil {
					return "", 0, err
				}
				next.DescriptionVersion = old.DescriptionVersion
				if old.Description != req.Description {
					next.DescriptionVersion++
				}
			}
			if err = tx.PutProjectRoleDescription(next, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return project.ID + ":" + req.RoleID, next.Revision, nil
		})
		return err
	}
	if req.Scope == "global" {
		err := s.withManagement(ctx, token, run, req.SourceMessageID, "role.global-description", req.RoleID, apply)
		return result, err
	}
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
		if err != nil {
			return err
		}
		return apply(tx, c, message)
	})
	return result, err
}

func (s *Service) RoleList(ctx context.Context, token, run string, req KnowledgeListRequest) (KnowledgePage[work.Role], error) {
	var page KnowledgePage[work.Role]
	err := s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		roles, err := tx.ListRoles()
		if err != nil {
			return err
		}
		visible := []work.Role{}
		for _, role := range roles {
			if _, err = tx.GetRoleBotBinding(role.ID); err != nil {
				if errors.Is(err, workstore.ErrNotFound) {
					continue
				}
				return err
			}
			if scope.Kind == work.ScopePersonal && role.ID != c.Worker.RoleID {
				continue
			}
			if scope.Kind == work.ScopeProject {
				member, e := tx.GetProjectMember(scope.ProjectID, role.ID)
				if e != nil || member.State == work.ProjectMemberRemoved {
					continue
				}
			}
			visible = append(visible, role)
		}
		page, err = knowledgePage(visible, req, func(v work.Role) string { return v.ID })
		return err
	})
	return page, err
}
