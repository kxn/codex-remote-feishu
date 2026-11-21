package workprovision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestProjectHomeRefreshProjectsStoppedLifecycleInPlace(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	if report, err := f.service.Run(ctx, f.operationID); err != nil || report.State != "confirmed" {
		t.Fatalf("provisioning = %s, %v", report.State, err)
	}
	for _, step := range []struct {
		state work.ProjectState
		label string
	}{{work.ProjectPausing, "正在暂停"}, {work.ProjectPaused, "已暂停"}, {work.ProjectArchiving, "正在归档"}, {work.ProjectArchived, "已归档"}} {
		if err := f.store.Within(ctx, func(tx *workstore.Tx) error {
			project, err := tx.GetProject(f.projectID)
			if err != nil {
				return err
			}
			project.State = step.state
			project.Revision++
			return tx.PutProject(project, project.Revision-1)
		}); err != nil {
			t.Fatal(err)
		}
		if err := f.service.RefreshHome(ctx, f.projectID); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.leader.blocks.Index.Text, "项目状态："+step.label) || f.leader.documentCalls != 1 || f.leader.appendCalls != 1 {
			t.Fatalf("homepage moved or did not show %s: %+v", step.label, f.leader.blocks)
		}
	}
}

func TestFixedNavigationDetectsHumanRemovalAndOnlyRestoresExplicitly(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	humanTab := feishu.ProjectChatTab{ID: "human-tab", Name: "项目", Type: "url", URL: "https://human.example"}
	f.leader.tabs = []feishu.ProjectChatTab{humanTab}
	f.leader.pins["human-pin"] = true
	if report, err := f.service.Run(ctx, f.operationID); err != nil || report.State != "confirmed" {
		t.Fatalf("provisioning = %s, %v", report.State, err)
	}
	f.leader.tabs = []feishu.ProjectChatTab{humanTab}
	delete(f.leader.pins, "navigation-message")
	readHome := func() workstore.ProjectHome {
		t.Helper()
		var home workstore.ProjectHome
		if err := f.store.Within(ctx, func(tx *workstore.Tx) error {
			var err error
			home, err = tx.GetProjectHome(f.projectID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return home
	}
	for range 2 {
		if err := f.service.ObserveFixedNavigation(ctx, f.projectID); err != nil {
			t.Fatal(err)
		}
	}
	home := readHome()
	if !home.NavigationTabMissing || !home.NavigationPinMissing || home.NavigationGeneration != 1 || f.leader.pinCalls != 1 || f.leader.tabCalls != 1 {
		t.Fatalf("removal must be recorded without writes: %+v", home)
	}
	if err := f.service.RestoreFixedTab(ctx, f.projectID, home.NavigationGeneration); err != nil {
		t.Fatal(err)
	}
	if home = readHome(); home.NavigationTabMissing || !home.NavigationPinMissing || f.leader.tabCalls != 2 || f.leader.tabs[0] != humanTab {
		t.Fatalf("restore failed or changed a manually managed tab: %+v %v", home, f.leader.tabs)
	}
	if f.leader.pinCalls != 1 || !f.leader.pins["human-pin"] {
		t.Fatal("tab recovery modified existing pins")
	}
}

func TestFixedTabRestoreDoesNotReplayUnknownCreate(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	if report, err := f.service.Run(ctx, f.operationID); err != nil || report.State != "confirmed" {
		t.Fatalf("provisioning = %s, %v", report.State, err)
	}
	f.leader.tabs = nil
	if err := f.service.ObserveFixedNavigation(ctx, f.projectID); err != nil {
		t.Fatal(err)
	}
	f.leader.tabErr = context.DeadlineExceeded
	f.leader.tabHasID = false
	if err := f.service.RestoreFixedTab(ctx, f.projectID, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unknown result lost: %v", err)
	}
	// An independent Pin removal changes the UI observation. It must not
	// change the old Tab's operation identity or authorize another create.
	delete(f.leader.pins, "navigation-message")
	if err := f.service.ObserveFixedNavigation(ctx, f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RestoreFixedTab(ctx, f.projectID, 2); !errors.Is(err, ErrUnknown) {
		t.Fatalf("second request should observe unknown and wait for explicit reconciliation: %v", err)
	}
	if f.leader.tabCalls != 2 {
		t.Fatalf("unknown results caused extra tab creation: %d", f.leader.tabCalls)
	}
}

func TestProjectHomeLinksTeamOriginalOnlyForAuthorizedAudience(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	const teamURL = "https://a.feishu.cn/docx/shared-original"
	setMaterials := func(tx *workstore.Tx) error {
		team, err := tx.GetTeam("team")
		if err != nil {
			return err
		}
		source, err := tx.GetMessage(f.sourceID)
		if err != nil {
			return err
		}
		team.Materials = &work.TeamMaterials{
			ScopeID: source.ScopeID, Generation: 1, Identity: f.leader.binding,
			Selection: work.TeamMaterialsSelection{Mode: "existing", URL: teamURL},
			State:     "ready", URL: teamURL, DocumentID: "shared-original",
			Source: work.SourceRef{Kind: "message", ID: source.ID, Version: "1", EvidenceRef: source.BodyRef},
		}
		team.Revision++
		return tx.PutTeam(team, team.Revision-1)
	}
	if err := f.store.Within(ctx, setMaterials); err != nil {
		t.Fatal(err)
	}
	if report, err := f.service.Run(ctx, f.operationID); err != nil || report.State != "confirmed" {
		t.Fatalf("provisioning = %s, %v", report.State, err)
	}
	if strings.Contains(f.leader.blocks.Index.Text, teamURL) {
		t.Fatal("roster reuse exposed the team original")
	}
	if err := f.store.Within(ctx, func(tx *workstore.Tx) error {
		source, err := tx.GetMessage(f.sourceID)
		if err != nil {
			return err
		}
		return tx.PutTeamOwnership(work.TeamOwnership{TeamID: "team", PrincipalID: source.Source.ID, UpdatedBy: source.Source, Source: work.SourceRef{Kind: "message", ID: source.ID, Version: "1", EvidenceRef: source.BodyRef}, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RefreshHome(ctx, f.projectID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.leader.blocks.Index.Text, teamURL) {
		t.Fatal("past project creation silently authorized this original")
	}
	if err := f.store.Within(ctx, func(tx *workstore.Tx) error {
		team, e := tx.GetTeam("team")
		if e != nil {
			return e
		}
		channels, e := tx.ListProjectChannelBindings(f.projectID)
		if e != nil {
			return e
		}
		_, e = tx.ShareTeamMaterials(team.ID, team.Revision, channels[0], team.Materials.Source)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RefreshHome(ctx, f.projectID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.leader.blocks.Index.Text, "团队共享资料（原件）") || !strings.Contains(f.leader.blocks.Index.Text, teamURL) {
		t.Fatalf("authorized project lost team original: %s", f.leader.blocks.Index.Text)
	}
}
