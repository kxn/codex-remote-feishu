package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) GetTeamOwnership(teamID string) (work.TeamOwnership, error) {
	var v work.TeamOwnership
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM team_ownership WHERE team_id=?`, teamID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

// PutTeamOwnership requires a saved actual principal message. The application
// must additionally prove old-owner/trusted-admin authority before a transfer.
func (t *Tx) PutTeamOwnership(v work.TeamOwnership, expected int64) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Revision != expected+1 {
		return ErrConflict
	}
	if _, err := t.GetTeam(v.TeamID); err != nil {
		return err
	}
	if _, err := t.GetPrincipal(v.PrincipalID); err != nil {
		return err
	}
	if v.Source.Kind != "message" {
		return fmt.Errorf("team ownership must cite actual human input")
	}
	message, err := t.GetMessage(v.Source.ID)
	if err != nil {
		return err
	}
	if v.Source.EvidenceRef != message.BodyRef {
		return fmt.Errorf("ownership source must identify actual human message body")
	}
	if err = t.validateInstructionSource(message.ScopeID, v.UpdatedBy, v.Source); err != nil {
		return err
	}
	if expected == 0 {
		scope, err := t.GetScope(message.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeBootstrap || scope.PrincipalID != v.PrincipalID || message.Source.ID != v.PrincipalID {
			return fmt.Errorf("new team owner must be the actual bootstrap initiator")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO team_ownership(team_id,principal_id,revision,payload) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, v.TeamID, v.PrincipalID, v.Revision, string(raw))
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE team_ownership SET principal_id=?,revision=?,payload=? WHERE team_id=? AND revision=?`, v.PrincipalID, v.Revision, string(raw), v.TeamID, expected)
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
