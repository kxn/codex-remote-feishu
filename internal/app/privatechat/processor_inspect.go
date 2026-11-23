package privatechat

import "context"

// InspectEffect serializes planning with all core mutations. The observer is a
// trusted local read-only callback: it must not retain app, mutate state, make
// network calls or perform external writes. Short local reads are permitted.
func (p *Processor) InspectEffect(ctx context.Context, o Owner, generation int64, id string, observer func(*App, EffectRecord) error) error {
	return p.inspectEffect(ctx, o, generation, id, false, false, observer)
}

// InspectLocalObservation allows only a read-only, repeatable observation to
// be re-planned after an interrupted process; no external write is authorized.
func (p *Processor) InspectLocalObservation(ctx context.Context, o Owner, generation int64, id string, observer func(*App, EffectRecord) error) error {
	return p.inspectEffect(ctx, o, generation, id, true, false, observer)
}

// InspectPendingHostIntent validates an already persisted external-write intent.
// The callback may register that exact idempotent intent in another local store;
// it must not launch processes or perform network I/O. Unknown intents retain
// their original identity and may only reconcile the same destination receipt.
func (p *Processor) InspectPendingHostIntent(ctx context.Context, o Owner, generation int64, id string, observer func(*App, EffectRecord) error) error {
	return p.inspectEffect(ctx, o, generation, id, true, true, observer)
}

func (p *Processor) inspectEffect(ctx context.Context, o Owner, generation int64, id string, allowUnknown, allowSending bool, observer func(*App, EffectRecord) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 || id == "" || observer == nil {
		return ErrJournalState
	}
	var e EffectRecord
	e.Effect.ID = id
	var receipt []byte
	err := p.journal.db.QueryRowContext(ctx, `SELECT revision,ordinal,lane,body,state,receipt FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&e.Revision, &e.Ordinal, &e.Effect.Lane, &e.Effect.Body, &e.State, &receipt)
	if err != nil {
		return err
	}
	e.Receipt = receipt
	if e.State != "prepared" && !(allowUnknown && e.State == "unknown") && !(allowSending && e.State == "sending") {
		return ErrJournalState
	}
	if _, err = p.ensureOwner(ctx, o, generation); err != nil {
		return err
	}
	return observer(p.app, e)
}
