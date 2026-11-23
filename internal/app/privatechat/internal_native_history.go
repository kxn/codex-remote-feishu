package privatechat

import (
	"context"
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// InternalNativeSessions returns only sessions proven by an accepted,
// owner-only OpenCode model-catalog RPC. It never infers from names or paths.
func (j *Journal) InternalNativeSessions(ctx context.Context) ([]NativeSession, error) {
	rows, err := j.db.QueryContext(ctx, `SELECT d.backend,d.namespace,d.carrier,d.carrier_generation,n.payload,e.payload FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id JOIN native_events e ON e.incarnation=d.carrier AND e.sequence=d.event_sequence AND e.command_id=d.command_id WHERE d.state='accepted' AND d.backend='opencode'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NativeSession
	for rows.Next() {
		var s NativeSession
		var cmdRaw, eventRaw []byte
		if err = rows.Scan(&s.Backend, &s.Namespace, &s.CarrierID, &s.CarrierGeneration, &cmdRaw, &eventRaw); err != nil {
			return nil, err
		}
		var cmd agentproto.Command
		var event agentproto.Event
		if json.Unmarshal(cmdRaw, &cmd) != nil || json.Unmarshal(eventRaw, &event) != nil {
			return nil, ErrJournalConflict
		}
		if cmd.Kind == agentproto.CommandModelList && cmd.NativeBinding != nil && cmd.NativeBinding.ExecutionID == "" && event.Kind == agentproto.EventModelCatalogUpdated && event.ThreadID != "" && event.CommandID == cmd.CommandID {
			s.SessionID = event.ThreadID
			result = append(result, s)
		}
	}
	return result, rows.Err()
}

// IsInternalNativeSession uses the same durable proof as catalog filtering.
func (j *Journal) IsInternalNativeSession(ctx context.Context, backend, namespace, session string) (bool, error) {
	entries, err := j.InternalNativeSessions(ctx)
	if err != nil {
		return false, err
	}
	for _, v := range entries {
		if v.Backend == backend && v.Namespace == namespace && v.SessionID == session {
			return true, nil
		}
	}
	return false, nil
}
