package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaDesiredLocationV1 = `
CREATE TABLE IF NOT EXISTS desired_execution_locations (
 context_id TEXT PRIMARY KEY REFERENCES contexts(id),
 scope_id TEXT NOT NULL REFERENCES scopes(id),
 location_id TEXT NOT NULL REFERENCES locations(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 payload TEXT NOT NULL
);
`

type DesiredExecutionLocation struct {
	ContextID            string      `json:"context_id"`
	ScopeID              string      `json:"scope_id"`
	LocationID           string      `json:"location_id"`
	LocationGeneration   int64       `json:"location_generation"`
	RootLocationID       string      `json:"root_location_id"`
	AuthorizingAttemptID string      `json:"authorizing_attempt_id"`
	RunRef               work.RunRef `json:"run_ref"`
	VerificationRef      string      `json:"verification_ref"`
	Revision             int64       `json:"revision"`
}

// PutDesiredExecutionLocation stages a verified, agent-prepared location. It
// does not move a live native process or release its old resource occupancy.
func (t *Tx) PutDesiredExecutionLocation(v DesiredExecutionLocation, expected int64) error {
	wc, err := t.GetContext(v.ContextID)
	if err != nil {
		return err
	}
	if wc.ScopeID != v.ScopeID || v.Revision != expected+1 {
		return ErrConflict
	}
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeBootstrap {
		return ErrScopeDenied
	}
	preference, err := t.GetExecutionPreference(v.ScopeID)
	if err != nil {
		return err
	}
	if !slices.Contains(preference.SelectedLocationIDs, v.RootLocationID) {
		return ErrScopeDenied
	}
	root, err := t.GetLocation(v.RootLocationID)
	if err != nil {
		return err
	}
	location, err := t.GetLocation(v.LocationID)
	if err != nil {
		return err
	}
	if location.HostID != root.HostID || location.Generation != v.LocationGeneration {
		return ErrConflict
	}
	attempt, err := t.GetAttempt(v.AuthorizingAttemptID)
	if err != nil {
		return err
	}
	worker, err := t.GetWorker(v.RunRef.WorkerID)
	if err != nil {
		return err
	}
	if attempt.RunRef != v.RunRef || attempt.ContextID != wc.ID || worker.ContextID != wc.ID || attempt.State != work.AttemptRunning || work.ValidateAttemptBinding(attempt, worker) != nil {
		return ErrScopeDenied
	}
	if err = t.requireContent(v.ScopeID, v.VerificationRef); err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO desired_execution_locations(context_id,scope_id,location_id,revision,payload) VALUES(?,?,?,?,?) ON CONFLICT(context_id) DO NOTHING`, v.ContextID, v.ScopeID, v.LocationID, v.Revision, string(raw))
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE desired_execution_locations SET location_id=?,revision=?,payload=? WHERE context_id=? AND scope_id=? AND revision=?`, v.LocationID, v.Revision, string(raw), v.ContextID, v.ScopeID, expected)
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

func (t *Tx) GetDesiredExecutionLocation(contextID string) (DesiredExecutionLocation, error) {
	var v DesiredExecutionLocation
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM desired_execution_locations WHERE context_id=?`, contextID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
