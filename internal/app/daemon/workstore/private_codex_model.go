package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// PrivateCodexObservedModel reads only a journaled turn/start that the runtime
// accepted into the exact carrier and session's native-turn ledger.
func (t *Tx) PrivateCodexObservedModel(incarnation, sessionID string) (*agentproto.CodexEffectiveThreadContract, error) {
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT r.payload FROM runtime_receipts r
 JOIN native_turns n ON n.carrier_incarnation=r.carrier_incarnation AND n.session_id=json_extract(r.payload,'$.threadId') AND n.turn_id=json_extract(r.payload,'$.turnId')
 WHERE r.carrier_incarnation=? AND n.session_id=? AND json_extract(n.payload,'$.started')=1
 AND NOT EXISTS(SELECT 1 FROM runtime_quarantine q WHERE q.incarnation=r.carrier_incarnation AND q.sequence=r.sequence)
 AND coalesce(json_extract(r.payload,'$.trafficClass'),'')<>'internal_helper'
 AND coalesce(json_extract(r.payload,'$.initiator.kind'),'')<>'internal_helper'
 AND json_extract(r.payload,'$.kind')=? AND coalesce(json_extract(r.payload,'$.codexEffectiveThread.model'),'')<>''
 ORDER BY r.sequence DESC LIMIT 1`, incarnation, sessionID, string(agentproto.EventTurnStarted)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var event agentproto.Event
	if err = json.Unmarshal([]byte(raw), &event); err != nil {
		return nil, err
	}
	return event.CodexEffectiveThread, nil
}
