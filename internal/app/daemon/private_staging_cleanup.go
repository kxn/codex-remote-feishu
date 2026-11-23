package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
)

type privateStagingBaseProvider interface {
	PrivateStagingBase(gatewayID, appID string) (string, error)
}

// runPrivateStagingCleanup consumes only durable work created after an exact
// target receipt. Foreign, historical, replaced or unverifiable files remain.
func runPrivateStagingCleanup(ctx context.Context, host *App, j *privatechat.Journal) error {
	if err := cleanupCompletedPrivateFileSnapshots(ctx, host.headlessRuntime.Paths.StateDir, j); err != nil {
		return err
	}
	provider, ok := host.gateway.(privateStagingBaseProvider)
	if !ok {
		return fmt.Errorf("private staging base unavailable")
	}
	work, err := j.PendingStagingCleanup(ctx, 128)
	if err != nil {
		return err
	}
	for _, item := range work {
		base, e := provider.PrivateStagingBase(item.Owner.GatewayID, item.Owner.AppID)
		if e != nil {
			continue
		}
		owner := privatestaging.Owner{Tenant: item.Owner.TenantID, AppID: item.Owner.AppID, Gateway: item.Owner.GatewayID, EventID: item.EventID, MessageID: item.MessageID}
		retained := !privatestaging.IsManagedPath(base, owner, item.Path)
		if !retained {
			hash := item.SHA256
			var cleanupErr error
			if hash == "" {
				hash, cleanupErr = privatestaging.ExpectedHash(base, owner, item.Path)
			}
			if cleanupErr == nil && hash != "" {
				cleanupErr = privatestaging.Cleanup(base, owner, item.Path, hash)
			}
			if cleanupErr != nil {
				if !errors.Is(cleanupErr, privatestaging.ErrUntrustedSource) {
					continue
				}
				retained = true
			}
		}
		if e = j.FinishStagingCleanup(ctx, item, retained); e != nil {
			return e
		}
	}
	return nil
}
