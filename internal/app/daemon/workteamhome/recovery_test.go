package workteamhome

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestOriginalAccessLossRequiresExplicitReconnect(t *testing.T) {
	store, svc, p := fixture(t, "managed", "")
	if err := svc.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	delete(p.grants, "chat-one")
	if err := svc.Run(t.Context()); err == nil {
		t.Fatal("lost sharing reported ready")
	}
	if p.grants["chat-one"] {
		t.Fatal("revoked sharing silently restored")
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		team, e := tx.GetTeam("team")
		if e != nil {
			return e
		}
		channel, e := tx.GetProjectChannelBinding("tenant", "chat-one")
		if e != nil {
			return e
		}
		_, e = tx.ShareTeamMaterials(team.ID, team.Revision, channel, work.SourceRef{Kind: "human_interaction", ID: "reconnect", Version: "1", EvidenceRef: "card"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !p.grants["chat-one"] || p.documents != 1 {
		t.Fatal("reconnect failed or copied original")
	}
	p.readError = errors.New("original not readable")
	if err := svc.Run(t.Context()); err == nil {
		t.Fatal("unreadable original reported ready")
	}
	if getTeam(t, store).Materials.State != "unavailable" {
		t.Fatal("connection loss not persisted")
	}
	p.readError = nil
	if err := svc.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if p.documents != 1 || p.intros != 1 {
		t.Fatal("recovery rewrote or copied original")
	}
}

func TestKnownRejectedCreationCanRetryWithoutReplayingUnknown(t *testing.T) {
	store, svc, p := fixture(t, "managed", "")
	p.createError = &feishu.APIError{Code: 99991672, StatusCode: 403, Msg: "permission rejected"}
	if err := svc.Run(t.Context()); err == nil {
		t.Fatal("expected permission error")
	}
	p.createError = nil
	if err := svc.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if p.documents != 2 || p.folders != 1 || getTeam(t, store).Materials.State != "ready" {
		t.Fatal("known rejection could not recover")
	}
}

func TestInactiveTeamIdentityCannotContinueCreation(t *testing.T) {
	store, svc, p := fixture(t, "managed", "")
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		member, e := tx.GetTeamMember("team", "leader")
		if e != nil {
			return e
		}
		member.Active = false
		member.Revision++
		return tx.PutTeamMember(member, member.Revision-1)
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Run(t.Context()); err == nil {
		t.Fatal("inactive bot accepted")
	}
	if p.folders != 0 || p.documents != 0 {
		t.Fatal("inactive identity wrote an original")
	}
}
