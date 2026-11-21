package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) GetProjectRoleDescription(projectID, roleID string) (work.ProjectRoleDescription, error) {
	var v work.ProjectRoleDescription
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM project_role_descriptions WHERE project_id=? AND role_id=?`, projectID, roleID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (t *Tx) PutProjectRoleDescription(v work.ProjectRoleDescription, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Revision != expected+1 {
		return ErrConflict
	}
	project, err := t.GetProject(v.ProjectID)
	if err != nil {
		return err
	}
	member, err := t.GetProjectMember(v.ProjectID, v.RoleID)
	if err != nil {
		return err
	}
	if member.State == work.ProjectMemberRemoved {
		return fmt.Errorf("removed role cannot acquire project responsibilities")
	}
	message, e := t.GetMessage(v.Source.ID)
	if e != nil {
		return e
	}
	if v.Source.Kind != "message" || v.Source.EvidenceRef != message.BodyRef {
		return fmt.Errorf("description source must identify actual human message body")
	}
	if err = t.validateInstructionSource(project.ScopeID, v.UpdatedBy, v.Source); err != nil {
		return err
	}
	if expected == 0 {
		if v.DescriptionVersion != 1 {
			return fmt.Errorf("initial project description version must be one")
		}
	} else {
		old, err := t.GetProjectRoleDescription(v.ProjectID, v.RoleID)
		if err != nil {
			return err
		}
		want := old.DescriptionVersion
		if old.Description != v.Description {
			want++
		}
		if v.DescriptionVersion != want {
			return fmt.Errorf("description version must match actual change")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO project_role_descriptions(project_id,role_id,revision,payload) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, v.ProjectID, v.RoleID, v.Revision, string(raw))
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE project_role_descriptions SET revision=?,payload=? WHERE project_id=? AND role_id=? AND revision=?`, v.Revision, string(raw), v.ProjectID, v.RoleID, expected)
	}
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
