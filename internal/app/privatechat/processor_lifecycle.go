package privatechat

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// HostLifecycleReducer may inspect one loaded owner under the processor lock.
// It returns display-only notices; it cannot start work or perform host I/O.
type HostLifecycleReducer func(*App, Owner) []eventcontract.Event

// ApplyHostLifecycle persists lifecycle notices through the same publication
// path as ordinary private display. Unloaded conversations remain untouched.
func (p *Processor) ApplyHostLifecycle(ctx context.Context, id string, at time.Time, reduce HostLifecycleReducer) ([]PendingEffectWork, error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
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
		state := p.owners[o]
		ref := tickRef{ID: "host-lifecycle:" + id, At: at.UTC().Format(time.RFC3339Nano)}
		var revision int64
		var observed string
		err := p.journal.db.QueryRowContext(ctx, `SELECT revision,observed_at FROM private_tick_applied WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND tick_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, state.generation, ref.ID).Scan(&revision, &observed)
		if err == nil && observed != ref.At {
			return result, ErrJournalConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			events := reduce(p.app, o)
			for _, event := range events {
				if event.Kind != eventcontract.KindNotice || event.SurfaceSessionID != o.SurfaceID {
					p.poisoned = true
					return result, ErrJournalConflict
				}
			}
			effects, e := p.freezeEffects(o, state.generation, ref.ID, events)
			if e != nil {
				p.poisoned = true
				return result, e
			}
			core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if e != nil {
				p.poisoned = true
				return result, e
			}
			cp := Checkpoint{Owner: o, Generation: state.generation, Revision: state.revision + 1, Cursor: state.cursor, Core: core}
			if e = p.journal.commitCheckpointExtended(ctx, cp, state.revision, effects, 0, nil, nil, nil, nil, nil, nil, &ref); e != nil {
				p.poisoned = true
				return result, e
			}
			p.owners[o] = processorOwner{cp.Revision, cp.Cursor, cp.Generation}
			revision = cp.Revision
		} else if err != nil {
			return result, err
		}
		effects, err := p.journal.processorEffects(ctx, o, state.generation, revision)
		if err != nil {
			return result, err
		}
		for _, effect := range effects {
			result = append(result, PendingEffectWork{Owner: o, Generation: state.generation, Effect: effect})
		}
	}
	return result, nil
}
