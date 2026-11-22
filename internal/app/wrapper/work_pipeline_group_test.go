package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSDKGroupLeaderExpertReplyNativePipeline(t *testing.T) {
	binary := buildPipelineTeamCLI(t)
	for _, capacity := range []int{1, 2} {
		t.Run("capacity-"+strconv.Itoa(capacity), func(t *testing.T) { runSDKNativePipeline(t, binary, work.BackendCodex, capacity) })
	}
}

func pipelineSeedGroup(tx *workstore.Tx, collaboration bool) error {
	if err := tx.PutRole(work.Role{ID: "pipeline-expert", Name: "Expert", Description: "Perform the delegated analysis and reply internally to the leader", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
		return err
	}
	if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "pipeline-expert", GatewayRef: "pipeline-gateway-expert", AppID: "pipeline-app-expert", IdentityGeneration: 1}, 0); err != nil {
		return err
	}
	roles := []string{"pipeline-role", "pipeline-expert"}
	if collaboration {
		if err := tx.PutRole(work.Role{ID: "pipeline-reviewer", Name: "Reviewer", Description: "Check the export independently", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "pipeline-reviewer", GatewayRef: "pipeline-gateway-reviewer", AppID: "pipeline-app-reviewer", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		roles = append(roles, "pipeline-reviewer")
	}
	if err := tx.PutTeam(work.Team{ID: "pipeline-team", Name: "Pipeline team", LeaderRoleID: "pipeline-role", Revision: 1}, 0); err != nil {
		return err
	}
	for _, role := range roles {
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
	for _, role := range roles {
		binding, err := tx.GetRoleBotBinding(role)
		if err != nil {
			return err
		}
		if err = tx.PutProjectMember(work.ProjectMember{
			ProjectID: "pipeline-project", RoleID: role, State: work.ProjectMemberReady,
			BotGeneration: binding.IdentityGeneration, Revision: 1,
		}, 0); err != nil {
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

func (p *pipelinePublisher) PublishCard(ctx context.Context, req feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	var card struct {
		Schema string `json:"schema"`
		Config struct {
			UpdateMulti bool `json:"update_multi"`
		} `json:"config"`
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal(req.Card, &card); err != nil {
		return feishu.ProjectPublishReceipt{}, err
	}
	if card.Schema != "2.0" || !card.Config.UpdateMulti || len(card.Body.Elements) != 1 || card.Body.Elements[0].Tag != "markdown" {
		return feishu.ProjectPublishReceipt{}, fmt.Errorf("expected one editable group progress card")
	}
	return p.PublishText(ctx, feishu.ProjectPublishRequest{
		ChatID: req.ChatID, ReplyToMessageID: req.ReplyToMessageID, ReplyInThread: req.ReplyInThread,
		Text: card.Body.Elements[0].Content, UUID: req.UUID,
	})
}
