package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Each queued input is checked against the exact applied SDK source and all of
// its dispatches; a historical migration hold never becomes a fresh input.
func (p *Processor) reconcileRecoveryGate(ctx context.Context, o Owner, generation int64) ([]eventcontract.Event, error) {
	proven, err := p.queuedRecoveryProvenLocked(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	return p.app.Service().ReconcilePrivateRecoveryGate(o.SurfaceID, proven)
}

// QueuedRecoveryProven checks frozen queue sources without dispatching them.
func (p *Processor) QueuedRecoveryProven(ctx context.Context, o Owner, generation int64) (bool, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return false, ErrProcessorPoisoned
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return false, err
	}
	return p.queuedRecoveryProvenLocked(ctx, o, generation)
}

func (p *Processor) queuedRecoveryProvenLocked(ctx context.Context, o Owner, generation int64) (bool, error) {
	cp, err := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		return false, err
	}
	proven := true
	for _, id := range cp.QueuedQueueItemIDs {
		item := cp.QueueItems[id]
		if item != nil && item.SourceKind == state.QueueItemSourceControlAction {
			if item.ActorUserID != o.UserID {
				proven = false
				break
			}
			seq, e := verifyControlInput(ctx, p.journal.db, o, generation, item.ControlIngressEventID, 0, true)
			if e != nil {
				return false, e
			}
			var n int
			if e = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM dispatch_input di JOIN dispatch d ON d.id=di.dispatch_id WHERE di.seq=? AND d.state<>'failed'`, seq).Scan(&n); e != nil {
				return false, e
			}
			if n != 0 {
				proven = false
				break
			}
			continue
		}
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
			err = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM ingress i JOIN private_applied a ON a.seq=i.seq AND a.canceled_by IS NULL JOIN json_each(i.sources) src WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.lane='input' AND src.value=? AND NOT EXISTS(SELECT 1 FROM dispatch_input di JOIN dispatch d ON d.id=di.dispatch_id WHERE di.seq=i.seq AND d.state<>'failed') AND NOT EXISTS(SELECT 1 FROM private_migration_input_holds h JOIN private_migration_imports m ON m.import_id=h.import_id WHERE m.tenant=i.tenant AND m.app_id=i.app_id AND m.gateway=i.gateway AND m.surface=i.surface AND m.chat=i.chat AND m.user_id=i.user_id AND m.generation=i.generation AND h.message_id=src.value)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, message).Scan(&n)
			if err != nil {
				return false, err
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
	return proven, nil
}
