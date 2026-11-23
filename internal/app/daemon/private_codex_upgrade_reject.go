package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

// A stale human interaction is a completed rejection, not an unknown external
// write. Persist its notice without replacing any currently active owner flow.
func rejectPrivateCodexCard(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, text string) ([]privatechat.EffectRecord, error) {
	if record.State == "prepared" {
		if err := j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	event := upgradeNoticeEvent(o.SurfaceID, "codex_upgrade_owner_expired", text)
	event.GatewayID = o.GatewayID
	eventRaw, err := privatechat.EncodeEffectEvent(event)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(privateCodexUpgradeReceipt{Version: 1, EffectID: record.Effect.ID, Event: eventRaw, Rejected: true})
	if err != nil {
		return nil, err
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(commitCtx, o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(commitCtx, o, generation, record.Effect.ID, privateCodexUpgradeReducer{host})
}
