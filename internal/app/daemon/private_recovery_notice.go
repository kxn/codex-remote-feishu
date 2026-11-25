package daemon

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// beginRecoveryNotice makes the stale decision and the operation's durable
// begin under the same processor lock as completion and route cancellation.
// It never waits for platform I/O while holding that lock.
func (p *privatePublisher) beginRecoveryNotice(ctx context.Context, o privatechat.Owner, generation int64, id string, ordinal int) (claimed, suppressed bool, err error) {
	err = p.processor.InspectPendingHostIntent(ctx, o, generation, id, func(app *privatechat.App, effect privatechat.EffectRecord) error {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			return e
		}
		if event.TimelineText == nil || event.TimelineText.Type != control.TimelineTextRecoveryInputWaiting {
			return nil
		}
		claimed = true
		if app.Service().PrivateRecoveryNoticeObsolete(o.SurfaceID, event.SourceMessageID) {
			suppressed = true
			return p.journal.SuppressPreparedPublication(ctx, o, generation, id, "obsolete_recovery_input", time.Now())
		}
		return p.journal.BeginPublicationOperation(ctx, o, generation, id, ordinal)
	})
	return
}
