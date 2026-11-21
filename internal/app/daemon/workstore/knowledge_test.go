package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func seedOtherProject(t *testing.T, s *Store) {
	t.Helper()
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutScope(work.WorkScope{ID: "s2", Kind: work.ScopeProject, ProjectID: "project2", TeamID: "team", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProject(work.Project{ID: "project2", ScopeID: "s2", TeamID: "team", Name: "Second", LeaderRoleID: "r", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutProjectMember(work.ProjectMember{ProjectID: "project2", RoleID: "r", State: work.ProjectMemberReady, Revision: 1}, 0)
	})
}
func memory(id string, owner work.MemoryScope) work.MemoryEntry {
	return work.MemoryEntry{ID: id, StorageScopeID: "s", Scope: owner, Author: work.Actor{Kind: work.ActorRole, ID: "r"}, Sources: []work.SourceRef{evidenceSource()}, ContentRef: "body", Applicability: "current", State: work.MemoryValid, Revision: 1}
}
func practice(owner work.KnowledgeOwner, version string) work.PracticeDefinition {
	return work.PracticeDefinition{StorageScopeID: "s", Owner: owner, Revision: 1, PracticeRef: work.PracticeRef{ID: "review", Version: version}, Purpose: "review result", Applicability: "changes", Exclusions: "no change", Escalation: "ask owner", ContentRef: "body", Source: evidenceSource()}
}
func TestDurableContentScopeDigestAndReopen(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	seedOtherProject(t, s)
	requireTx(t, s, func(tx *Tx) error {
		v := work.NewContent("private-blob", "s", "application/octet-stream", []byte{0, 1, 2, 255}, time.Now().UTC())
		if err := tx.PutContent(v); err != nil {
			return err
		}
		if _, err := tx.GetContent("s2", v.ID); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("cross-scope content: %v", err)
		}
		v.Data = []byte("corrupt")
		if err := tx.PutContent(v); err == nil {
			t.Fatal("digest mismatch accepted")
		}
		return nil
	})
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		v, err := tx.GetContent("s", "private-blob")
		if err != nil {
			return err
		}
		if len(v.Data) != 4 || v.Data[3] != 255 {
			t.Fatalf("blob changed: %v", v.Data)
		}
		return nil
	})
}
func TestMemoryUsesSemanticOwnershipBeforeReadingBody(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedOtherProject(t, s)
	grants := grantSharing(t, s, []work.SharingTarget{memorySharingTarget(memory("team-note", work.MemoryScope{Kind: work.MemoryTeam, TeamID: "team"})), memorySharingTarget(memory("role-note", work.MemoryScope{Kind: work.MemoryRole, RoleID: "r"}))})
	requireTx(t, s, func(tx *Tx) error {
		for _, v := range []work.MemoryEntry{memory("team-note", work.MemoryScope{Kind: work.MemoryTeam, TeamID: "team"}), memory("project-note", work.MemoryScope{Kind: work.MemoryProject, ProjectID: "project"}), memory("role-note", work.MemoryScope{Kind: work.MemoryRole, RoleID: "r"})} {
			v.SharingGrantID = grants[v.ID]
			if err := tx.PutMemory(v, 0); err != nil {
				return err
			}
		}
		return nil
	})
	read := work.ReadScope{ScopeID: "s2", RoleID: "r"}
	requireTx(t, s, func(tx *Tx) error {
		entry, body, err := tx.ReadMemory(read, "team-note")
		if err != nil {
			return err
		}
		if entry.StorageScopeID != "s" || string(body.Data) != "body" {
			t.Fatal("shared team memory body missing")
		}
		if _, err = tx.GetContent("s2", body.ID); !errors.Is(err, ErrScopeDenied) {
			t.Fatal("raw content widened with memory sharing")
		}
		if _, _, err = tx.ReadMemory(read, "project-note"); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("project memory leaked: %v", err)
		}
		entries, err := tx.ListMemory(read)
		if err != nil {
			return err
		}
		if len(entries) != 2 {
			t.Fatalf("authorized memories=%d", len(entries))
		}
		return nil
	})
	requireTx(t, s, func(tx *Tx) error {
		member, err := tx.GetProjectMember("project2", "r")
		if err != nil {
			return err
		}
		member.State = work.ProjectMemberRemoved
		member.Revision++
		if err = tx.PutProjectMember(member, 1); err != nil {
			return err
		}
		if _, _, err = tx.ReadMemory(read, "team-note"); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("removed member read shared knowledge: %v", err)
		}
		return nil
	})
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.InvalidateMemorySources("s", evidenceSource()); err != nil {
			return err
		}
		if _, _, err := tx.ReadMemory(work.ReadScope{ScopeID: "s", RoleID: "r"}, "team-note"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invalidated memory still retrieved: %v", err)
		}
		return nil
	})
}
func TestPracticeOwnerSharingAndFrozenAdoption(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedOtherProject(t, s)
	owner := work.KnowledgeOwner{Kind: work.KnowledgeTeam, TeamID: "team"}
	requireTx(t, s, func(tx *Tx) error {
		definition := practice(owner, "1")
		if err := tx.InstallBuiltinPractice(definition, []byte("body"), time.Now().UTC()); err != nil {
			return err
		}
		definition.Revision++
		definition.Purpose = "silently different"
		if err := tx.PutPractice(definition, 1); err == nil {
			t.Fatal("published method version changed")
		}
		adoption := work.PracticeAdoption{ID: "adoption", ScopeID: "s2", Owner: owner, Practice: work.PracticeRef{ID: "review", Version: "1"}, AdoptedBy: work.Actor{Kind: work.ActorRole, ID: "r"}, Source: evidenceSource(), Revision: 1}
		if err := tx.PutPracticeAdoption(adoption, 0); err != nil {
			return err
		}
		return tx.InstallBuiltinPractice(practice(owner, "2"), []byte("body"), time.Now().UTC())
	})
	requireTx(t, s, func(tx *Tx) error {
		definition, body, err := tx.GetPracticeFor(work.ReadScope{ScopeID: "s2", RoleID: "r"}, owner, work.PracticeRef{ID: "review", Version: "1"})
		if err != nil {
			return err
		}
		if definition.Version != "1" || string(body.Data) != "body" {
			t.Fatal("wrong frozen method")
		}
		adoption, err := tx.GetPracticeAdoption("s2", "adoption")
		if err != nil {
			return err
		}
		if adoption.Practice.Version != "1" {
			t.Fatal("method upgrade changed running adoption")
		}
		if _, err = tx.GetPracticeAdoption("s", "adoption"); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("adoption leaked: %v", err)
		}
		return nil
	})
}

func grantSharing(t *testing.T, s *Store, targets []work.SharingTarget) map[string]string {
	t.Helper()
	requireTx(t, s, func(tx *Tx) error {
		message := work.Message{ID: "sharing-message", ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, BodyRef: "body", Revision: 1}
		_, _, err := tx.AcceptMessage(message, []work.Inbox{testInput("sharing-input", "r", "c-r")})
		return err
	})
	claim := testClaim()
	claim.Attempt.InputIDs = []string{"sharing-input"}
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	ids := map[string]string{}
	requireTx(t, s, func(tx *Tx) error {
		body, err := tx.GetContent("s", "body")
		if err != nil {
			return err
		}
		for _, target := range targets {
			id := "grant:" + target.ObjectID
			grant := work.SharingGrant{ID: id, SourceScopeID: "s", ContentRef: "body", ContentSHA256: body.SHA256, Target: target, AuthorizedBy: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, AuthorizingRunRef: claim.Attempt.RunRef, SourceInboxID: "sharing-input", SourceMessageID: "sharing-message", State: work.SharingActive, Revision: 1, CreatedAt: time.Now().UTC()}
			if err = tx.PutSharingGrant(grant, 0); err != nil {
				return err
			}
			ids[target.ObjectID] = id
		}
		return nil
	})
	return ids
}
