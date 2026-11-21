package workservice

import (
	"context"

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
	Scope  work.WorkScope   `json:"scope"`
	Teams  []BootstrapTeam  `json:"teams"`
	Inputs []BootstrapInput `json:"inputs"`
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
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		c, scope, err := s.authorizeBootstrapWorker(tx, claims, runRef)
		if err != nil {
			return err
		}
		view.Scope = scope
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
			item := BootstrapTeam{Team: team, Roles: []work.Role{}}
			for _, candidate := range members {
				if !candidate.Active {
					continue
				}
				if _, e = tx.GetRoleBotBinding(candidate.RoleID); e != nil {
					continue
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
