package privatechat

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RetiredRuntimeAuthority verifies the exact stopped carrier before a local
// checkpoint transaction schedules replacement; it performs no network I/O.
type RetiredRuntimeAuthority interface {
	WithRetiredRuntime(context.Context, Owner, int64, agentproto.NativeOwner, func() error) error
}

func (p *Processor) PrepareRetiredRuntimeReplacement(ctx context.Context, o Owner, generation int64, old agentproto.NativeOwner, contract orchestrator.PrivateRetiredContract, cwd string, authority RetiredRuntimeAuthority) ([]EffectRecord, error) {
	if authority == nil || !validOwner(o) || generation < 1 || old.Validate() != nil || cwd == "" {
		return nil, ErrJournalConflict
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	var out []EffectRecord
	err := authority.WithRetiredRuntime(ctx, o, generation, old, func() error {
		owner, e := p.ensureOwner(ctx, o, generation)
		if e != nil {
			return e
		}
		surface := p.app.Service().Surface(o.SurfaceID)
		if surface == nil {
			return ErrJournalConflict
		}
		if surface.PendingHeadless != nil {
			return nil
		}
		checkpoint, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			return e
		}
		if checkpoint.Navigation == nil && surface.RouteMode == state.RouteModePinned && surface.SelectedThreadID != "" {
			event, e := p.appliedRetiredThreadFact(ctx, old, surface.SelectedThreadID)
			if e != nil {
				return e
			}
			if event == nil {
				return ErrDeferred
			}
			if e = p.app.Service().RememberRetiredPrivateNavigation(o.SurfaceID, old, contract.Profiles, *event); e != nil {
				return e
			}
		}
		events, e := p.app.Service().MaterializeRetiredPrivateReplacement(o.SurfaceID, old, contract, cwd)
		if e != nil {
			return e
		}
		key := fmt.Sprintf("runtime-replacement:%s:%d", old.CarrierIncarnation, owner.revision+1)
		effects, e := p.freezeEffects(o, generation, key, events)
		if e != nil {
			p.poisoned = true
			return e
		}
		core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			p.poisoned = true
			return e
		}
		cp := Checkpoint{Owner: o, Generation: generation, Revision: owner.revision + 1, Cursor: owner.cursor, Core: core}
		if e = p.journal.commitCheckpoint(ctx, cp, owner.revision, effects, 0, nil); e != nil {
			p.poisoned = true
			return e
		}
		p.owners[o] = processorOwner{cp.Revision, cp.Cursor, generation}
		out, e = p.journal.processorEffects(ctx, o, generation, cp.Revision)
		return e
	})
	return out, err
}

// RestoreRetiredRuntimeReplacement rebuilds only the checkpoint-committed
// pending launch after restart, after the same retired authority is rechecked.
func (p *Processor) RestoreRetiredRuntimeReplacement(ctx context.Context, o Owner, generation int64, effectID string, old agentproto.NativeOwner, profiles orchestrator.PrivateNativeProfiles, cwd string, authority RetiredRuntimeAuthority) error {
	if authority == nil || effectID == "" {
		return ErrJournalConflict
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	return authority.WithRetiredRuntime(ctx, o, generation, old, func() error {
		if _, err := p.ensureOwner(ctx, o, generation); err != nil {
			return err
		}
		effect, err := p.journal.LoadEffect(ctx, o, generation, effectID)
		if err != nil {
			return err
		}
		event, _, err := DecodeStoredEffect(effect.Effect.Body)
		if err != nil {
			return err
		}
		cmd := event.DaemonCommand
		if cmd == nil || !cmd.AutoRestore || cmd.Kind != control.DaemonCommandStartHeadless || cmd.Backend != old.Backend {
			return ErrJournalConflict
		}
		profile := ""
		switch old.Backend {
		case agentproto.BackendCodex:
			profile = profiles.Codex
		case agentproto.BackendClaude:
			profile = profiles.Claude
		case agentproto.BackendOpenCode:
			profile = profiles.OpenCode
		}
		return p.app.Service().RestorePrivateUpgradeHeadlessProjection(orchestrator.PrivateUpgradeResume{SurfaceID: o.SurfaceID, OldInstanceID: old.InstanceID, ThreadID: cmd.ThreadID, CWD: cmd.ThreadCWD, ProfileID: profile, HistoryNamespace: old.HistoryNamespace, Backend: old.Backend}, cmd.InstanceID)
	})
}
