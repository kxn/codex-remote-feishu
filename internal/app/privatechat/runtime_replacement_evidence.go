package privatechat

import (
	"context"
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (p *Processor) appliedRetiredThreadFact(ctx context.Context, old agentproto.NativeOwner, thread string) (*agentproto.Event, error) {
	rows, err := p.journal.db.QueryContext(ctx, `SELECT payload,payload_hash FROM native_events WHERE incarnation=? AND applied=1 AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=? ORDER BY sequence DESC`, old.CarrierIncarnation, string(agentproto.EventThreadDiscovered), thread)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var hash string
		if err = rows.Scan(&raw, &hash); err != nil {
			return nil, err
		}
		var event agentproto.Event
		if json.Unmarshal(raw, &event) != nil {
			return nil, ErrJournalConflict
		}
		actual, e := agentproto.RuntimeEventPayloadHash(event)
		if e != nil || actual != hash {
			return nil, ErrJournalConflict
		}
		if event.Kind == agentproto.EventThreadDiscovered && event.ThreadID == thread && event.CWD != "" && (event.NativeBinding == nil || event.NativeBinding.Owner == old) {
			return &event, nil
		}
	}
	return nil, rows.Err()
}
