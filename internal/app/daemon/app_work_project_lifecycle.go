package daemon

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) runTeamProjectLifecycle(ctx context.Context) error {
	if a.teamRuntime == nil {
		return nil
	}
	r := a.teamRuntime
	var projects []work.Project
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		projects, err = tx.ListProjects()
		return err
	}); err != nil {
		return err
	}
	var failures []error
	for _, project := range projects {
		if project.State == work.ProjectArchiving && r.provision != nil {
			if err := r.provision.SettleArchivedEstablishment(ctx, project); err != nil {
				failures = append(failures, err)
				continue
			}
		}
		if project.State == work.ProjectPausing || project.State == work.ProjectArchiving {
			failures = append(failures, r.service.ReconcileProjectLifecycle(ctx, project.ID))
		}
	}
	return errors.Join(failures...)
}
