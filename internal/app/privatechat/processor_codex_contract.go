package privatechat

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// ApplyCodexContracts persists one owner's versioned projection and all
// generated effects atomically. stillCurrent may only read an atomic snapshot.
func (p *Processor) ApplyCodexContracts(ctx context.Context, o Owner, generation int64, version string, profiles map[string]orchestrator.PrivateCodexContract, stillCurrent func() bool, guard ExecutionGuard) ([]EffectRecord, error) {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	if !validOwner(o) || generation < 1 {
		return nil, ErrJournalState
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return nil, err
	}
	if stillCurrent != nil && stillCurrent() && p.app.Service().PrivateCodexContractsCurrent(o.SurfaceID, version) {
		return nil, nil
	}
	var out []EffectRecord
	err := p.withSurfaceExecutionGuard(o.SurfaceID, guard, func() error {
		ctx, projectionErr := beginProjection(ctx)
		if projectionErr != nil {
			return projectionErr
		}
		var e error
		out, e = p.applyCodexContractsLocked(ctx, o, generation, version, profiles, stillCurrent)
		return e
	})
	return out, err
}

func (p *Processor) applyCodexContractsLocked(ctx context.Context, o Owner, generation int64, version string, profiles map[string]orchestrator.PrivateCodexContract, stillCurrent func() bool) ([]EffectRecord, error) {
	if !validOwner(o) || generation < 1 || version == "" || stillCurrent == nil {
		return nil, ErrJournalState
	}
	if !stillCurrent() {
		return nil, ErrJournalConflict
	}
	state, e := p.ensureOwner(ctx, o, generation)
	if e != nil {
		return nil, e
	}
	if !stillCurrent() {
		return nil, ErrJournalConflict
	}
	events, changed, e := p.app.Service().ApplyPrivateCodexContracts(o.SurfaceID, version, profiles)
	if e != nil {
		p.poisoned = true
		return nil, e
	}
	if !changed {
		return nil, nil
	}
	effects, e := p.freezeEffects(o, generation, fmt.Sprintf("codex-contract:%s:%d", version, state.revision+1), events)
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
	if !stillCurrent() {
		p.poisoned = true
		return nil, ErrJournalConflict
	}
	if e = p.journal.commitCheckpoint(ctx, cp, state.revision, effects, 0, nil); e != nil {
		p.poisoned = true
		return nil, fmt.Errorf("%w: codex contract: %v", ErrProcessorPoisoned, e)
	}
	p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
	return p.journal.processorEffects(ctx, o, generation, cp.Revision)
}
