package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// liveEffectRef is distinct from the critical native journal sequence.
// Sequence values can have gaps because wrappers reserve durable ranges.
type liveEffectRef struct {
	Incarnation string
	Sequence    int64
	Hash        string
}

// ApplyLiveNative projects only an authenticated, sequenced display delta.
// It cannot establish execution, input acceptance, completion or stop facts.
// Its independent applied marker, checkpoint and effects commit atomically.
func (p *Processor) ApplyLiveNative(ctx context.Context, l *NativeLease, event agentproto.Event, reducer NativeReducer) (out []EffectRecord, err error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if l == nil || l.journal != p.journal || reducer == nil || event.Live == nil || event.Live.Version != agentproto.RuntimeLiveProtocolVersion || event.Live.CarrierIncarnation != l.native.CarrierIncarnation || event.Live.Sequence == 0 || event.Live.Sequence > math.MaxInt64 || event.Journal != nil || event.Seq != 0 || agentproto.PrivateDeliveryForEvent(event.Kind) != agentproto.PrivateEventLive || event.NativeBinding == nil || event.NativeBinding.Owner != l.native || event.ThreadID == "" || event.TurnID == "" || event.ItemID == "" || strings.TrimSpace(event.Delta) == "" {
		return nil, ErrJournalState
	}
	hash, e := agentproto.HashRuntimeLiveEvent(event)
	if e != nil || hash != event.Live.PayloadHash {
		return nil, ErrJournalConflict
	}
	ref := liveEffectRef{Incarnation: event.Live.CarrierIncarnation, Sequence: int64(event.Live.Sequence), Hash: hash}
	err = l.with(ctx, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}

		var oldHash string
		var revision int64
		e := p.journal.db.QueryRowContext(ctx, `SELECT payload_hash,revision FROM private_live_applied WHERE incarnation=? AND sequence=?`, ref.Incarnation, ref.Sequence).Scan(&oldHash, &revision)
		if e == nil {
			if oldHash != ref.Hash {
				return ErrJournalConflict
			}
			out, e = p.journal.processorEffects(ctx, l.surface, l.generation, revision)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var latest sql.NullInt64
		if e = p.journal.db.QueryRowContext(ctx, `SELECT max(sequence) FROM private_live_applied WHERE incarnation=?`, ref.Incarnation).Scan(&latest); e != nil {
			return e
		}
		if latest.Valid && ref.Sequence <= latest.Int64 {
			return ErrJournalConflict
		}
		state, e := p.ensureOwner(ctx, l.surface, l.generation)
		if e != nil {
			return e
		}
		if p.app.Service().PrivateDetachedInstance(l.surface.SurfaceID, l.native.InstanceID) {
			return ErrDeferred
		}
		inst := p.app.Service().Instance(l.native.InstanceID)
		if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID || inst == nil || !inst.Online {
			return ErrJournalState
		}
		command, e := p.journal.nativeProjectionCommand(ctx, l, event)
		if e != nil {
			return e
		}
		// The authenticated hello and accepted command authorize restoration of
		// an existing held binding; the live frame itself proves no new turn.
		if e = p.app.Service().RestorePrivateNativeProjection(l.surface.SurfaceID, l.native.InstanceID, command, event.ThreadID, event.TurnID); e != nil {
			return e
		}
		if !p.app.Service().PrivateLiveTurnMatches(l.surface.SurfaceID, l.native.InstanceID, event.ThreadID, event.TurnID, command) {
			return ErrJournalState
		}
		events, e := reducer.ReduceNative(p.app, l.native.InstanceID, event)
		if e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: live reducer: %v", ErrProcessorPoisoned, e)
		}
		for _, result := range events {
			if result.Command != nil || result.DaemonCommand != nil {
				p.poisoned = true
				return fmt.Errorf("%w: live event produced command", ErrProcessorPoisoned)
			}
		}
		effects, e := p.freezeEffects(l.surface, l.generation, fmt.Sprintf("live:%s:%d", ref.Incarnation, ref.Sequence), events)
		if e != nil {
			p.poisoned = true
			return e
		}
		if e = bindNativeDisplayChains(l.surface, ref.Incarnation, events, effects); e != nil {
			p.poisoned = true
			return e
		}
		bindNativePublicationOrder(l.surface, l.generation, ref.Incarnation, event.ThreadID, event.TurnID, events, effects)
		core, e := p.app.Service().ExportPrivateCheckpoint(l.surface.SurfaceID)
		if e != nil {
			p.poisoned = true
			return e
		}
		cp := Checkpoint{Owner: l.surface, Generation: l.generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
		if e = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, &ref, nil, nil, nil); e != nil {
			p.poisoned = true
			return fmt.Errorf("%w: live commit: %v", ErrProcessorPoisoned, e)
		}
		p.owners[l.surface] = processorOwner{cp.Revision, cp.Cursor, l.generation}
		out, e = p.journal.processorEffects(ctx, l.surface, l.generation, cp.Revision)
		return e
	})
	return out, err
}
