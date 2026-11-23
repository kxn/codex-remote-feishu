package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func (p *Processor) SetThreadPolicy(ctx context.Context, o Owner, generation int64, policy orchestrator.PrivateThreadPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validOwner(o) || generation < 1 {
		return ErrJournalState
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	p.app.Service().SetPrivateThreadPolicy(o.SurfaceID, policy)
	return nil
}
