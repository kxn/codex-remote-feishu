package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
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
	projectionCtx, projectionErr := beginProjection(ctx)
	if projectionErr != nil {
		return projectionErr
	}
	ctx = projectionCtx
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	p.app.Service().SetPrivateThreadPolicy(o.SurfaceID, policy)
	return nil
}

// UpdateThreadCarrier installs only a verified carrier's namespace, without
// reading or authorizing unrelated historical sessions.
func (p *Processor) UpdateThreadCarrier(ctx context.Context, o Owner, generation int64, instance, namespace, profile string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validOwner(o) || generation < 1 || instance == "" || namespace == "" {
		return ErrJournalState
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	projectionCtx, projectionErr := beginProjection(ctx)
	if projectionErr != nil {
		return projectionErr
	}
	ctx = projectionCtx
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	p.app.Service().UpdatePrivateThreadCarrier(o.SurfaceID, instance, namespace, profile)
	return nil
}

func (p *Processor) RecordRetiredThread(ctx context.Context, o Owner, generation int64, backend agentproto.Backend, instance, namespace, session string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validOwner(o) || generation < 1 || instance == "" || namespace == "" {
		return ErrJournalState
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	projectionCtx, projectionErr := beginProjection(ctx)
	if projectionErr != nil {
		return projectionErr
	}
	ctx = projectionCtx
	if p.poisoned {
		return ErrProcessorPoisoned
	}
	p.app.Service().RecordPrivateRetiredSession(o.SurfaceID, backend, instance, namespace, session)
	return nil
}

// IsHistoryDraftNavigation uses the already-frozen picker candidates and never
// consults a history database; the eventual confirmation is revalidated.
func (p *Processor) IsHistoryDraftNavigation(ctx context.Context, o Owner, generation int64, action control.Action) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return false, ErrProcessorPoisoned
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return false, err
	}
	return p.app.Service().IsTargetPickerDraftNavigation(action), nil
}
