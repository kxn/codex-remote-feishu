package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const SchemaDispatchProblemsV1 = `CREATE TABLE team_dispatch_problems (id TEXT PRIMARY KEY,scope_id TEXT NOT NULL REFERENCES scopes(id),payload TEXT NOT NULL CHECK(json_valid(payload)))`

type DispatchProblem struct {
	ID         string    `json:"id"`
	ScopeID    string    `json:"scope_id"`
	ContextID  string    `json:"context_id,omitempty"`
	RoleID     string    `json:"role_id"`
	TaskID     string    `json:"task_id,omitempty"`
	Reason     string    `json:"reason"`
	Revision   int64     `json:"revision"`
	ObservedAt time.Time `json:"observed_at"`
}

// Diagnostic projection only. It never grants execution or changes a task.
// A failed scheduler scan must not call this method and erase earlier evidence.
func (t *Tx) ReplaceDispatchProblems(problems []DispatchProblem, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("diagnostic observation time required")
	}
	return t.atomic(func() error {
		keep := map[string]bool{}
		for _, v := range problems {
			if v.ScopeID == "" || v.RoleID == "" || v.Reason == "" {
				return fmt.Errorf("diagnostic execution identity required")
			}
			key, _ := json.Marshal([]string{v.ScopeID, v.ContextID, v.RoleID, v.TaskID, v.Reason})
			hash := sha256.Sum256(key)
			v.ID = "dispatch:" + hex.EncodeToString(hash[:20])
			if keep[v.ID] {
				continue
			}
			keep[v.ID] = true
			v.Revision = 1
			v.ObservedAt = at
			var previous string
			err := t.tx.QueryRowContext(t.ctx, "SELECT payload FROM team_dispatch_problems WHERE id=?", v.ID).Scan(&previous)
			if err == nil {
				var old DispatchProblem
				if err = json.Unmarshal([]byte(previous), &old); err != nil {
					return err
				}
				v.Revision = old.Revision
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if _, err = t.tx.ExecContext(t.ctx, "INSERT INTO team_dispatch_problems(id,scope_id,payload) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", v.ID, v.ScopeID, string(raw)); err != nil {
				return err
			}
		}
		rows, err := t.tx.QueryContext(t.ctx, "SELECT id FROM team_dispatch_problems")
		if err != nil {
			return err
		}
		var remove []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !keep[id] {
				remove = append(remove, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range remove {
			if _, err = t.tx.ExecContext(t.ctx, "DELETE FROM team_dispatch_problems WHERE id=?", id); err != nil {
				return err
			}
		}
		return nil
	})
}
func (t *Tx) ListDispatchProblems(scopeID string) ([]DispatchProblem, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM team_dispatch_problems WHERE scope_id=? ORDER BY id", scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DispatchProblem
	for rows.Next() {
		var raw string
		var value DispatchProblem
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
