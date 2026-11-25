package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// restoreStoppingInstances rebuilds only the projection of durable, unapplied
// private kill effects; the effects remain the authority for actual stop.
func (p *Processor) restoreStoppingInstances(ctx context.Context, o Owner, generation int64) error {
	rows, err := p.journal.db.QueryContext(ctx, `SELECT e.body FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND a.effect_id IS NULL AND json_extract(e.body,'$.event.DaemonCommand.Kind')=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, control.DaemonCommandKillHeadless)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		event, e := DecodeEffectEvent(raw)
		if e != nil {
			return e
		}
		if event.DaemonCommand == nil || event.DaemonCommand.InstanceID == "" || event.SurfaceSessionID != o.SurfaceID {
			return ErrJournalConflict
		}
		p.app.Service().MarkPrivateInstanceStopping(event.DaemonCommand.InstanceID)
	}
	return rows.Err()
}
