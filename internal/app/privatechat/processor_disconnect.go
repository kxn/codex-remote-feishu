package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type disconnectRef struct{ Incarnation, ConnectionID string }

// ApplyNativeDisconnect persists authenticated connectivity loss, not a stop
// fact. connectionID must include the host lifecycle and connection identity;
// the caller holds the current-connection guard through this operation.
func (p *Processor) ApplyNativeDisconnect(ctx context.Context, l *NativeLease, connectionID string) (out []EffectRecord, err error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if l == nil || l.journal != p.journal || connectionID == "" {
		return nil, ErrJournalState
	}
	ref := disconnectRef{Incarnation: l.native.CarrierIncarnation, ConnectionID: connectionID}
	err = l.with(ctx, func() error {
		var revision int64
		e := p.journal.db.QueryRowContext(ctx, `SELECT revision FROM private_disconnect_applied WHERE incarnation=? AND connection_id=?`, ref.Incarnation, ref.ConnectionID).Scan(&revision)
		if e == nil {
			out, e = p.journal.processorEffects(ctx, l.surface, l.generation, revision)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		state, e := p.ensureOwner(ctx, l.surface, l.generation)
		if e != nil {
			return e
		}
		if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID {
			return ErrJournalConflict
		}
		events, e := p.app.Service().ApplyPrivateTransportDisconnected(l.surface.SurfaceID, l.native.InstanceID)
		if e != nil {
			return e
		}
		effects, e := p.freezeEffects(l.surface, l.generation, "disconnect:"+ref.Incarnation+":"+ref.ConnectionID, events)
		if e != nil {
			p.poisoned = true
			return e
		}
		core, e := p.app.Service().ExportPrivateCheckpoint(l.surface.SurfaceID)
		if e != nil {
			p.poisoned = true
			return e
		}
		cp := Checkpoint{Owner: l.surface, Generation: l.generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
		if e = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, &ref, nil); e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: disconnect: %v", ErrProcessorPoisoned, e)
		}
		p.owners[l.surface] = processorOwner{cp.Revision, cp.Cursor, l.generation}
		out, e = p.journal.processorEffects(ctx, l.surface, l.generation, cp.Revision)
		return e
	})
	return out, err
}
