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

type HostEffectPosition struct {
	Revision int64
	Ordinal  int
	ID       string
}

func (w PendingEffectWork) Position() HostEffectPosition {
	return HostEffectPosition{Revision: w.Effect.Revision, Ordinal: w.Effect.Ordinal, ID: w.Effect.Effect.ID}
}

func (p HostEffectPosition) Before(other HostEffectPosition) bool {
	if p.Revision != other.Revision {
		return p.Revision < other.Revision
	}
	if p.Ordinal != other.Ordinal {
		return p.Ordinal < other.Ordinal
	}
	return p.ID < other.ID
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
	return j.pendingHostEffects(ctx, limit, offset, HostEffectPosition{}, "")
}

// PendingHostEffectsAfter keeps the scan position stable as completed effects
// drop out of the pending set.
func (j *Journal) PendingHostEffectsAfter(ctx context.Context, limit int, after HostEffectPosition) ([]PendingEffectWork, error) {
	return j.pendingHostEffects(ctx, limit, 0, after, "")
}

// RunnableHostEffectsAfter omits publications waiting for durable delivery
// evidence and undispatched commands without a dispatchable target. The audit
// inventory still includes these effects and their receipts.
func (j *Journal) RunnableHostEffectsAfter(ctx context.Context, limit int, after HostEffectPosition) ([]PendingEffectWork, error) {
	return j.pendingHostEffects(ctx, limit, 0, after, runnablePublicationSQL+runnableNativeSQL)
}

const runnablePublicationSQL = `
AND (e.lane<>'interactive' OR (
	NOT ` + publicationOrderHeldSQL + `
	AND (
		(e.state='prepared' AND NOT COALESCE((
			SELECT p.state<>'confirmed' OR a.revision IS NULL
			FROM private_display_chain current
			` + displayPredecessorJoinSQL + `
			WHERE current.effect_id=e.id AND ` + displayPredecessorSQL + `),0))
		OR EXISTS(SELECT 1 FROM private_publications pub WHERE pub.effect_id=e.id
			AND (e.state IN ('prepared','confirmed') OR NOT EXISTS(
				SELECT 1 FROM private_publication_ops op
				WHERE op.effect_id=e.id AND op.state IN ('sending','unknown'))))
	)
))`

const runnableNativeSQL = `
AND NOT (e.lane='background' AND e.state<>'confirmed'
	AND NOT EXISTS(SELECT 1 FROM dispatch d WHERE d.id=e.id||':native')
	AND CASE WHEN json_valid(e.body) THEN (
		coalesce(json_extract(e.body,'$.version'),0)=1
		AND coalesce(json_type(e.body,'$.event.Command'),'')='object'
		AND json_type(e.body,'$.event.Payload') IS NULL
		AND NOT EXISTS(SELECT 1 FROM json_each(e.body) field
			WHERE field.key NOT IN ('version','event','command_inputs','target_instance_id'))
		AND (coalesce(json_type(e.body,'$.target_instance_id'),'null')='null'
			OR EXISTS(` + detachedCarrierRowsSQL + `json_extract(e.body,'$.target_instance_id')))
	) ELSE 0 END)`

func (j *Journal) pendingHostEffects(ctx context.Context, limit, offset int, after HostEffectPosition, candidateFilter string) ([]PendingEffectWork, error) {
	if j == nil || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	if offset < 0 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT e.tenant,e.app_id,e.gateway,e.surface,e.chat,e.user_id,e.generation,e.id,e.revision,e.ordinal,e.lane,e.body,e.state,e.receipt
FROM private_effect e
WHERE e.state IN ('prepared','sending','unknown','confirmed')
AND NOT EXISTS(SELECT 1 FROM private_effect_applied a WHERE a.effect_id=e.id)
AND NOT (e.state<>'confirmed' AND EXISTS(SELECT 1 FROM dispatch d WHERE d.id=e.id||':native' AND d.state IN ('accepted','failed','resolved')))
AND (?='' OR (e.revision,e.ordinal,e.id) > (?,?,?))
`+candidateFilter+` ORDER BY e.revision,e.ordinal,e.id LIMIT ? OFFSET ?`, after.ID, after.Revision, after.Ordinal, after.ID, limit, offset)
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
