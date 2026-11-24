package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Each queued input is checked against the exact applied SDK source and all of
// its dispatches; a historical migration hold never becomes a fresh input.
func (p *Processor) reconcileRecoveryGate(ctx context.Context, o Owner, generation int64) ([]eventcontract.Event, error) {
	cp, err := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		return nil, err
	}
	proven := true
	for _, id := range cp.QueuedQueueItemIDs {
		item := cp.QueueItems[id]
		if item == nil || item.SourceKind != state.QueueItemSourceUser || item.ActorUserID != o.UserID || item.SourceMessageID == "" || len(item.SourceMessageIDs) == 0 {
			proven = false
			break
		}
		foundPrimary := false
		seen := map[string]bool{}
		for _, message := range item.SourceMessageIDs {
			if message == "" || seen[message] {
				proven = false
				break
			}
			seen[message] = true
			foundPrimary = foundPrimary || message == item.SourceMessageID
			var n int
			err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM ingress i JOIN private_applied a ON a.seq=i.seq JOIN json_each(i.sources) src WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.lane='input' AND src.value=? AND NOT EXISTS(SELECT 1 FROM dispatch_input di JOIN dispatch d ON d.id=di.dispatch_id WHERE di.seq=i.seq AND d.state<>'failed') AND NOT EXISTS(SELECT 1 FROM private_migration_input_holds h JOIN private_migration_imports m ON m.import_id=h.import_id WHERE m.tenant=i.tenant AND m.app_id=i.app_id AND m.gateway=i.gateway AND m.surface=i.surface AND m.chat=i.chat AND m.user_id=i.user_id AND m.generation=i.generation AND h.message_id=src.value)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, message).Scan(&n)
			if err != nil {
				return nil, err
			}
			if n != 1 {
				proven = false
				break
			}
		}
		if !foundPrimary {
			proven = false
		}
		if !proven {
			break
		}
	}
	return p.app.Service().ReconcilePrivateRecoveryGate(o.SurfaceID, proven)
}
