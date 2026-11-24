package privatechat

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// Only previously applied, hash-verified facts from this exact carrier can
// supply an old checkpoint's missing navigation projection.
func (p *Processor) restoreReconnectNativeEvidence(ctx context.Context, l *NativeLease) ([]eventcontract.Event, error) {
	v := p.app.Service().Surface(l.surface.SurfaceID)
	if v == nil || v.AttachedInstanceID != l.native.InstanceID || v.SelectedThreadID == "" {
		return nil, nil
	}
	if inst := p.app.Service().Instance(l.native.InstanceID); inst != nil && inst.Threads[v.SelectedThreadID] != nil {
		return nil, nil
	}
	rows, err := p.journal.db.QueryContext(ctx, `SELECT payload,payload_hash FROM native_events WHERE incarnation=? AND applied=1 AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.threadId')=? ORDER BY sequence DESC`, l.native.CarrierIncarnation, string(agentproto.EventThreadDiscovered), v.SelectedThreadID)
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
		var e agentproto.Event
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		digest, hashErr := agentproto.RuntimeEventPayloadHash(e)
		if hashErr != nil || digest != hash {
			return nil, ErrJournalConflict
		}
		if e.Kind == agentproto.EventThreadDiscovered && e.ThreadID == v.SelectedThreadID && (e.NativeBinding == nil || e.NativeBinding.Owner == l.native) {
			return p.app.Service().RestorePrivateNavigationFromEvent(l.surface.SurfaceID, l.native, e), nil
		}
	}
	return nil, rows.Err()
}
