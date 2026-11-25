package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type privateStagingBaseProvider interface {
	PrivateStagingBase(gatewayID, appID string) (string, error)
}

// runPrivateStagingCleanup sweeps durable target receipts and canceled ingress.
// Foreign, historical, replaced or unverifiable files remain.
func runPrivateStagingCleanup(ctx context.Context, host *App, j *privatechat.Journal) error {
	if err := cleanupCompletedPrivateFileSnapshots(ctx, host.headlessRuntime.Paths.StateDir, j); err != nil {
		return err
	}
	provider, ok := host.gateway.(privateStagingBaseProvider)
	if !ok {
		return fmt.Errorf("private staging base unavailable")
	}
	const pageSize = 128
	var after privatechat.StagingCleanupPosition
	for {
		work, err := j.PendingStagingCleanupAfter(ctx, pageSize, after)
		if err != nil {
			return err
		}
		for _, item := range work {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := cleanupPrivateStagingItem(ctx, provider, j, item); err != nil {
				return err
			}
		}
		if len(work) < pageSize {
			return nil
		}
		after = work[len(work)-1].Position()
	}
}

func cleanupPrivateStagingItem(ctx context.Context, provider privateStagingBaseProvider, j *privatechat.Journal, item privatechat.StagingCleanup) error {
	if item.IngressSequence == 0 {
		effect, err := j.LoadEffect(ctx, item.Owner, item.Generation, item.EffectID)
		if err != nil {
			return nil
		}
		event, _, err := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if err != nil {
			return nil
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput && event.DaemonCommand.StageObjectID == "" {
			return j.FinishStagingCleanup(ctx, item, true)
		}
	}
	base, err := provider.PrivateStagingBase(item.Owner.GatewayID, item.Owner.AppID)
	if err != nil {
		return nil
	}
	owner := privatestaging.Owner{Tenant: item.Owner.TenantID, AppID: item.Owner.AppID, Gateway: item.Owner.GatewayID, EventID: item.EventID, MessageID: item.MessageID}
	if !privatestaging.IsManagedPath(base, owner, item.Path) {
		return j.FinishStagingCleanup(ctx, item, true)
	}
	hash := item.SHA256
	if hash == "" {
		hash, err = privatestaging.ExpectedHash(base, owner, item.Path)
	}
	if err == nil && hash != "" {
		err = privatestaging.Cleanup(base, owner, item.Path, hash)
	}
	if err != nil && !errors.Is(err, privatestaging.ErrUntrustedSource) {
		return nil
	}
	return j.FinishStagingCleanup(ctx, item, err != nil)
}
