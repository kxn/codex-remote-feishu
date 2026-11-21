package workstore

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Tx) NativeFinalMessageEvents(incarnation, session, turn string, before uint64) ([]agentproto.Event, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM runtime_receipts WHERE carrier_incarnation=? AND sequence<? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.itemKind')='agent_message' ORDER BY sequence LIMIT 1025`, incarnation, before, session, turn, agentproto.EventItemCompleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []agentproto.Event
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event agentproto.Event
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if len(events) > 1024 {
		return nil, fmt.Errorf("review final message count exceeds its bounded evidence limit")
	}
	return events, rows.Err()
}
