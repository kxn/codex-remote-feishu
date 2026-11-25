package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// appendRequestVisibility runs after callback planning, so the recovered
// request can never replace the user's menu response. Unknown or unapplied
// display effects remain the authority and are not sent a second time.
func (p *Processor) appendRequestVisibility(ctx context.Context, o Owner, generation int64, events []eventcontract.Event, alreadyEmitted bool) ([]eventcontract.Event, error) {
	if alreadyEmitted {
		return events, nil
	}
	id := p.app.Service().PendingRequestVisibilityID(o.SurfaceID)
	if id == "" {
		return events, nil
	}
	var pending int
	err := p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e LEFT JOIN private_effect_applied a ON a.effect_id=e.id WHERE e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND json_extract(e.body,'$.event.RequestView.RequestID')=? AND (e.state<>'confirmed' OR a.effect_id IS NULL)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, id).Scan(&pending)
	if err != nil {
		return nil, err
	}
	if pending != 0 {
		return events, nil
	}
	return append(events, p.app.Service().ReplayActivePendingRequestVisibility(o.SurfaceID)...), nil
}
