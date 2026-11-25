package privatechat

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// ProfileCatalog is an immutable projection of the authoritative host config.
// It is deliberately not stored as an owner checkpoint or an authority.
type ProfileCatalog struct {
	Codex                            []state.CodexProfileSummary
	Claude                           []state.ClaudeProfileRecord
	OpenCode                         []state.OpenCodeProfileSummary
	HasCodex, HasClaude, HasOpenCode bool
}

func (p *Processor) ApplyProfileCatalog(ctx context.Context, catalog ProfileCatalog, current func() bool) error {
	unlockReference := p.journal.LockReferenceMutation()
	defer unlockReference()
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if current == nil || !current() {
		return ErrJournalConflict
	}
	if catalog.HasCodex {
		p.app.Service().MaterializeCodexProfiles(catalog.Codex)
	}
	if catalog.HasClaude {
		p.app.Service().MaterializeClaudeProfiles(catalog.Claude)
	}
	if catalog.HasOpenCode {
		p.app.Service().MaterializeOpenCodeProfiles(catalog.OpenCode)
	}
	if !current() {
		return ErrJournalConflict
	}
	return nil
}
