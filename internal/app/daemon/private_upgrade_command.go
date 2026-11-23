package daemon

import (
	"context"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// Upgrade commands use the existing shared install owner. Long-running owner
// flows remain pending until their durable continuation is implemented.
func runPrivateUpgradeCommand(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.SurfaceSessionID != o.SurfaceID || event.SurfaceSessionID != o.SurfaceID {
		return nil, privatechat.ErrJournalConflict
	}
	if cmd.Kind == control.DaemonCommandUpgrade {
		parsed, e := parseUpgradeCommandText(cmd.Text)
		if e == nil && parsed.Mode == upgradeCommandCodex {
			return runPrivateCodexUpgrade(ctx, host, p, j, o, generation, record, *cmd)
		}
		if e == nil && parsed.Mode == upgradeCommandLocal {
			return runPrivateUpgradeLocal(ctx, host, p, j, o, generation, record, *cmd)
		}
		if e == nil && (parsed.Mode == upgradeCommandLatest || parsed.Mode == upgradeCommandDev) {
			return runPrivateUpgradeLatest(ctx, host, p, j, o, generation, record, *cmd)
		}
		return runPrivateSystemCommand(ctx, host, p, j, o, generation, id)
	}
	if cmd.Kind == control.DaemonCommandUpgradeOwnerFlow && strings.HasPrefix(cmd.PickerID, privateCodexUpgradePrefix) {
		return runPrivateCodexUpgrade(ctx, host, p, j, o, generation, record, *cmd)
	}
	if cmd.Kind == control.DaemonCommandUpgradeOwnerFlow && cmd.OptionID == privateUpgradeResult {
		return reconcilePrivateUpgradeResult(ctx, host, p, j, o, generation, record, *cmd, false)
	}
	if cmd.Kind == control.DaemonCommandUpgradeOwnerFlow && cmd.OptionID == privateUpgradeExecute {
		return runPrivateUpgradeExecution(ctx, host, p, j, o, generation, record, *cmd)
	}
	if cmd.Kind == control.DaemonCommandUpgradeOwnerFlow && (cmd.OptionID == upgradeOwnerActionConfirm || cmd.OptionID == upgradeOwnerActionCancel) {
		return runPrivateUpgradeTransition(ctx, host, p, j, o, generation, record, *cmd)
	}
	return nil, privatechat.ErrDeferred
}
