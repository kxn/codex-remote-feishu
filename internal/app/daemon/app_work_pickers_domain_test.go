package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func teamPickerFixture(t *testing.T, appIDs ...string) (*App, teamHumanSource) {
	t.Helper()
	appID := "app"
	roleID := "leader"
	if len(appIDs) > 1 {
		roleID = appIDs[1]
	}
	if len(appIDs) > 0 {
		appID = appIDs[0]
	}
	s, e := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "work.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	a := &App{teamRuntime: &teamRuntime{store: s}}
	a.headlessRuntime.Paths.StateDir = t.TempDir()
	signer, e := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if e != nil {
		t.Fatal(e)
	}
	a.teamRuntime.service, e = workservice.New(s, signer, nil)
	if e != nil {
		t.Fatal(e)
	}
	var source teamHumanSource
	e = s.Within(t.Context(), func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: roleID, Name: "Leader", Description: "Coordinate", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: appID, RoleID: roleID, BotGeneration: 1}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: gateway.RoleID, GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		menu, _, e := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "menu", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "human-open"}, EventKey: "menu", ReceivedAt: time.Now().UTC()}, gateway)
		if e != nil {
			return e
		}
		sending, _, e := tx.BeginMenuDestination(menu.ID, menu.Revision, "Actual SDK entry fixture")
		if e != nil {
			return e
		}
		if e = tx.FinishMenuDestination(menu.ID, sending.Revision, "private-chat", "entry-message", "", true); e != nil {
			return e
		}
		source = teamHumanSource{Key: menu.ID, ScopeID: menu.ScopeID, PrincipalID: menu.PrincipalID, RoleID: gateway.RoleID, Tenant: "tenant", ChatID: "private-chat", MessageID: "entry-message", Gateway: gateway, SenderIDs: menu.Envelope.SenderIDs, NavigationRevision: menu.NavigationRevision, Authority: workstore.PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return a, source
}
func pickerTx(t *testing.T, a *App, fn func(*workstore.Tx) error) {
	t.Helper()
	if e := a.teamRuntime.store.Within(t.Context(), fn); e != nil {
		t.Fatal(e)
	}
}
func pickerAction(t *testing.T, a *App, source teamHumanSource, id, action string) (workstore.UIView, workstore.HumanInteraction) {
	t.Helper()
	var view workstore.UIView
	var interaction workstore.HumanInteraction
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var e error
		view, e = tx.GetUIView(id)
		if e != nil {
			return e
		}
		view.ExternalMessageID = "actual-card:" + id
		view.Revision++
		if e = tx.PutUIView(view, view.Revision-1); e != nil {
			return e
		}
		body, e := tx.GetContent(view.ScopeID, view.BodyRef)
		if e != nil {
			return e
		}
		var outer struct {
			Card json.RawMessage `json:"card"`
		}
		if e = json.Unmarshal(body.Data, &outer); e != nil {
			return e
		}
		var card struct {
			Body struct {
				Elements []struct {
					Behaviors []struct {
						Value map[string]any `json:"value"`
					} `json:"behaviors"`
				} `json:"elements"`
			} `json:"body"`
		}
		if e = json.Unmarshal(outer.Card, &card); e != nil {
			return e
		}
		var choice map[string]any
		for _, element := range card.Body.Elements {
			for _, behavior := range element.Behaviors {
				if behavior.Value["action"] == action {
					choice = behavior.Value
					break
				}
			}
		}
		if choice == nil {
			t.Fatalf("missing action %s", action)
		}
		interaction, _, e = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "click:" + id, Tenant: source.Tenant, SenderIDs: source.SenderIDs, ChatID: source.ChatID, MessageID: view.ExternalMessageID, Kind: "card", Values: choice, ReceivedAt: time.Now().UTC()}, source.Gateway)
		return e
	})
	return view, interaction
}
func TestTeamPickerSelectsActualDirectoryAndRejectsStaleCard(t *testing.T) {
	a, source := teamPickerFixture(t)
	hostname, _ := os.Hostname()
	path := t.TempDir()
	hostID := teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.PutLocation(work.ExecutionLocation{ID: "repository", HostID: hostID, Cwd: path, Generation: 1, Revision: 1}, 0)
	})
	id, e := a.queueTeamResourcePicker(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	view, interaction := pickerAction(t, a, source, id, "picker.location.select")
	result, e := a.handleTeamPickerAction(t.Context(), interaction, view, "picker.location.select")
	if e != nil {
		t.Fatal(e)
	}
	if result == "" {
		t.Fatal("missing durable result view")
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		preference, e := tx.GetExecutionPreference(source.ScopeID)
		if e != nil {
			return e
		}
		if preference.DefaultLocationID != "repository" || preference.SelectedBy.ID != source.PrincipalID {
			t.Fatal("human directory selection not saved")
		}
		return nil
	})
	var stale workstore.UIView
	pickerTx(t, a, func(tx *workstore.Tx) error { var e error; stale, e = tx.GetUIView(view.ID); return e })
	if !stale.Invalidated {
		t.Fatal("old picker still active")
	}
	if _, e = a.handleTeamPickerAction(t.Context(), interaction, stale, "picker.location.select"); e == nil {
		t.Fatal("stale picker remains actionable")
	}
}
func TestTeamPickerDoesNotDiscloseUnverifiedProjectOrSelectScratch(t *testing.T) {
	a, source := teamPickerFixture(t)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if e := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "leader", Active: true, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "project-scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "project", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProject(work.Project{ID: "project", ScopeID: "project-scope", TeamID: "team", Name: "secret project name", LeaderRoleID: "leader", State: work.ProjectActive, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: "leader", State: work.ProjectMemberReady, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0)
	})
	id, e := a.queueTeamProjectPicker(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, e := tx.GetUIView(id)
		if e != nil {
			return e
		}
		body, e := tx.GetContent(view.ScopeID, view.BodyRef)
		if e != nil {
			return e
		}
		if strings.Contains(string(body.Data), "secret project name") {
			t.Fatal("project name disclosed without actual SDK membership")
		}
		return nil
	})
	pickerTx(t, a, func(tx *workstore.Tx) error {
		location := work.ExecutionLocation{ID: "scratch", HostID: "local", Cwd: t.TempDir(), IsolatedContextID: "private-front:" + source.ScopeID, Generation: 1, Revision: 1}
		if e := tx.PutLocation(location, 0); e != nil {
			return e
		}
		body := work.NewContent("selection", source.ScopeID, "text/plain", []byte("human"), time.Now().UTC())
		if e := tx.PutContent(body); e != nil {
			return e
		}
		e := tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: source.ScopeID, SelectedLocationIDs: []string{location.ID}, DefaultLocationID: location.ID, SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, SourceRef: body.ID, Revision: 1}, 0)
		if e == nil {
			t.Fatal("scratch exposed as selectable business directory")
		}
		return nil
	})
}
