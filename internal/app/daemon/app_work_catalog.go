package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

// A model list comes from the selected profile's real carrier. Preparing it
// creates neither a business attempt nor a prompt, and never borrows a legacy
// surface or another person's current native session.
func (a *App) prepareTeamModelCatalog(ctx context.Context, source teamHumanSource) error {
	var need workengine.WorkerNeed
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, _, err := tx.ValidatePrivateAction(source.Authority)
		if err != nil {
			return err
		}
		frozen, revision, err := worksettings.FrozenContext(tx, source.Authority)
		if err != nil {
			return err
		}
		if revision != source.NavigationRevision || scope.PrincipalID != source.PrincipalID || scope.RoleID != source.RoleID {
			return fmt.Errorf("工作位置已经变化，请重新打开模型选择。")
		}
		need = workengine.WorkerNeed{ScopeID: scope.ID, RoleID: scope.RoleID, ContextID: frozen.ID}
		return nil
	})
	if err != nil {
		return err
	}
	return a.teamRuntime.host.Prepare(ctx, need)
}
