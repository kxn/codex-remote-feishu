package workservice

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (s *Service) authorizeBootstrapWorker(tx *workstore.Tx, claims workauth.Claims, runRef string) (Caller, work.WorkScope, error) {
	var c Caller
	var scope work.WorkScope
	worker, err := tx.GetWorker(claims.WorkerID)
	if err != nil {
		return c, scope, workauth.ErrUnauthorized
	}
	attempt, err := tx.GetAttemptByRunRef(runRef)
	if err != nil {
		return c, scope, workauth.ErrUnauthorized
	}
	binding, err := tx.GetRoleBotBinding(claims.RoleID)
	if err != nil {
		return c, scope, workauth.ErrUnauthorized
	}
	gates, err := tx.ListPauseGates(worker.ScopeID)
	if err != nil {
		return c, scope, err
	}
	if err = workauth.AuthorizeRun(claims, runRef, worker, attempt, binding, gates); err != nil {
		return c, scope, err
	}
	scope, err = tx.GetScope(worker.ScopeID)
	if err != nil {
		return c, scope, err
	}
	if scope.Kind != work.ScopeBootstrap || scope.RoleID != worker.RoleID || attempt.TaskID != "" {
		return c, scope, workauth.ErrUnauthorized
	}
	return Caller{Claims: claims, Worker: worker, Attempt: attempt}, scope, nil
}

type BootstrapTeam struct {
	Team  work.Team   `json:"team"`
	Roles []work.Role `json:"roles"`
}
type BootstrapView struct {
	Scope           work.WorkScope                 `json:"scope"`
	Teams           []BootstrapTeam                `json:"teams"`
	Roles           []work.Role                    `json:"configured_roles"`
	Inputs          []BootstrapInput               `json:"inputs"`
	DirectoryChoice *workstore.ExecutionPreference `json:"directory_choice,omitempty"`
}
type BootstrapInput struct {
	ID      string       `json:"id"`
	Message work.Message `json:"message"`
	Text    string       `json:"text"`
}

// BootstrapContext exposes configured teammates and this establishment's exact
// inputs, without borrowing any private conversation or other project's memory.
func (s *Service) BootstrapContext(ctx context.Context, token, runRef string) (BootstrapView, error) {
	var view BootstrapView
	claims, err := s.signer.Verify(token)
	if err != nil {
		return view, err
	}
	apps, err := s.bootstrapConfiguredApps()
	if err != nil {
		return view, err
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		c, scope, err := s.authorizeBootstrapWorker(tx, claims, runRef)
		if err != nil {
			return err
		}
		view.Scope = scope
		roles, err := tx.ListRoles()
		if err != nil {
			return err
		}
		for _, role := range roles {
			binding, err := tx.GetRoleBotBinding(role.ID)
			if err != nil {
				if errors.Is(err, workstore.ErrNotFound) {
					continue
				}
				return err
			}
			if bootstrapAppEnabled(apps, binding) {
				view.Roles = append(view.Roles, role)
			}
		}
		choice, err := tx.GetExecutionPreference(scope.ID)
		if err == nil {
			view.DirectoryChoice = &choice
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		teams, err := tx.ListTeams()
		if err != nil {
			return err
		}
		for _, team := range teams {
			member, e := tx.GetTeamMember(team.ID, c.Worker.RoleID)
			if e != nil || !member.Active {
				continue
			}
			members, e := tx.ListTeamMembers(team.ID)
			if e != nil {
				return e
			}
			team, e = teamViewForScope(tx, team, scope)
			if e != nil {
				return e
			}
			item := BootstrapTeam{Team: team, Roles: []work.Role{}}
			for _, candidate := range members {
				if !candidate.Active {
					continue
				}
				binding, e := tx.GetRoleBotBinding(candidate.RoleID)
				if errors.Is(e, workstore.ErrNotFound) || e == nil && !bootstrapAppEnabled(apps, binding) {
					continue
				}
				if e != nil {
					return e
				}
				role, e := tx.GetRole(candidate.RoleID)
				if e != nil {
					return e
				}
				item.Roles = append(item.Roles, role)
			}
			view.Teams = append(view.Teams, item)
		}
		for _, id := range c.Attempt.InputIDs {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.MessageID == "" {
				continue
			}
			message, e := tx.GetMessage(input.MessageID)
			if e != nil {
				return e
			}
			body, e := tx.GetContent(scope.ID, message.BodyRef)
			if e != nil {
				return e
			}
			view.Inputs = append(view.Inputs, BootstrapInput{ID: id, Message: message, Text: string(body.Data)})
		}
		return nil
	})
	return view, err
}
