package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// HostLifecycleReducer returns display-only notices under the processor lock.
type HostLifecycleReducer func(*App, Owner) []eventcontract.Event

// ApplyHostLifecycle operates on already loaded owners; callers which have a
// verified durable owner may use ApplyOwnerHostLifecycle for that owner.
func (p *Processor) ApplyHostLifecycle(ctx context.Context, id string, at time.Time, reduce HostLifecycleReducer) ([]PendingEffectWork, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if id == "" || at.IsZero() || reduce == nil {
		return nil, ErrJournalState
	}
	owners := make([]Owner, 0, len(p.owners))
	for o := range p.owners {
		owners = append(owners, o)
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i].SurfaceID < owners[j].SurfaceID })
	var result []PendingEffectWork
	for _, o := range owners {
		effects, err := p.applyOwnerHostLifecycleLocked(ctx, o, p.owners[o].generation, id, at, reduce)
		result = append(result, effects...)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

// ApplyOwnerHostLifecycle commits a lifecycle notice for a durable, externally
// verified owner and exact epoch. It does not validate a platform binding itself.
func (p *Processor) ApplyOwnerHostLifecycle(ctx context.Context, o Owner, generation int64, id string, at time.Time, reduce HostLifecycleReducer) ([]PendingEffectWork, error) {
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 || id == "" || at.IsZero() || reduce == nil {
		return nil, ErrJournalState
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return nil, err
	}
	return p.applyOwnerHostLifecycleLocked(ctx, o, generation, id, at, reduce)
}

func (p *Processor) applyOwnerHostLifecycleLocked(ctx context.Context, o Owner, generation int64, id string, at time.Time, reduce HostLifecycleReducer) ([]PendingEffectWork, error) {
	state, ok := p.owners[o]
	if !ok || state.generation != generation {
		return nil, ErrJournalConflict
	}
	ref := tickRef{ID: "host-lifecycle:" + id, At: at.UTC().Format(time.RFC3339Nano)}
	var revision int64
	var observed string
	err := p.journal.db.QueryRowContext(ctx, `SELECT revision,observed_at FROM private_tick_applied WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND tick_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, ref.ID).Scan(&revision, &observed)
	if err == nil && observed != ref.At {
		return nil, ErrJournalConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		events := reduce(p.app, o)
		for _, event := range events {
			if event.Kind != eventcontract.KindNotice || event.SurfaceSessionID != o.SurfaceID {
				p.poisoned = true
				return nil, ErrJournalConflict
			}
		}
		effects, e := p.freezeEffects(o, generation, ref.ID, events)
		if e != nil {
			p.poisoned = true
			return nil, e
		}
		core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			p.poisoned = true
			return nil, e
		}
		cp := Checkpoint{Owner: o, Generation: generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
		if e = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, &ref); e != nil {
			p.poisoned = true
			return nil, e
		}
		p.owners[o] = processorOwner{cp.Revision, cp.Cursor, cp.Generation}
		revision = cp.Revision
	} else if err != nil {
		return nil, err
	}
	effects, err := p.journal.processorEffects(ctx, o, generation, revision)
	if err != nil {
		return nil, err
	}
	result := make([]PendingEffectWork, 0, len(effects))
	for _, effect := range effects {
		result = append(result, PendingEffectWork{Owner: o, Generation: generation, Effect: effect})
	}
	return result, nil
}
