package privatechat

import "context"

type PendingIngressWork struct {
	Owner                Owner
	Generation, Sequence int64
	Lane                 string
}
type PendingEffectWork struct {
	Owner      Owner
	Generation int64
	Effect     EffectRecord
}

// PendingIngressWork returns unapplied durable inputs; control and inspect lanes
// may pass a held input, while each owner's input lane remains FIFO.
func (j *Journal) PendingIngressWork(ctx context.Context, limit int) ([]PendingIngressWork, error) {
	return j.PendingIngressWorkPage(ctx, limit, 0)
}

func (j *Journal) PendingIngressWorkPage(ctx context.Context, limit, offset int) ([]PendingIngressWork, error) {
	if j == nil || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	if offset < 0 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT i.tenant,i.app_id,i.gateway,i.surface,i.chat,i.user_id,i.generation,i.seq,i.lane FROM ingress i WHERE NOT EXISTS(SELECT 1 FROM private_applied a WHERE a.seq=i.seq) ORDER BY CASE WHEN i.lane='input' THEN 1 ELSE 0 END,i.seq LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingIngressWork
	for rows.Next() {
		var w PendingIngressWork
		if err = rows.Scan(&w.Owner.TenantID, &w.Owner.AppID, &w.Owner.GatewayID, &w.Owner.SurfaceID, &w.Owner.ChatID, &w.Owner.UserID, &w.Generation, &w.Sequence, &w.Lane); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// PendingHostEffects includes unknown/confirmed effects so their own trusted
// executors can reconcile or complete them without blind retransmission.
func (j *Journal) PendingHostEffects(ctx context.Context, limit int) ([]PendingEffectWork, error) {
	return j.PendingHostEffectsPage(ctx, limit, 0)
}

// PendingHostEffectsPage allows the host to keep scanning past unresolved
// effects, so an old unknown result cannot starve a later control action.
func (j *Journal) PendingHostEffectsPage(ctx context.Context, limit, offset int) ([]PendingEffectWork, error) {
	if j == nil || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	if offset < 0 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT e.tenant,e.app_id,e.gateway,e.surface,e.chat,e.user_id,e.generation,e.id,e.revision,e.ordinal,e.lane,e.body,e.state,e.receipt FROM private_effect e WHERE e.state IN ('prepared','sending','unknown','confirmed') AND NOT EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=e.id) ORDER BY e.revision,e.ordinal,e.id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingEffectWork
	for rows.Next() {
		var w PendingEffectWork
		var receipt []byte
		if err = rows.Scan(&w.Owner.TenantID, &w.Owner.AppID, &w.Owner.GatewayID, &w.Owner.SurfaceID, &w.Owner.ChatID, &w.Owner.UserID, &w.Generation, &w.Effect.Effect.ID, &w.Effect.Revision, &w.Effect.Ordinal, &w.Effect.Effect.Lane, &w.Effect.Effect.Body, &w.Effect.State, &receipt); err != nil {
			return nil, err
		}
		w.Effect.Receipt = receipt
		out = append(out, w)
	}
	return out, rows.Err()
}
