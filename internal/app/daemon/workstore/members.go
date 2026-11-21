package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) PutTeamMember(v work.TeamMember, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if expected < 0 || v.Revision != expected+1 {
		return ErrConflict
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO team_members(team_id,role_id,revision,payload) VALUES(?,?,?,?)`, v.TeamID, v.RoleID, v.Revision, string(payload))
		return err
	}
	result, err := t.tx.ExecContext(t.ctx, `UPDATE team_members SET revision=?,payload=? WHERE team_id=? AND role_id=? AND revision=?`, v.Revision, string(payload), v.TeamID, v.RoleID, expected)
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

func (t *Tx) GetTeamMember(teamID, roleID string) (work.TeamMember, error) {
	var payload string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM team_members WHERE team_id=? AND role_id=?`, teamID, roleID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return work.TeamMember{}, ErrNotFound
	}
	if err != nil {
		return work.TeamMember{}, err
	}
	var v work.TeamMember
	err = json.Unmarshal([]byte(payload), &v)
	return v, err
}

func (t *Tx) ListTeamMembers(teamID string) ([]work.TeamMember, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM team_members WHERE team_id=? ORDER BY role_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []work.TeamMember{}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var v work.TeamMember
		if err = json.Unmarshal([]byte(payload), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
