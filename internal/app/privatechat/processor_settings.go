package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RefreshBotSettings applies an authoritative owner snapshot under the single
// processor lock. stillCurrent must only read an atomic invalidation revision;
// it must not acquire the config owner lock or perform I/O.
func (p *Processor) RefreshBotSettings(ctx context.Context, o Owner, generation int64, record state.BotCapabilitySettingsRecord, stillCurrent func() bool) error {
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
	if !validOwner(o) || generation < 1 || record.GatewayID != o.GatewayID || stillCurrent == nil {
		return ErrJournalState
	}
	if _, err := p.ensureOwner(ctx, o, generation); err != nil {
		return err
	}
	if !stillCurrent() {
		return ErrJournalConflict
	}
	var pending int
	err := p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND json_extract(e.body,'$.event.DaemonCommand.Kind')=? AND (e.state<>'confirmed' OR a.effect_id IS NULL)`, o.TenantID, o.AppID, o.GatewayID, "private.bot.settings").Scan(&pending)
	if err != nil {
		return err
	}
	if pending != 0 {
		return nil
	}
	return p.app.Service().ApplyPrivateBotSettingsToSurface(record, o.SurfaceID)
}
