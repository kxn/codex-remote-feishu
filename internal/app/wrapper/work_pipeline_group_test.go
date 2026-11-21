package wrapper

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSDKGroupLeaderExpertReplyNativePipeline(t *testing.T) {
	for _, capacity := range []int{1, 2} {
		t.Run("capacity-"+strconv.Itoa(capacity), func(t *testing.T) { runSDKNativePipeline(t, work.BackendCodex, capacity) })
	}
}

func pipelineSeedGroup(tx *workstore.Tx) error {
	if err := tx.PutRole(work.Role{ID: "pipeline-expert", Name: "Expert", Description: "Perform the delegated analysis and reply internally to the leader", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
		return err
	}
	if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "pipeline-expert", GatewayRef: "pipeline-gateway-expert", AppID: "pipeline-app-expert", IdentityGeneration: 1}, 0); err != nil {
		return err
	}
	if err := tx.PutTeam(work.Team{ID: "pipeline-team", Name: "Pipeline team", LeaderRoleID: "pipeline-role", Revision: 1}, 0); err != nil {
		return err
	}
	for _, role := range []string{"pipeline-role", "pipeline-expert"} {
		if err := tx.PutTeamMember(work.TeamMember{TeamID: "pipeline-team", RoleID: role, Active: true, Revision: 1}, 0); err != nil {
			return err
		}
	}
	if err := tx.PutScope(work.WorkScope{ID: "pipeline-project-scope", Kind: work.ScopeProject, TeamID: "pipeline-team", ProjectID: "pipeline-project", Revision: 1}, 0); err != nil {
		return err
	}
	if err := tx.PutProject(work.Project{ID: "pipeline-project", ScopeID: "pipeline-project-scope", TeamID: "pipeline-team", Name: "Actual group handoff", LeaderRoleID: "pipeline-role", State: work.ProjectActive, Revision: 1}, 0); err != nil {
		return err
	}
	for _, role := range []string{"pipeline-role", "pipeline-expert"} {
		if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "pipeline-project", RoleID: role, State: work.ProjectMemberReady, Revision: 1}, 0); err != nil {
			return err
		}
	}
	return tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "pipeline-project", Tenant: "pipeline-tenant", ChatID: "pipeline-group-chat", Purpose: "main", BindingGeneration: 1, Active: true}, 0)
}

type pipelineOverlapEvidence struct{ observed atomic.Bool }

func (p *pipelineOverlapEvidence) handler(store *workstore.Store, scopeID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.observed.Load() {
			err := store.Within(r.Context(), func(tx *workstore.Tx) error {
				attempts, err := tx.ListAttempts(scopeID)
				if err != nil {
					return err
				}
				running := 0
				for _, attempt := range attempts {
					if attempt.State != work.AttemptRunning {
						continue
					}
					count, e := tx.CountOpenNativeTurns(attempt.ID)
					if e != nil {
						return e
					}
					if count > 0 {
						running++
					}
				}
				if running >= 2 {
					p.observed.Store(true)
				}
				return nil
			})
			if err != nil {
				http.Error(w, "inspection unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		if !p.observed.Load() {
			http.Error(w, "awaiting a second actual native turn", http.StatusConflict)
			return
		}
		_, _ = fmt.Fprint(w, "two independently bound native turns are open")
	})
}

func pipelineGroupSourcePrincipal(ctx context.Context, store *workstore.Store, input work.Inbox) (string, error) {
	var principal string
	err := store.Within(ctx, func(tx *workstore.Tx) error {
		message, err := tx.GetMessage(input.MessageID)
		if err != nil {
			return err
		}
		principal = message.Source.ID
		return nil
	})
	return principal, err
}
