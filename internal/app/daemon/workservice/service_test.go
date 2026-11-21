package workservice

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func runningService(t *testing.T) (*Service, string, string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store, signer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "leader", Name: "Leader", Description: "Coordinate", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "leader", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: "human", RoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "leader", Workstream: "main", Revision: 1}, 0); err != nil {
			return err
		}
		w := work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(w, 0); err != nil {
			return err
		}
		a := work.ExecutionAttempt{ID: "attempt", ContextID: "context", ScopeID: "scope", RunRef: work.RunRef{WorkerID: w.ID, Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, CommandID: "start", Revision: 1}
		op := work.Operation{ID: "start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: w.ID, TargetGeneration: 1, IdempotencyKey: "start", RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: a, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: w.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: op, Now: now}); err != nil {
			return err
		}
		a.State = work.AttemptRunning
		a.Revision = 2
		return tx.PutAttempt(a, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "scope", ContextID: "context", WorkerID: "worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, token, "run"
}

func TestAssignReplayConflictAndScopeIsolation(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	req := AssignTaskRequest{Key: "same request", Title: "Implement search", OwnerRoleID: "leader"}
	first, err := s.AssignTask(ctx, token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AssignTask(ctx, token, run, req)
	if err != nil || !second.Replay || first.ObjectID != second.ObjectID {
		t.Fatalf("replay=%+v err=%v", second, err)
	}
	req.Title = "Different request"
	if _, err = s.AssignTask(ctx, token, run, req); !errors.Is(err, workstore.ErrIdempotencyConflict) {
		t.Fatalf("want key conflict, got %v", err)
	}
	req.Key = "bad target"
	req.OwnerRoleID = "another role"
	if _, err = s.AssignTask(ctx, token, run, req); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("cross-role personal scope got %v", err)
	}
	if _, err = s.Context(ctx, token, ""); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("missing run ref got %v", err)
	}
	if err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		tasks, err := tx.ListTasks("scope")
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			t.Fatalf("tasks=%d", len(tasks))
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		if len(inputs) != 1 {
			t.Fatalf("inbox=%d", len(inputs))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskRevisionAndDelegationAreAtomic(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	parent, err := s.AssignTask(ctx, token, run, AssignTaskRequest{Key: "parent", Title: "Ship search", OwnerRoleID: "leader"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AssignTask(ctx, token, run, AssignTaskRequest{Key: "child", Title: "Test search", OwnerRoleID: "leader", ParentTaskID: parent.ObjectID, ParentRevision: 99, Condition: work.DependencyResultAvailable})
	if !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("wrong parent revision got %v", err)
	}
	result, err := s.ReviseTask(ctx, token, run, ReviseTaskRequest{Key: "revise", TaskID: parent.ObjectID, ExpectedRevision: 1, Title: "Ship revised search", OwnerRoleID: "leader"})
	if err != nil || result.Revision != 2 {
		t.Fatalf("revise=%+v err=%v", result, err)
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		task, err := tx.GetTask(parent.ObjectID)
		if err != nil {
			return err
		}
		if task.RequirementsRevision != 2 || task.AssignmentGeneration != 1 {
			t.Fatalf("versions=%+v", task)
		}
		tasks, err := tx.ListTasks("scope")
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			t.Fatalf("failed delegation left %d tasks", len(tasks))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
