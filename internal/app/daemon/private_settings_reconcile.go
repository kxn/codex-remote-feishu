package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

// privateSettingsOwnerLease must validate the current execution owner and hold
// that exact generation while its local reconciliation transaction commits.
type privateSettingsOwnerLease interface {
	WithPrivateOwner(context.Context, privatechat.Owner, int64, func() error) error
}

func reconcilePrivateBotSettings(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, lease privateSettingsOwnerLease, o privatechat.Owner, generation int64) error {
	if host == nil || p == nil || j == nil {
		return privatechat.ErrJournalState
	}
	var guard privatechat.ExecutionGuard
	if lease != nil {
		guard = func(commit func() error) error { return lease.WithPrivateOwner(ctx, o, generation, commit) }
	}
	versions, err := j.PendingSettingsReconciliations(ctx, o, generation, 64)
	if err != nil {
		return err
	}
	for _, version := range versions {
		snapshot, e := host.readPrivateBotSettings(o.GatewayID, o.AppID)
		if e != nil {
			return e
		}
		if e = p.RefreshBotSettings(ctx, o, generation, snapshot.Record, func() bool { return host.privateSettingsRevision.Load() == snapshot.Revision }); e != nil {
			return e
		}
		if _, e = p.ApplySettingsReconcile(ctx, o, generation, version, snapshot.Fingerprint, func() bool { return host.privateSettingsRevision.Load() == snapshot.Revision }, guard); e != nil {
			return e
		}
	}
	return host.reconcilePrivateCodexContracts(ctx, p, o, generation, guard)
}
