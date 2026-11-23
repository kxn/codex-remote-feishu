package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

const privateUpgradeResult = "private-result"

// reconcilePrivateUpgradeResult uses only the shared install owner's persisted
// terminal facts. In-flight/unknown helper state is never retried or guessed.
func reconcilePrivateUpgradeResult(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand, unknownExecution bool) ([]privatechat.EffectRecord, error) {
	reducer := privateUpgradeExecutionReducer{host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
	}
	if unknownExecution && record.State != "unknown" || !unknownExecution && record.State != "prepared" {
		return nil, privatechat.ErrDeferred
	}
	var flow *orchestrator.PrivateUpgradeFlow
	if err := p.InspectLocalObservation(ctx, o, generation, record.Effect.ID, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		flow = app.Service().PrivateUpgradeFlow(o.SurfaceID)
		return nil
	}); err != nil {
		return nil, err
	}
	if flow == nil || flow.ID != cmd.PickerID || flow.OwnerUserID != o.UserID || (flow.Phase != "running" && flow.Phase != "cancelling" && flow.Phase != "restarting") {
		return nil, privatechat.ErrJournalConflict
	}
	host.mu.Lock()
	state, exists, err := host.loadUpgradeStateLocked(false)
	host.mu.Unlock()
	if err != nil {
		return nil, err
	}
	pending := state.PendingUpgrade
	if !exists || pending == nil || pending.GatewayID != o.GatewayID || pending.SurfaceSessionID != o.SurfaceID || pending.ChatID != o.ChatID || pending.ActorUserID != o.UserID || pending.SourceMessageID != flow.MessageID || pending.PrivateRequestID != flow.ID || (flow.Source != string(install.UpgradeSourceLocal) && pending.TargetVersion != flow.TargetVersion) || (flow.Source == string(install.UpgradeSourceLocal) && pending.SourceSHA256 != flow.SourceSHA256) {
		return nil, privatechat.ErrDeferred
	}
	phase, title, theme := "", "", ""
	switch pending.Phase {
	case install.PendingUpgradePhaseCommitted:
		phase, title, theme = "completed", "升级完成", "success"
	case install.PendingUpgradePhaseRolledBack, install.PendingUpgradePhaseFailed:
		phase, title, theme = "failed", "升级失败", "error"
	default:
		return nil, privatechat.ErrDeferred
	}
	if !unknownExecution {
		if err = j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	updated := *flow
	updated.Phase = phase
	view := upgradePrivateOwnerView(o, &updated)
	event := upgradeOwnerTerminalEvent(o.SurfaceID, view, title, theme, upgradeOwnerContextSections(flow.CurrentVersion, flow.TargetVersion, flow.Track), upgradeOwnerNoticeSections(buildUpgradeResultText(state)))
	event.GatewayID = o.GatewayID
	event.SurfaceSessionID = o.SurfaceID
	event.SourceMessageID = flow.MessageID
	event.InlineReplaceCurrentCard = true
	eventRaw, err := privatechat.EncodeEffectEvent(event)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(privateUpgradeExecutionReceipt{Version: 1, EffectID: record.Effect.ID, Before: flow, After: &updated, Event: eventRaw})
	if err != nil {
		return nil, err
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(commitCtx, o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
}
