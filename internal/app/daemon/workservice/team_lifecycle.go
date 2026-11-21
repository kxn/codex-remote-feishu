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

type TeamCreateRequest struct {
	Key             string   `json:"key"`
	Name            string   `json:"name"`
	LeaderRoleID    string   `json:"leader_role_id"`
	MemberRoleIDs   []string `json:"member_role_ids"`
	SourceMessageID string   `json:"source_message_id"`
}
type TeamUpdateRequest struct {
	Key              string `json:"key"`
	TeamID           string `json:"team_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Name             string `json:"name"`
	LeaderRoleID     string `json:"leader_role_id"`
	SourceMessageID  string `json:"source_message_id"`
}
type TeamMemberRequest struct {
	Key                    string `json:"key"`
	TeamID                 string `json:"team_id"`
	ExpectedRevision       int64  `json:"expected_revision"`
	RoleID                 string `json:"role_id"`
	ExpectedMemberRevision int64  `json:"expected_member_revision"`
	SourceMessageID        string `json:"source_message_id"`
}
type TeamInspectRequest struct {
	TeamID string `json:"team_id"`
}
type TeamView struct {
	Team    work.Team         `json:"team"`
	Members []work.TeamMember `json:"members"`
}

func (s *Service) TeamCreate(ctx context.Context, token, run string, req TeamCreateRequest) (Result, error) {
	var result Result
	err := s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeBootstrap {
			return workauth.ErrUnauthorized
		}
		message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
		if err != nil {
			return err
		}
		if message.Source.ID != scope.PrincipalID || req.LeaderRoleID != c.Worker.RoleID {
			return workauth.ErrUnauthorized
		}
		id := stableID("team", scope.ID, scope.PrincipalID, req.Key)
		result, err = s.mutate(tx, c, "team.create", req.Key, id, req, func() (string, int64, error) {
			if strings.TrimSpace(req.Name) == "" {
				return "", 0, fmt.Errorf("team name required")
			}
			selected := map[string]bool{req.LeaderRoleID: true}
			for _, role := range req.MemberRoleIDs {
				if role == "" {
					return "", 0, fmt.Errorf("empty team role")
				}
				selected[role] = true
			}
			for role := range selected {
				if _, err = tx.GetRole(role); err != nil {
					return "", 0, err
				}
				if _, err = tx.GetRoleBotBinding(role); err != nil {
					return "", 0, err
				}
			}
			team := work.Team{ID: id, Name: req.Name, LeaderRoleID: req.LeaderRoleID, Revision: 1}
			if err = tx.PutTeam(team, 0); err != nil {
				return "", 0, err
			}
			for role := range selected {
				if err = tx.PutTeamMember(work.TeamMember{TeamID: id, RoleID: role, Active: true, Revision: 1}, 0); err != nil {
					return "", 0, err
				}
			}
			if err = tx.PutTeamOwnership(work.TeamOwnership{TeamID: id, PrincipalID: scope.PrincipalID, Source: humanSource(message), UpdatedBy: message.Source, Revision: 1}, 0); err != nil {
				return "", 0, err
			}
			return id, 1, nil
		})
		return err
	})
	return result, err
}
func (s *Service) TeamList(ctx context.Context, token, run string, req KnowledgeListRequest) (KnowledgePage[work.Team], error) {
	var page KnowledgePage[work.Team]
	err := s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		teams, err := tx.ListTeams()
		if err != nil {
			return err
		}
		visible := []work.Team{}
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		for _, team := range teams {
			member, e := tx.GetTeamMember(team.ID, c.Worker.RoleID)
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			if e == nil && member.Active {
				visible = append(visible, team)
				continue
			}
			if scope.PrincipalID != "" {
				owner, e := tx.GetTeamOwnership(team.ID)
				if e != nil && !errors.Is(e, workstore.ErrNotFound) {
					return e
				}
				if e == nil && owner.PrincipalID == scope.PrincipalID {
					visible = append(visible, team)
				}
			}
		}
		page, err = knowledgePage(visible, req, func(v work.Team) string { return v.ID })
		return err
	})
	return page, err
}
func (s *Service) TeamInspect(ctx context.Context, token, run string, req TeamInspectRequest) (TeamView, error) {
	var view TeamView
	err := s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		member, e := tx.GetTeamMember(req.TeamID, c.Worker.RoleID)
		if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		allowed := e == nil && member.Active
		if !allowed && scope.PrincipalID != "" {
			owner, e := tx.GetTeamOwnership(req.TeamID)
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			allowed = e == nil && owner.PrincipalID == scope.PrincipalID
		}
		if !allowed {
			return workauth.ErrUnauthorized
		}
		if view.Team, err = tx.GetTeam(req.TeamID); err != nil {
			return err
		}
		view.Members, err = tx.ListTeamMembers(req.TeamID)
		return err
	})
	return view, err
}
func (s *Service) TeamUpdate(ctx context.Context, token, run string, req TeamUpdateRequest) (Result, error) {
	var result Result
	err := s.withManagement(ctx, token, run, req.SourceMessageID, "team.manage", req.TeamID, func(tx *workstore.Tx, c Caller, _ work.Message) error {
		var err error
		result, err = s.mutate(tx, c, "team.update", req.Key, req.TeamID, req, func() (string, int64, error) {
			team, err := tx.GetTeam(req.TeamID)
			if err != nil {
				return "", 0, err
			}
			if team.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			member, err := tx.GetTeamMember(team.ID, req.LeaderRoleID)
			if err != nil || !member.Active {
				return "", 0, fmt.Errorf("team leader must be an active roster member")
			}
			if strings.TrimSpace(req.Name) == "" {
				return "", 0, fmt.Errorf("team name required")
			}
			team.Name = req.Name
			team.LeaderRoleID = req.LeaderRoleID
			team.Revision++
			if err = tx.PutTeam(team, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return team.ID, team.Revision, nil
		})
		return err
	})
	return result, err
}
func (s *Service) teamMember(ctx context.Context, token, run string, req TeamMemberRequest, active bool) (Result, error) {
	kind := "team.member.remove"
	if active {
		kind = "team.member.add"
	}
	var result Result
	err := s.withManagement(ctx, token, run, req.SourceMessageID, "team.manage", req.TeamID, func(tx *workstore.Tx, c Caller, _ work.Message) error {
		var err error
		result, err = s.mutate(tx, c, kind, req.Key, req.TeamID, req, func() (string, int64, error) {
			team, err := tx.GetTeam(req.TeamID)
			if err != nil {
				return "", 0, err
			}
			if team.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			if !active {
				if team.LeaderRoleID == req.RoleID {
					return "", 0, fmt.Errorf("select a new team leader before removing the current leader")
				}
				projects, err := tx.ListProjects()
				if err != nil {
					return "", 0, err
				}
				for _, project := range projects {
					if project.TeamID != team.ID || project.State == work.ProjectArchived {
						continue
					}
					member, e := tx.GetProjectMember(project.ID, req.RoleID)
					if e != nil && !errors.Is(e, workstore.ErrNotFound) {
						return "", 0, e
					}
					if e == nil && member.State != work.ProjectMemberRemoved {
						return "", 0, fmt.Errorf("remove role from project %s before removing its team membership", project.ID)
					}
				}
			} else {
				if _, err = tx.GetRoleBotBinding(req.RoleID); err != nil {
					return "", 0, err
				}
			}
			member := work.TeamMember{TeamID: team.ID, RoleID: req.RoleID, Active: active, Revision: req.ExpectedMemberRevision + 1}
			if err = tx.PutTeamMember(member, req.ExpectedMemberRevision); err != nil {
				return "", 0, err
			}
			team.Revision++
			if err = tx.PutTeam(team, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			return team.ID, team.Revision, nil
		})
		return err
	})
	return result, err
}
func (s *Service) TeamMemberAdd(ctx context.Context, t, r string, q TeamMemberRequest) (Result, error) {
	return s.teamMember(ctx, t, r, q, true)
}
func (s *Service) TeamMemberRemove(ctx context.Context, t, r string, q TeamMemberRequest) (Result, error) {
	return s.teamMember(ctx, t, r, q, false)
}
