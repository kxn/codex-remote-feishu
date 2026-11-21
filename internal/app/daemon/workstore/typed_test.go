package workstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"sync"
	"testing"
	"time"
)

func seedPersonal(t *testing.T, s *Store) {
	t.Helper()
	err := s.Within(context.Background(), func(tx *Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "Person", Revision: 1}, 0); err != nil {
			return err
		}
		for _, id := range []string{"r", "expert"} {
			if err := tx.PutRole(work.Role{ID: id, Name: id, Description: "responsibility", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
				return err
			}
		}
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "r", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "s", Kind: work.ScopeProject, ProjectID: "project", TeamID: "team", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProject(work.Project{ID: "project", Name: "Project", ScopeID: "s", TeamID: "team", LeaderRoleID: "r", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return err
		}
		for _, id := range []string{"r", "expert"} {
			if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: id, Active: true, Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutProjectMember(work.ProjectMember{ProjectID: "project", RoleID: id, State: work.ProjectMemberReady, Revision: 1}, 0); err != nil {
				return err
			}
		}
		for _, id := range []string{"r", "expert"} {
			if err := tx.PutContext(work.WorkContext{ID: "c-" + id, ScopeID: "s", RoleID: id, Workstream: "main", Revision: 1}, 0); err != nil {
				return err
			}
		}
		for _, id := range []string{"body", "outcome:1", "canonical-body", "conflicting-expanded-body"} {
			if err := tx.PutContent(work.NewContent(id, "s", "text/plain", []byte(id), time.Now().UTC())); err != nil {
				return err
			}
		}
		return tx.PutWorker(work.WorkerSession{ID: "w", ContextID: "c-r", ScopeID: "s", RoleID: "r", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func testTask(id, role string) work.Task {
	return work.Task{ID: id, ScopeID: "s", Title: id, OwnerRoleID: role, RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}
}
func testInput(id, role, context string) work.Inbox {
	return work.Inbox{ID: id, ScopeID: "s", RecipientRoleID: role, RecipientContextID: context, EventID: "event:" + id, State: work.InboxPending, Revision: 1}
}
func testOperation(id string) work.Operation {
	return work.Operation{ID: id, ScopeID: "s", Actor: work.Actor{Kind: work.ActorRole, ID: "r"}, ContextID: "c-r", Kind: "start", TargetID: "w", TargetGeneration: 1, IdempotencyKey: id, RequestHash: "hash:" + id, State: work.OperationPrepared, Revision: 1}
}
func testClaim() ClaimRequest {
	return ClaimRequest{Attempt: work.ExecutionAttempt{ID: "attempt", ContextID: "c-r", ScopeID: "s", RunRef: work.RunRef{WorkerID: "w", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{"input"}, CommandID: "command", Revision: 1}, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: testOperation("command"), Now: time.Now().UTC()}
}
func evidenceSource() work.SourceRef {
	return work.SourceRef{Kind: "runtime", ID: "receipt", Version: "1", EvidenceRef: "journal:1"}
}
func testOutcome(claim ClaimRequest) OutcomeRequest {
	return OutcomeRequest{AttemptID: "attempt", RunRef: claim.Attempt.RunRef, ExpectedAttemptRevision: 2, ProcessedInputIDs: []string{"input"}, OutcomeRef: "outcome:1", Checkpoint: "finished", TerminalState: work.AttemptCompleted, TerminalEvidence: []work.SourceRef{evidenceSource()}, Quiescent: true, Now: time.Now().UTC()}
}
func requireTx(t *testing.T, s *Store, fn func(*Tx) error) {
	t.Helper()
	if err := s.Within(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func TestTypedCASAcrossHandlesAndReopen(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, handle := range []*Store{s, second} {
		wg.Add(1)
		go func(i int, handle *Store) {
			defer wg.Done()
			results <- handle.Within(context.Background(), func(tx *Tx) error {
				return tx.PutPrincipal(work.Principal{ID: "p", DisplayName: fmt.Sprint(i), Revision: 2}, 1)
			})
		}(i, handle)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS winners=%d conflicts=%d", success, conflict)
	}
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "overwrite", Revision: 1}, 0); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate create: %v", err)
		}
		return nil
	})
	third, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	requireTx(t, third, func(tx *Tx) error {
		v, err := tx.GetPrincipal("p")
		if err != nil {
			return err
		}
		if v.Revision != 2 {
			t.Fatalf("reopen revision=%d", v.Revision)
		}
		return nil
	})
}
func TestMessageMultiBotDedupAndAtomicRollback(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	message := work.Message{ID: "msg-app-a", ScopeID: "s", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "chat", MessageID: "platform-msg"}, BodyRef: "body", Observations: []work.Observation{{AppID: "app-a", IdentityGeneration: 1}}, Revision: 1}
	requireTx(t, s, func(tx *Tx) error {
		_, created, err := tx.AcceptMessage(message, []work.Inbox{testInput("in-a", "r", "")})
		if !created {
			t.Fatal("first message replayed")
		}
		return err
	})
	message.ID = "msg-app-b"
	message.Observations = []work.Observation{{AppID: "app-b", IdentityGeneration: 2}}
	requireTx(t, s, func(tx *Tx) error {
		canonical, created, err := tx.AcceptMessage(message, []work.Inbox{testInput("in-b", "r", "")})
		if err != nil {
			return err
		}
		if created || canonical.ID != "msg-app-a" || len(canonical.Observations) != 2 {
			t.Fatalf("bad replay %+v created=%v", canonical, created)
		}
		inputs, err := tx.ListInbox("s")
		if len(inputs) != 1 || inputs[0].RecipientContextID != "" {
			t.Fatalf("duplicate or prematurely assigned inbox %+v", inputs)
		}
		return err
	})
	message.ID = "rejected"
	message.PlatformKey.MessageID = "other"
	requireTx(t, s, func(tx *Tx) error {
		_, _, err := tx.AcceptMessage(message, []work.Inbox{testInput("invalid", "missing", "")})
		if err == nil {
			t.Fatal("invalid recipient accepted")
		}
		if _, err = tx.GetMessage("rejected"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("failed acceptance leaked: %v", err)
		}
		return nil
	})
}
func TestOperationReplayNamespace(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		v := testOperation("op")
		if _, created, err := tx.PutOperation(v, 0); err != nil || !created {
			t.Fatalf("create %v %v", created, err)
		}
		v.ID = "replay"
		old, created, err := tx.PutOperation(v, 0)
		if err != nil || created || old.ID != "op" {
			t.Fatalf("replay %+v %v %v", old, created, err)
		}
		v.RequestHash = "changed"
		if _, _, err = tx.PutOperation(v, 0); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("body conflict %v", err)
		}
		v.RequestHash = "hash:op"
		v.Actor.ID = "expert"
		v.ID = "other-actor"
		_, created, err = tx.PutOperation(v, 0)
		if err != nil || !created {
			t.Fatalf("actor namespace %v %v", created, err)
		}
		return nil
	})
}

func markRunning(t *testing.T, s *Store) {
	t.Helper()
	requireTx(t, s, func(tx *Tx) error {
		v, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		v.State = work.AttemptRunning
		v.Revision++
		return tx.PutAttempt(v, 1)
	})
}
