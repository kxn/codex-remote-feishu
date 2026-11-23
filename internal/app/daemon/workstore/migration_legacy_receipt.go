package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// LegacyCarrierSessionReceipt obtains one exact, independently persisted source
// receipt linking a carrier and worker owner to a native session.
func (t *Tx) LegacyCarrierSessionReceipt(scopeID, workerID, incarnation, session string) (agentproto.Event, error) {
	var event agentproto.Event
	if scopeID == "" || workerID == "" || incarnation == "" || session == "" {
		return event, ErrScopeDenied
	}
	var raw []byte
	var expected string
	var sequence uint64
	err := t.tx.QueryRowContext(t.ctx, `SELECT sequence,payload_hash,payload FROM runtime_receipts WHERE carrier_incarnation=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.execution.scopeId')=? AND json_extract(payload,'$.execution.workerSessionId')=? ORDER BY sequence LIMIT 1`, incarnation, session, scopeID, workerID).Scan(&sequence, &expected, &raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return event, ErrNotFound
		}
		return event, err
	}
	if json.Unmarshal(raw, &event) != nil || event.Journal == nil || event.Execution == nil || event.Journal.CarrierIncarnation != incarnation || event.Journal.Sequence != sequence || event.Journal.PayloadHash != expected || event.Execution.ScopeID != scopeID || event.Execution.WorkerSessionID != workerID || event.ThreadID != session {
		return agentproto.Event{}, ErrConflict
	}
	actual, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		return agentproto.Event{}, err
	}
	if actual != expected {
		return agentproto.Event{}, fmt.Errorf("legacy source receipt hash mismatch: %w", ErrConflict)
	}
	return event, nil
}
