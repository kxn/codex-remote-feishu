package workteamhome

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type materialPlatform struct {
	workprovision.Platform
	folders, documents, intros, wikis, reads int
	createError, readError                   error
	grants                                   map[string]bool
	started, release                         chan struct{}
}

func (p *materialPlatform) CreateFolder(context.Context, string, string) (workprovision.Folder, error) {
	p.folders++
	return workprovision.Folder{ID: "team-folder", URL: "https://a.feishu.cn/drive/folder/team-folder"}, nil
}
func (p *materialPlatform) CreateDocument(context.Context, string, string) (workprovision.HomeDocument, error) {
	p.documents++
	if p.started != nil {
		close(p.started)
		<-p.release
	}
	if p.createError != nil {
		return workprovision.HomeDocument{}, p.createError
	}
	return workprovision.HomeDocument{ID: "team-original", URL: "https://a.feishu.cn/docx/team-original"}, nil
}
func (p *materialPlatform) CheckFolder(context.Context, string) error { return nil }
func (p *materialPlatform) ReadDocument(context.Context, string) (workprovision.HomeDocument, error) {
	p.reads++
	return workprovision.HomeDocument{ID: "team-original"}, p.readError
}
func (p *materialPlatform) ReadDocumentURL(context.Context, string) (string, error) {
	return "https://a.feishu.cn/docx/team-original", p.readError
}
func (p *materialPlatform) ResolveHome(context.Context, string) (workprovision.HomeDocument, error) {
	p.reads++
	return workprovision.HomeDocument{ID: "selected-original", URL: "https://a.feishu.cn/docx/selected-original"}, p.readError
}
func (p *materialPlatform) InitializeHome(context.Context, string, string, string, string) error {
	p.intros++
	return nil
}
func (p *materialPlatform) ResolveWikiParent(context.Context, string) (workprovision.WikiParent, error) {
	return workprovision.WikiParent{SpaceID: "existing-space", NodeToken: "existing-parent"}, nil
}
func (p *materialPlatform) CreateWikiDocument(_ context.Context, parent workprovision.WikiParent, _ string) (workprovision.HomeDocument, error) {
	if parent.SpaceID != "existing-space" {
		panic("wrong wiki space")
	}
	p.wikis++
	return workprovision.HomeDocument{ID: "team-original", URL: "https://a.feishu.cn/wiki/shared"}, nil
}
func (p *materialPlatform) HasGroupView(_ context.Context, _ string, chat string) (bool, error) {
	return p.grants[chat], nil
}
func (p *materialPlatform) EnsureGroupView(_ context.Context, _ string, chat string) error {
	p.grants[chat] = true
	return nil
}

func fixture(t *testing.T, mode, link string) (*workstore.Store, *Service, *materialPlatform) {
	t.Helper()
	ctx := context.Background()
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	binding := work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "owner", DisplayName: "Owner", Revision: 1, CreatedAt: time.Now()}, 0); e != nil {
			return e
		}
		if e := tx.PutRole(work.Role{ID: "leader", Name: "Leader", Description: "Lead", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(binding, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "bootstrap", Kind: work.ScopePersonal, RoleID: "leader", PrincipalID: "owner", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutTeam(work.Team{ID: "team", Name: "Shared team", LeaderRoleID: "leader", Revision: 1, Materials: &work.TeamMaterials{ScopeID: "bootstrap", Generation: 1, Selection: work.TeamMaterialsSelection{Mode: mode, URL: link}, Identity: binding, Source: work.SourceRef{Kind: "message", ID: "source", Version: "1", EvidenceRef: "body"}, State: "pending"}}, 0); e != nil {
			return e
		}
		if e := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "leader", Active: true, Revision: 1}, 0); e != nil {
			return e
		}
		for _, id := range []string{"one", "two"} {
			if e := tx.PutScope(work.WorkScope{ID: "scope-" + id, Kind: work.ScopeProject, TeamID: "team", ProjectID: id, Revision: 1}, 0); e != nil {
				return e
			}
			if e := tx.PutProject(work.Project{ID: id, ScopeID: "scope-" + id, TeamID: "team", Name: id, LeaderRoleID: "leader", State: work.ProjectActive, Revision: 1}, 0); e != nil {
				return e
			}
			if e := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: id, Tenant: "tenant", ChatID: "chat-" + id, Purpose: "main", BindingGeneration: 1, Active: true}, 0); e != nil {
				return e
			}
			team, e := tx.GetTeam("team")
			if e != nil {
				return e
			}
			if _, e = tx.ShareTeamMaterials("team", team.Revision, work.ProjectChannelBinding{ProjectID: id, Tenant: "tenant", ChatID: "chat-" + id, Purpose: "main", BindingGeneration: 1, Active: true}, team.Materials.Source); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &materialPlatform{grants: map[string]bool{}}
	svc := New(store, func(context.Context, work.RoleBotBinding) (Platform, error) { return p, nil })
	return store, svc, p
}
func getTeam(t *testing.T, store *workstore.Store) work.Team {
	t.Helper()
	var team work.Team
	if e := store.Within(context.Background(), func(tx *workstore.Tx) error { var e error; team, e = tx.GetTeam("team"); return e }); e != nil {
		t.Fatal(e)
	}
	return team
}
func TestSharedHomeSurvivesProjectArchiveAndRestart(t *testing.T) {
	store, svc, p := fixture(t, "managed", "")
	ctx := context.Background()
	if e := svc.Run(ctx); e != nil {
		t.Fatal(e)
	}
	first := getTeam(t, store)
	if first.Materials.State != "ready" || !p.grants["chat-one"] || !p.grants["chat-two"] {
		t.Fatalf("shared home not reachable: %+v grants=%v", first.Materials, p.grants)
	}
	if e := store.Within(ctx, func(tx *workstore.Tx) error {
		project, e := tx.GetProject("one")
		if e != nil {
			return e
		}
		project.State = work.ProjectArchiving
		project.Revision++
		if e = tx.PutProject(project, project.Revision-1); e != nil {
			return e
		}
		project.State = work.ProjectArchived
		project.Revision++
		return tx.PutProject(project, project.Revision-1)
	}); e != nil {
		t.Fatal(e)
	}
	restarted := New(store, svc.resolve)
	if e := restarted.Run(ctx); e != nil {
		t.Fatal(e)
	}
	if home := getTeam(t, store).Materials; home.DocumentID != first.Materials.DocumentID || home.URL != first.Materials.URL || p.folders != 1 || p.documents != 1 || p.intros != 1 {
		t.Fatalf("team original duplicated/lost: %+v calls=%d/%d/%d", home, p.folders, p.documents, p.intros)
	}
}
func TestUnknownCreateCannotReplayAndExplicitOriginalRecovers(t *testing.T) {
	store, svc, p := fixture(t, "managed", "")
	p.createError = errors.New("response lost")
	ctx := context.Background()
	if e := svc.Run(ctx); e == nil {
		t.Fatal("expected unknown create")
	}
	if e := New(store, svc.resolve).Run(ctx); e == nil {
		t.Fatal("restarted unknown create must remain unresolved")
	}
	if p.documents != 1 {
		t.Fatal("duplicated unknown create")
	}
	old := getTeam(t, store)
	if e := store.Within(ctx, func(tx *workstore.Tx) error {
		_, e := tx.SelectTeamMaterials(old.ID, old.Revision, old.Materials.ScopeID, work.TeamMaterialsSelection{Mode: "existing", URL: "https://a.feishu.cn/docx/selected-original"}, old.Materials.Identity, old.Materials.Source)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := svc.Run(ctx); e != nil {
		t.Fatal(e)
	}
	if home := getTeam(t, store).Materials; home.State != "ready" || home.DocumentID != "selected-original" || p.documents != 1 || p.intros != 0 {
		t.Fatalf("bad recovery: %+v", home)
	}
	if e := store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(key("team", "1", "document"))
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatalf("original write falsely confirmed: %+v", op)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestSelectedParentsAndExistingHomeNeverCreateSpaceOrCopy(t *testing.T) {
	for _, tc := range []struct{ mode, url string }{{"drive", "https://a.feishu.cn/drive/folder/selected"}, {"wiki", "https://a.feishu.cn/wiki/selected"}, {"existing", "https://a.feishu.cn/docx/selected"}} {
		t.Run(tc.mode, func(t *testing.T) {
			_, svc, p := fixture(t, tc.mode, tc.url)
			if e := svc.Run(context.Background()); e != nil {
				t.Fatal(e)
			}
			if p.folders != 0 {
				t.Fatal("ignored selected parent")
			}
			if tc.mode == "wiki" && (p.wikis != 1 || p.documents != 0) {
				t.Fatal("wiki original not created under selected parent")
			}
			if tc.mode == "existing" && (p.documents != 0 || p.intros != 0) {
				t.Fatal("existing original overwritten")
			}
		})
	}
}
func TestLateCreationCannotOverwriteExplicitReplacement(t *testing.T) {
	store, svc, p := fixture(t, "managed", "")
	p.started = make(chan struct{})
	p.release = make(chan struct{})
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("create not reached")
	}
	old := getTeam(t, store)
	if e := store.Within(ctx, func(tx *workstore.Tx) error {
		_, e := tx.SelectTeamMaterials(old.ID, old.Revision, old.Materials.ScopeID, work.TeamMaterialsSelection{Mode: "existing", URL: "https://a.feishu.cn/docx/selected-original"}, old.Materials.Identity, old.Materials.Source)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	close(p.release)
	if e := <-done; e == nil {
		t.Fatal("obsolete generation must fail before further effects")
	}
	if p.intros != 0 {
		t.Fatal("late old generation wrote intro")
	}
	if e := svc.Run(ctx); e != nil {
		t.Fatal(e)
	}
	if home := getTeam(t, store).Materials; home.DocumentID != "selected-original" {
		t.Fatal("late receipt replaced chosen original")
	}
	if e := store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(key("team", "1", "document"))
		if e != nil {
			return e
		}
		if op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
			t.Fatal("lost late receipt")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
