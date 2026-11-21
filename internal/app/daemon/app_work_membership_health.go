package daemon

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Run in its own two-minute lane; SDK events provide immediate negative facts.
func (a *App) runTeamProjectMembershipHealth(ctx context.Context) error {
	if a.teamRuntime == nil || a.teamRuntime.provision == nil {
		return nil
	}
	var projects []string
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, err := tx.ListScopes()
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if scope.Kind == work.ScopeProject {
				projects = append(projects, scope.ProjectID)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, project := range projects {
		if err = a.teamRuntime.provision.CheckMembershipHealth(ctx, project); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
