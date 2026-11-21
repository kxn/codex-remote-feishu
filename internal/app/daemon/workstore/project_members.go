package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) PutProjectMember(v work.ProjectMember, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if expected < 0 || v.Revision != expected+1 {
		return ErrConflict
	}
	project, err := t.GetProject(v.ProjectID)
	if err != nil {
		return err
	}
	teamMember, err := t.GetTeamMember(project.TeamID, v.RoleID)
	if err != nil || !teamMember.Active {
		return fmt.Errorf("role is not an active team candidate")
	}
	if expected > 0 {
		old, e := t.GetProjectMember(v.ProjectID, v.RoleID)
		if e != nil {
			return e
		}
		if v.BotGeneration < old.BotGeneration {
			return fmt.Errorf("project member bot generation regressed")
		}
	}
	data := mustJSON(v)
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, "INSERT INTO project_members(project_id,role_id,revision,payload) VALUES(?,?,?,?) ON CONFLICT(project_id,role_id) DO NOTHING", v.ProjectID, v.RoleID, v.Revision, data)
	} else {
		result, err = t.tx.ExecContext(t.ctx, "UPDATE project_members SET revision=?,payload=? WHERE project_id=? AND role_id=? AND revision=?", v.Revision, data, v.ProjectID, v.RoleID, expected)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
func (t *Tx) GetProjectMember(projectID, roleID string) (work.ProjectMember, error) {
	var v work.ProjectMember
	var payload string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM project_members WHERE project_id=? AND role_id=?", projectID, roleID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(payload), &v)
	return v, err
}
func (t *Tx) ListProjectMembers(projectID string) ([]work.ProjectMember, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM project_members WHERE project_id=? ORDER BY role_id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []work.ProjectMember{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var v work.ProjectMember
		if err = json.Unmarshal([]byte(payload), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (t *Tx) requireProjectReady(projectID, roleID string) error {
	project, err := t.GetProject(projectID)
	if err != nil {
		return err
	}
	if project.State != work.ProjectActive && project.State != work.ProjectDegraded {
		return fmt.Errorf("project is not active")
	}
	member, err := t.GetProjectMember(projectID, roleID)
	if err != nil || member.State != work.ProjectMemberReady {
		return ErrScopeDenied
	}
	team, err := t.GetTeamMember(project.TeamID, roleID)
	if err != nil || !team.Active {
		return ErrScopeDenied
	}
	return nil
}

func (t *Tx) requireRoleInScope(scopeID, roleID string) error {
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeLibrary {
		return ErrScopeDenied
	}
	if scope.Kind == work.ScopePersonal || scope.Kind == work.ScopeBootstrap {
		if scope.RoleID != roleID {
			return ErrScopeDenied
		}
		return nil
	}
	member, err := t.GetProjectMember(scope.ProjectID, roleID)
	if err != nil || member.State == work.ProjectMemberRemoved {
		return ErrScopeDenied
	}
	team, err := t.GetTeamMember(scope.TeamID, roleID)
	if err != nil || !team.Active {
		return ErrScopeDenied
	}
	return nil
}
