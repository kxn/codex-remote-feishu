package daemon

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Warm only configured profile directories. This lane has no worker/host
// dependency, cannot create business inputs, and never sends a Feishu message.
func (a *App) warmProfileModelCatalogs(ctx context.Context) error {
	var bindings []work.RoleBotBinding
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; bindings, e = tx.ListRoleBotBindings(); return e }); err != nil {
		return err
	}
	var problems []error
	for _, binding := range bindings {
		snapshot, err := (teamBotSettingsPort{app: a}).Snapshot(ctx, binding)
		if err != nil {
			continue
		}
		if snapshot.Settings.Backend != agentproto.BackendCodex || teamFixedModel(snapshot) != "" {
			continue
		}
		target, err := a.teamProfileCatalog(ctx, binding, teamSettingsProfile(snapshot.Settings), "")
		if err != nil {
			continue
		}
		if _, err = a.readOrRefreshProfileCatalog(ctx, target, false); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
