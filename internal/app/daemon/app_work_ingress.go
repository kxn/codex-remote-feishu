package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) teamIngressHook() *feishu.RawIngressHook {
	r := a.teamRuntime
	if r == nil || !r.active.Load() {
		return nil
	}
	return &feishu.RawIngressHook{
		Authorize: func(ctx context.Context, raw work.RawIngressEnvelope) (bool, error) {
			if !r.active.Load() {
				return false, fmt.Errorf("team authority is not active")
			}
			if err := raw.Validate(); err != nil {
				return false, err
			}
			// The gateway SDK has already authenticated this App's event stream. The
			// prior product admitted those chats; preserve that boundary while checking
			// current Bot identity and tenant rather than inferring identity from IDs.
			if raw.SenderTenant != "" && raw.SenderTenant != raw.Tenant {
				return false, nil
			}
			_, err := (teamDirectory{app: a, store: r.store}).ResolveGateway(ctx, raw.GatewayRef, raw.AppID)
			return err == nil, err
		},
		Receive: func(ctx context.Context, raw work.RawIngressEnvelope) error {
			receipt, err := r.ingress.Receive(ctx, raw)
			if err != nil {
				return err
			}
			if err = a.adoptTeamLegacyHistory(ctx, receipt.IngressID); err != nil {
				return err
			}
			r.wakeLane("ingress")
			return nil
		},
		ReceiveInteraction:   a.receiveTeamInteraction,
		ReceiveMenu:          a.receiveTeamMenu,
		ReceivePlatformEvent: a.receiveTeamPlatformEvent,
	}
}
