package workstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestReviewPersonalScopeRejectsForeignRoleWork(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		return tx.PutScope(work.WorkScope{ID: "private-r", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0)
	})
	requireTx(t, s, func(tx *Tx) error {
		input := testInput("private-input", "expert", "")
		input.ScopeID = "private-r"
		if err := tx.PutInbox(input, 0); err == nil {
			t.Error("foreign-role inbox accepted in role-private scope although no valid expert context can claim it")
		}
		task := testTask("private-task", "expert")
		task.ScopeID = "private-r"
		if err := tx.PutTask(task, 0); err == nil {
			t.Error("foreign-role task owner accepted in role-private scope although no valid expert worker can run it")
		}
		return nil
	})
}

func TestReviewTeamMemberCASAcrossHandles(t *testing.T) {
	first, path := openTestStore(t)
	seedPersonal(t, first)
	var expectedRevision int64
	requireTx(t, first, func(tx *Tx) error {
		member, err := tx.GetTeamMember("team", "r")
		if errors.Is(err, ErrNotFound) {
			member = work.TeamMember{TeamID: "team", RoleID: "r", Active: true, Revision: 1}
			if err = tx.PutTeamMember(member, 0); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		expectedRevision = member.Revision
		return nil
	})
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, handle := range []*Store{first, second} {
		wg.Add(1)
		go func(handle *Store) {
			defer wg.Done()
			results <- handle.Within(context.Background(), func(tx *Tx) error {
				return tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: "r", Active: false, Revision: expectedRevision + 1}, expectedRevision)
			})
		}(handle)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("member CAS winners=%d conflicts=%d", successes, conflicts)
	}
}

func TestReviewMultiAppConflictingBodyDoesNotMutateCanonical(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		for _, id := range []string{"canonical-body", "conflicting-expanded-body"} {
			if err := tx.PutContent(work.NewContent(id, "s", "text/plain", []byte(id), time.Now().UTC())); err != nil {
				return err
			}
		}
		return nil
	})
	message := work.Message{ID: "original", ScopeID: "s", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "chat", MessageID: "message"}, BodyRef: "canonical-body", Observations: []work.Observation{{AppID: "app-a", IdentityGeneration: 1}}, Revision: 1}
	requireTx(t, s, func(tx *Tx) error {
		_, _, err := tx.AcceptMessage(message, []work.Inbox{testInput("first", "r", "")})
		return err
	})
	message.ID = "app-b-copy"
	message.BodyRef = "conflicting-expanded-body"
	message.Observations = []work.Observation{{AppID: "app-b", IdentityGeneration: 1}}
	requireTx(t, s, func(tx *Tx) error {
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{testInput("second", "expert", "")}); !errors.Is(err, ErrIdempotencyConflict) {
			t.Errorf("body conflict swallowed: %v", err)
		}
		stored, err := tx.GetMessage("original")
		if err != nil {
			return err
		}
		if stored.BodyRef != "canonical-body" || len(stored.Observations) != 1 {
			t.Error("conflicting observation changed canonical message")
		}
		inbox, err := tx.ListInbox("s")
		if err != nil {
			return err
		}
		if len(inbox) != 1 {
			t.Error("failed conflicting observation produced recipient side effect")
		}
		return nil
	})
}
