package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const SchemaNativeRequestsV1 = `
CREATE TABLE IF NOT EXISTS native_ui_requests (
 id TEXT PRIMARY KEY,
 scope_id TEXT NOT NULL REFERENCES scopes(id),
 worker_id TEXT NOT NULL REFERENCES workers(id),
 state TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0),
 payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS native_ui_requests_scope ON native_ui_requests(scope_id,state);
`

type NativeRequest struct {
	ID                  string                      `json:"id"`
	ScopeID             string                      `json:"scope_id"`
	CarrierIncarnation  string                      `json:"carrier_incarnation"`
	Binding             agentproto.ExecutionBinding `json:"binding"`
	NativeRequestID     string                      `json:"native_request_id"`
	NativeSessionID     string                      `json:"native_session_id"`
	NativeTurnID        string                      `json:"native_turn_id"`
	State               string                      `json:"state"` // open, response_pending, resolved, stale
	PromptRef           string                      `json:"prompt_ref,omitempty"`
	FormRef             string                      `json:"form_ref,omitempty"`
	ResolvedEvidenceRef string                      `json:"resolved_evidence_ref,omitempty"`
	ResponseOperationID string                      `json:"response_operation_id,omitempty"`
	Revision            int64                       `json:"revision"`
	CreatedAt           time.Time                   `json:"created_at"`
	UpdatedAt           time.Time                   `json:"updated_at"`
}

func (t *Tx) PutNativeRequest(v NativeRequest, expected int64) error {
	if v.ID == "" || v.NativeRequestID == "" || v.CarrierIncarnation == "" || v.Binding.ValidateExecution() != nil || v.ScopeID != v.Binding.ScopeID || v.Revision != expected+1 {
		return fmt.Errorf("native request requires exact execution identity and revision")
	}
	if v.State != "open" && v.State != "response_pending" && v.State != "resolved" && v.State != "stale" {
		return fmt.Errorf("invalid native request state")
	}
	for _, ref := range []string{v.PromptRef, v.FormRef, v.ResolvedEvidenceRef} {
		if ref != "" {
			if err := t.requireContent(v.ScopeID, ref); err != nil {
				return err
			}
		}
	}
	if _, err := t.ValidateNativeBinding(v.Binding); err != nil {
		return err
	}
	if expected > 0 {
		old, err := t.GetNativeRequest(v.ID)
		if err != nil {
			return err
		}
		if old.ScopeID != v.ScopeID || old.CarrierIncarnation != v.CarrierIncarnation || old.Binding != v.Binding || old.NativeRequestID != v.NativeRequestID || old.NativeSessionID != v.NativeSessionID || old.NativeTurnID != v.NativeTurnID || old.PromptRef != "" && old.PromptRef != v.PromptRef {
			return ErrIdempotencyConflict
		}
		if (old.State == "resolved" || old.State == "stale") && v.State != old.State {
			return fmt.Errorf("obsolete native request cannot reopen")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO native_ui_requests(id,scope_id,worker_id,state,revision,payload) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, v.ID, v.ScopeID, v.Binding.WorkerSessionID, v.State, v.Revision, string(raw))
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE native_ui_requests SET state=?,revision=?,payload=? WHERE id=? AND revision=?`, v.State, v.Revision, string(raw), v.ID, expected)
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

func (t *Tx) GetNativeRequest(id string) (NativeRequest, error) {
	var v NativeRequest
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM native_ui_requests WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

func (t *Tx) ListNativeRequests(scopeID string) ([]NativeRequest, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM native_ui_requests WHERE scope_id=? ORDER BY id`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NativeRequest
	for rows.Next() {
		var raw string
		var v NativeRequest
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
