package daemon

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Provisioning validates the chosen resources and launch configuration. The
// scheduler alone creates carriers when there is durable work to execute.
// Repeating this local phase cannot start another native process.
type teamExecutionPreparation struct {
	store    *workstore.Store
	settings teamSettingsResolver
}

func (p teamExecutionPreparation) PrepareProject(ctx context.Context, project work.Project, ids []string) error {
	return p.prepare(ctx, project, "", ids)
}

func (p teamExecutionPreparation) PrepareProjectMember(ctx context.Context, project work.Project, roleID string, ids []string) error {
	return p.prepare(ctx, project, roleID, ids)
}

func (p teamExecutionPreparation) prepare(ctx context.Context, project work.Project, onlyRole string, ids []string) error {
	var scope work.WorkScope
	var roles []work.Role
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		preference, err := tx.GetExecutionPreference(project.ScopeID)
		if err != nil {
			return err
		}
		if !slices.Equal(preference.SelectedLocationIDs, ids) {
			return fmt.Errorf("project resources differ from the human selection")
		}
		scope, err = tx.GetScope(project.ScopeID)
		if err != nil {
			return err
		}
		members, err := tx.ListProjectMembers(project.ID)
		if err != nil {
			return err
		}
		for _, member := range members {
			if member.State != work.ProjectMemberReady || onlyRole != "" && member.RoleID != onlyRole {
				continue
			}
			role, e := tx.GetRole(member.RoleID)
			if e != nil {
				return e
			}
			roles = append(roles, role)
		}
		if onlyRole != "" && len(roles) != 1 {
			return fmt.Errorf("selected project member is not ready")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, role := range roles {
		// Only a deterministic preparation location; no WorkContext or native
		// worker is created by this validation step.
		wc := work.WorkContext{ID: teamID("preparation", project.ID+":"+role.ID), ScopeID: scope.ID, RoleID: role.ID}
		settings, e := p.settings.ResolveLaunch(ctx, scope, role, wc)
		if e != nil {
			return e
		}
		info, e := os.Stat(settings.Location.Cwd)
		if e != nil {
			return e
		}
		if !info.IsDir() {
			return fmt.Errorf("selected execution resource is not a directory")
		}
	}
	return nil
}
