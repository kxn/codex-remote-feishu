package workservice

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestTeamMaterialsCLISelectionUsesPlatformAudience(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false, "owner")
	var revision int64
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		message, e := tx.GetMessage("owner")
		if e != nil {
			return e
		}
		if message.ConversationID == message.PlatformKey.ChatID {
			t.Fatal("fixture conflates internal conversation with platform audience")
		}
		team, e := tx.GetTeam("team")
		if e != nil {
			return e
		}
		revision = team.Revision
		bootstrap := work.WorkScope{ID: "owner-bootstrap", Kind: work.ScopeBootstrap, RoleID: "leader", PrincipalID: message.Source.ID, Tenant: "tenant", ChatID: "group", BootstrapOperationID: "owner-bootstrap-op", Revision: 1}
		if e = tx.PutScope(bootstrap, 0); e != nil {
			return e
		}
		if _, _, e = tx.PutOperation(work.Operation{ID: bootstrap.BootstrapOperationID, ScopeID: bootstrap.ID, Actor: message.Source, Kind: "project.bootstrap", TargetID: "group", TargetGeneration: 1, IdempotencyKey: bootstrap.ID, RequestHash: "actual-owner", State: work.OperationPrepared, Revision: 1}, 0); e != nil {
			return e
		}
		body := work.NewContent("owner-bootstrap-body", bootstrap.ID, "text/plain", []byte("Create my team"), s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		message.ID, message.ScopeID, message.BodyRef, message.PlatformKey = "owner-bootstrap-message", bootstrap.ID, body.ID, nil
		if _, _, e = tx.AcceptMessage(message, nil); e != nil {
			return e
		}
		return tx.PutTeamOwnership(work.TeamOwnership{TeamID: team.ID, PrincipalID: message.Source.ID, Source: humanSource(message), UpdatedBy: message.Source, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	req := TeamMaterialsSelectRequest{Key: "select-team-home", TeamID: "team", ExpectedRevision: revision, SourceMessageID: "owner", Selection: work.TeamMaterialsSelection{Mode: "managed"}}
	selected, err := s.TeamMaterialsSelect(t.Context(), token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.TeamMaterialsSelect(t.Context(), token, run, req)
	if err != nil || !replay.Replay || selected.Revision != replay.Revision {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		team, e := tx.GetTeam("team")
		if e != nil {
			return e
		}
		grant, ok := team.Materials.ProjectGrants["project"]
		if !ok || grant.ChatID != "group" || grant.Source.ID != "owner" {
			t.Fatalf("wrong platform audience: %+v", team.Materials)
		}
		scope, e := tx.GetScope("project-scope")
		if e != nil {
			return e
		}
		visible, e := teamViewForScope(tx, team, scope)
		if e != nil {
			return e
		}
		if visible.Materials == nil || visible.Materials.Source.ID != "" || visible.Materials.ScopeID != "" || len(visible.Materials.ProjectGrants) != 0 {
			t.Fatalf("project received private material provenance: %+v", visible.Materials)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
