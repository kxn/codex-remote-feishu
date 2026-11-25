package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type tickRef struct{ ID, At string }

// ApplyPrivateTick advances only the surface owned by the authenticated lease.
// Tick identity and observation time are persisted with its checkpoint and effects.
// The caller must hold its execution-owner lease while invoking this method.
func (p *Processor) ApplyPrivateTick(ctx context.Context, l *NativeLease, tickID string, now time.Time) (out []EffectRecord, err error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if l == nil || l.journal != p.journal || tickID == "" || now.IsZero() {
		return nil, ErrJournalState
	}
	ref := tickRef{ID: tickID, At: now.UTC().Format(time.RFC3339Nano)}
	err = l.with(ctx, func() error {
		o := l.surface
		var revision int64
		var observed string
		e := p.journal.db.QueryRowContext(ctx, `SELECT revision,observed_at FROM private_tick_applied WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND tick_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, l.generation, tickID).Scan(&revision, &observed)
		if e == nil {
			if observed != ref.At {
				return ErrJournalConflict
			}
			out, e = p.journal.processorEffects(ctx, o, l.generation, revision)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		state, e := p.ensureOwner(ctx, o, l.generation)
		if e != nil {
			return e
		}
		if p.app.Service().PrivateDetachedInstance(o.SurfaceID, l.native.InstanceID) {
			return ErrDeferred
		}
		if p.app.instances[l.native.InstanceID] != o.SurfaceID {
			return ErrJournalConflict
		}
		// A surface may own several authenticated carriers. Progress and
		// native commands from its tick belong to the attached one only.
		attached := p.app.Service().AttachedInstanceID(o.SurfaceID)
		if attached != "" && attached != l.native.InstanceID {
			return ErrJournalConflict
		}
		events := p.app.Service().TickPrivateSurface(o.SurfaceID, now)
		retry, e := p.requestInventoryRetryDue(ctx, l, now)
		if e != nil {
			return e
		}
		if retry {
			events = append(events, requestInventoryEvent(l))
			// The next attempt is eligible only if the returned inventory is
			// explicitly incomplete. An in-flight inspection is never duplicated.
			delete(p.requestInventoryRetry, l.native.CarrierIncarnation)
		}
		effects, e := p.freezeEffects(o, l.generation, "tick:"+tickID, events)
		if e != nil {
			p.poisoned = true
			return e
		}
		if e = bindNativeDisplayChains(o, l.native.CarrierIncarnation, events, effects); e != nil {
			p.poisoned = true
			return e
		}
		core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			p.poisoned = true
			return e
		}
		cp := Checkpoint{Owner: o, Generation: l.generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
		if e = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, &ref); e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: tick: %v", ErrProcessorPoisoned, e)
		}
		p.owners[o] = processorOwner{cp.Revision, cp.Cursor, l.generation}
		out, e = p.journal.processorEffects(ctx, o, l.generation, cp.Revision)
		return e
	})
	return out, err
}

// PrivateTickDue is a conservative scheduling hint, not an applied tick. The
// host must recheck on each interval; no event or durable identity is consumed.
func (p *Processor) PrivateTickDue(ctx context.Context, l *NativeLease, now time.Time) (due bool, err error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return false, ErrProcessorPoisoned
	}
	if l == nil || l.journal != p.journal || now.IsZero() {
		return false, ErrJournalState
	}
	err = l.with(ctx, func() error {
		if _, e := p.ensureOwner(ctx, l.surface, l.generation); e != nil {
			return e
		}
		if p.app.Service().PrivateDetachedInstance(l.surface.SurfaceID, l.native.InstanceID) {
			return nil
		}
		if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID {
			return ErrJournalConflict
		}
		if attached := p.app.Service().AttachedInstanceID(l.surface.SurfaceID); attached != "" && attached != l.native.InstanceID {
			return ErrJournalConflict
		}
		due = p.app.Service().PrivateTickDue(l.surface.SurfaceID, now)
		if !due {
			var retryErr error
			due, retryErr = p.requestInventoryRetryDue(ctx, l, now)
			if retryErr != nil {
				return retryErr
			}
		}
		return nil
	})
	return due, err
}
