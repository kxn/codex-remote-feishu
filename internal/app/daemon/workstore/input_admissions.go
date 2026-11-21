package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaInputAdmissionsV1 = `CREATE TABLE attempt_input_admissions(id TEXT PRIMARY KEY,command_id TEXT NOT NULL UNIQUE REFERENCES operations(id),attempt_id TEXT NOT NULL REFERENCES attempts(id),payload TEXT NOT NULL CHECK(json_valid(payload)));CREATE INDEX attempt_admissions ON attempt_input_admissions(attempt_id);`

func (t *Tx) GetAttemptInputAdmission(id string) (work.AttemptInputAdmission, error) {
	var v work.AttemptInputAdmission
	var raw string
	err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM attempt_input_admissions WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (t *Tx) GetAttemptInputAdmissionByCommand(commandID string) (work.AttemptInputAdmission, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM attempt_input_admissions WHERE command_id=?", commandID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.AttemptInputAdmission{}, ErrNotFound
	}
	if err != nil {
		return work.AttemptInputAdmission{}, err
	}
	return t.GetAttemptInputAdmission(id)
}
func (t *Tx) ListAttemptInputAdmissions(attemptID string) ([]work.AttemptInputAdmission, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM attempt_input_admissions WHERE attempt_id=? ORDER BY rowid", attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []work.AttemptInputAdmission
	for rows.Next() {
		var raw string
		var v work.AttemptInputAdmission
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}
func (t *Tx) HasPendingAttemptInputAdmissions(attemptID string) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM attempt_input_admissions WHERE attempt_id=? AND json_extract(payload,'$.state') IN ('sending','unknown')", attemptID).Scan(&n)
	return n > 0, err
}

// EffectiveAttemptInputIDs retains the initial immutable prompt snapshot and
// adds only separately confirmed native steer admissions.
func (t *Tx) EffectiveAttemptInputIDs(attemptID string) ([]string, error) {
	attempt, err := t.GetAttempt(attemptID)
	if err != nil {
		return nil, err
	}
	ids := append([]string(nil), attempt.InputIDs...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	admissions, err := t.ListAttemptInputAdmissions(attemptID)
	if err != nil {
		return nil, err
	}
	for _, v := range admissions {
		if v.State != work.InputAdmissionConfirmed {
			continue
		}
		for _, id := range v.InputIDs {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	return ids, nil
}
func (t *Tx) effectiveAttemptInputs(attempt work.ExecutionAttempt) (work.ExecutionAttempt, error) {
	ids, err := t.EffectiveAttemptInputIDs(attempt.ID)
	attempt.InputIDs = ids
	return attempt, err
}
