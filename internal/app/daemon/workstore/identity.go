package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

func (t *Tx) PutPrincipal(v work.Principal, expected int64) error {
	return t.record("principals", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetPrincipal(id string) (work.Principal, error) {
	var v work.Principal
	err := t.read("principals", id, &v)
	return v, err
}
func (t *Tx) ListPrincipals() ([]work.Principal, error) {
	return listRecords[work.Principal](t, "principals", "")
}
func (t *Tx) PutRole(v work.Role, expected int64) error {
	return t.record("roles", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetRole(id string) (work.Role, error) {
	var v work.Role
	err := t.read("roles", id, &v)
	return v, err
}
func (t *Tx) ListRoles() ([]work.Role, error) { return listRecords[work.Role](t, "roles", "") }
func (t *Tx) PutTeam(v work.Team, expected int64) error {
	if err := t.exists("roles", v.LeaderRoleID); err != nil {
		return err
	}
	return t.record("teams", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetTeam(id string) (work.Team, error) {
	var v work.Team
	err := t.read("teams", id, &v)
	return v, err
}
func (t *Tx) ListTeams() ([]work.Team, error) { return listRecords[work.Team](t, "teams", "") }
func (t *Tx) PutScope(v work.WorkScope, expected int64) error {
	if expected > 0 {
		old, err := t.GetScope(v.ID)
		if err != nil {
			return err
		}
		if err = immutable(old.Tenant != v.Tenant || old.ChatID != v.ChatID || old.BootstrapOperationID != v.BootstrapOperationID || old.BindingGeneration != v.BindingGeneration || old.RoleID != v.RoleID || old.Kind != v.Kind || old.PrincipalID != v.PrincipalID || old.TeamID != v.TeamID || old.ProjectID != v.ProjectID); err != nil {
			return err
		}
	}
	return t.record("scopes", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetScope(id string) (work.WorkScope, error) {
	var v work.WorkScope
	err := t.read("scopes", id, &v)
	return v, err
}
func (t *Tx) ListScopes() ([]work.WorkScope, error) {
	return listRecords[work.WorkScope](t, "scopes", "")
}
func (t *Tx) PutProject(v work.Project, expected int64) error {
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	if err = immutable(scope.Kind != work.ScopeProject || scope.ProjectID != v.ID || scope.TeamID != v.TeamID); err != nil {
		return err
	}
	if expected > 0 {
		old, e := t.GetProject(v.ID)
		if e != nil {
			return e
		}
		if e = immutable(old.ScopeID != v.ScopeID || old.TeamID != v.TeamID); e != nil {
			return e
		}
		if e = work.ValidateProjectTransition(old.State, v.State); e != nil {
			return e
		}
		v.LeaderGeneration = old.LeaderGeneration
		if old.LeaderRoleID != v.LeaderRoleID {
			v.LeaderGeneration = old.LeadershipGeneration() + 1
		}
	} else {
		v.LeaderGeneration = 0
	}
	return t.record("projects", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetProject(id string) (work.Project, error) {
	var v work.Project
	err := t.read("projects", id, &v)
	return v, err
}
func (t *Tx) ListProjects() ([]work.Project, error) {
	return listRecords[work.Project](t, "projects", "")
}
