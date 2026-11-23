package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPrivateExecutionDisplayV1 = `CREATE TABLE IF NOT EXISTS private_execution_display(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)));
CREATE INDEX IF NOT EXISTS private_execution_display_pending_preview ON private_execution_display(json_extract(payload,'$.Binding.attemptId'),id) WHERE json_extract(payload,'$.PreviewTerminal') IS NOT NULL;
CREATE TABLE IF NOT EXISTS native_thread_token_usage(incarnation TEXT NOT NULL REFERENCES carrier_registrations(incarnation),session_id TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),PRIMARY KEY(incarnation,session_id))`

type PrivateExecutionDisplay struct {
	ID                   string
	Revision             int64
	Binding              agentproto.ExecutionBinding
	Incarnation          string
	SessionID            string
	TurnID               string
	RejectionCommandID   string
	Tenant               string
	ChatID               string
	ReplyToMessageID     string
	Bot                  work.RoleBotBinding
	LatestTokenUsage     *agentproto.ThreadTokenUsage
	PublicSummaryOffsets map[string]int
	StartedAt            time.Time
	LastOutboxID         string
	ProgressAnchorID     string
	ProgressFenceID      string
	ProgressHash         string
	Progress             state.ExecCommandProgressRecord
	Plan                 *control.PlanUpdate
	TurnDiff             *control.TurnDiffSnapshot
	PreviewState         string
	PreviewSource        string
	PreviewBody          string
	PreviewDiff          *control.TurnDiffPreview
	PreviewTerminal      *agentproto.Event
	PreviewTerminalAt    time.Time
	Files                map[string]control.FileChangeSummaryEntry
	Pending              string
	PendingItem          string
	PendingFinal         bool
	SeenMessages         map[string]bool
	Terminated           bool
	HadOutput            bool
}

func (v PrivateExecutionDisplay) Validate() error {
	if v.ID == "" || v.ChatID == "" || v.Tenant == "" || v.Incarnation == "" || v.SessionID == "" || v.TurnID == "" || v.Bot.RoleID != v.Binding.RoleID || v.Bot.IdentityGeneration != v.Binding.BotGeneration {
		return fmt.Errorf("private execution display requires an exact turn and destination")
	}
	return v.Binding.ValidateExecution()
}

func (t *Tx) GetPrivateExecutionDisplay(id string) (PrivateExecutionDisplay, error) {
	var v PrivateExecutionDisplay
	err := t.read("private_execution_display", id, &v)
	return v, err
}

func (t *Tx) PutPrivateExecutionDisplay(v PrivateExecutionDisplay, expected int64) error {
	return t.record("private_execution_display", v.ID, v.Revision, expected, v)
}

// PrivateRunPublications compares complete bodies within the same authenticated
// run. An uncertain send already owns the UUID and must not be repeated under a
// different operation ID.
func (t *Tx) PrivateRunPublications(scopeID, contextID, runRef string) ([]work.Operation, error) {
	return listRecords[work.Operation](t, "operations", ` WHERE scope_id=? AND kind='feishu.publish'
		AND json_extract(payload,'$.context_id')=? AND json_extract(payload,'$.state')<>'terminal'
		AND json_extract((SELECT CAST(data AS TEXT) FROM content WHERE id=json_extract(operations.payload,'$.request_ref')),'$.source_run_ref')=?`, scopeID, contextID, runRef)
}

func (t *Tx) SupersedePrivateDisplayUpdate(id string, now time.Time) (bool, error) {
	item, err := t.GetOutbox(id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil || (item.State != work.OutboxPending && item.State != work.OutboxRetryable) {
		return false, err
	}
	op, err := t.GetOperation(item.OperationID)
	if err != nil {
		return false, err
	}
	if op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"}) {
		return false, ErrScopeDenied
	}
	lastOp, lastItem := op.Revision, item.Revision
	op.State, item.State = work.OperationTerminal, work.OutboxTerminal
	op.Revision, item.Revision = lastOp+1, lastItem+1
	op.UpdatedAt, op.LastError = now, "newer private execution progress"
	if _, _, err = t.PutOperation(op, lastOp); err != nil {
		return false, err
	}
	return true, t.PutOutbox(item, lastItem)
}

// ListPendingPrivatePreviews returns accepted terminal displays awaiting their
// durable preview decision, including interrupted preparations after restart.
func (t *Tx) ListPendingPrivatePreviews(limit int) ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id FROM private_execution_display d WHERE json_extract(d.payload,'$.PreviewTerminal') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=json_extract(d.payload,'$.Binding.scopeId')) ORDER BY json_extract(d.payload,'$.Binding.attemptId'),id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (t *Tx) HasPendingPrivatePreview(attemptID string) (bool, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT d.payload,p.payload_hash FROM private_execution_display d LEFT JOIN personal_migration_preserved p ON p.kind='preview' AND p.scope_id=json_extract(d.payload,'$.Binding.scopeId') AND p.record_id=d.id AND p.record_revision=d.revision AND p.payload=d.payload AND json_extract(d.payload,'$.PreviewState')='queued' AND EXISTS (SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=p.scope_id AND f.state='draining') WHERE json_extract(d.payload,'$.PreviewTerminal') IS NOT NULL AND json_extract(d.payload,'$.Binding.attemptId')=?`, attemptID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var saved sql.NullString
		if err = rows.Scan(&raw, &saved); err != nil {
			return false, err
		}
		sum := sha256.Sum256([]byte(raw))
		if !saved.Valid || saved.String != hex.EncodeToString(sum[:]) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (t *Tx) ListPersonalMigrationPreviews(scopeID string) ([]PrivateExecutionDisplay, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM private_execution_display WHERE json_extract(payload,'$.Binding.scopeId')=? AND json_extract(payload,'$.PreviewTerminal') IS NOT NULL ORDER BY id`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PrivateExecutionDisplay
	for rows.Next() {
		var raw string
		var display PrivateExecutionDisplay
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &display); err != nil {
			return nil, err
		}
		result = append(result, display)
	}
	return result, rows.Err()
}
