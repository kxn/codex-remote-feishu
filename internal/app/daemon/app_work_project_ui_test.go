package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func teamProjectViewFixture(t *testing.T, appIDs ...string) *App {
	t.Helper()
	appID := "app"
	if len(appIDs) > 0 {
		appID = appIDs[0]
	}
	store, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a := &App{teamRuntime: &teamRuntime{store: store}}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.PutRole(work.Role{ID: "leader", Name: "Leader", Description: "Lead", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: appID, IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "leader", Active: true, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopeProject, ProjectID: "project", TeamID: "team", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProject(work.Project{ID: "project", ScopeID: "scope", TeamID: "team", Name: "Product", LeaderRoleID: "leader", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "chat", Purpose: "main", BindingGeneration: 1, Active: true}, 0); err != nil {
			return err
		}
		return tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: "leader", State: work.ProjectMemberReady, BotGeneration: 1, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestProjectViewsRepeatTicksDoNotPublishDuplicates(t *testing.T) {
	a := teamProjectViewFixture(t)
	for range 3 {
		if err := a.runTeamProjectViews(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	err := a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		if err != nil {
			return err
		}
		outbox, err := tx.ListOutbox("scope")
		if err != nil {
			return err
		}
		if len(views) != 1 || len(outbox) != 1 || views[0].Kind != "project.status" || views[0].TargetRevision != 1 {
			t.Fatalf("duplicate projections views%d outbox%d", len(views), len(outbox))
		}
		problem := workstore.DispatchProblem{ScopeID: "scope", RoleID: "leader", Reason: "location not ready"}
		return tx.ReplaceDispatchProblems([]workstore.DispatchProblem{problem}, time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.runTeamProjectViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		if err != nil {
			return err
		}
		if len(views) != 2 {
			t.Fatalf("changed facts not projected%d", len(views))
		}
		return tx.ReplaceDispatchProblems([]workstore.DispatchProblem{{ScopeID: "scope", RoleID: "leader", Reason: "location not ready"}}, time.Now().Add(time.Second))
	}); err != nil {
		t.Fatal(err)
	}
	if err = a.runTeamProjectViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, err := tx.ListUIViews("scope")
		if len(views) != 2 {
			t.Fatal("diagnostic timestamp caused card spam")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestProjectPanelLifecycleButtonsPreservePausedAndArchivedSemantics(t *testing.T) {
	for _, test := range []struct {
		state  work.ProjectState
		action string
	}{{work.ProjectPaused, "project.resume"}, {work.ProjectArchived, "project.reopen"}} {
		panel := teamProjectPanel(teamProjectProjection{Project: work.Project{Name: "Human text **not markdown**", State: test.state}, TaskStates: map[work.TaskState]int{}})
		if panel.Buttons[0].Action != test.action {
			t.Fatalf("button %+v", panel.Buttons)
		}
		raw, err := workui.Card("view", panel)
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if json.Unmarshal(raw, &parsed) != nil || !strings.Contains(string(raw), "behaviors") || strings.Contains(string(raw), "markdown") {
			if !strings.Contains(string(raw), "Human text **not markdown**") {
				t.Fatal("lost plain text")
			}
		}
	}
}

func TestProjectPanelUsesHumanNamesAndSummaries(t *testing.T) {
	facts := teamProjectProjection{Project: work.Project{Name: "网站改版", State: work.ProjectDegraded}, Members: []work.ProjectMember{{RoleID: "INTERNAL_ROLE_ID", State: work.ProjectMemberUnavailable}}, MemberNames: map[string]string{"INTERNAL_ROLE_ID": "设计师"}, TaskStates: map[work.TaskState]int{}, Decisions: []string{"首页方案：等待您确认"}, Problems: []string{"设计师：工作目录尚未准备就绪"}, HomeSync: "conflict", HomeURL: "https://example.feishu.cn/docx/stable"}
	raw, err := workui.Card("view", teamProjectPanel(facts))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"INTERNAL_ROLE_ID", "unavailable", "conflict", "project.inspect", "代次"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("leaked implementation vocabulary %s", forbidden)
		}
	}
	if !strings.Contains(string(raw), "设计师") || !strings.Contains(string(raw), "首页方案：等待您确认") {
		t.Fatal("lost human names or actual summary")
	}
}
