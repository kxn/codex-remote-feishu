package daemon

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workreaction"
)

func (a *App) runTeamPrivateReactions(ctx context.Context) error {
	if err := a.runTeamPrivateReactionActions(ctx); err != nil {
		return err
	}
	service := workreaction.Service{Store: a.teamRuntime.store, Resolver: teamPublisherResolver{directory: teamDirectory{app: a, store: a.teamRuntime.store}}}
	return service.RunOnce(ctx, 16)
}
