package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func TestSharedMemoryCorrectionNotifiesOnlyAuthorizedActiveContextsWithoutBody(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	seedOtherProject(t, s)
	entry := memory("shared-correction", work.MemoryScope{Kind: work.MemoryRole, RoleID: "r"})
	grant := grantSharing(t, s, []work.SharingTarget{memorySharingTarget(entry)})
	entry.SharingGrantID = grant[entry.ID]
	requireTx(t, s, func(tx *Tx) error {
		if e := tx.PutContext(work.WorkContext{ID: "second-role-context", ScopeID: "s2", RoleID: "r", Workstream: "second", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutWorker(work.WorkerSession{ID: "second-worker", ContextID: "second-role-context", ScopeID: "s2", RoleID: "r", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "second-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutWorker(work.WorkerSession{ID: "expert-worker", ContextID: "c-expert", ScopeID: "s", RoleID: "expert", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "expert-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutMemory(entry, 0); e != nil {
			return e
		}
		entry.State = work.MemoryInvalidated
		entry.Revision++
		entry.UpdatedAt = time.Now().UTC()
		if e := tx.PutMemory(entry, 1); e != nil {
			return e
		}
		for _, contextID := range []string{"c-r", "second-role-context"} {
			id := "memory-correction:" + entry.ID + ":2:" + contextID
			message, e := tx.GetMessage(id)
			if e != nil {
				return e
			}
			body, e := tx.GetContent(message.ScopeID, message.BodyRef)
			if e != nil {
				return e
			}
			if strings.Contains(string(body.Data), "canonical-body") || strings.Contains(string(body.Data), "sharing instruction") {
				t.Fatal("crossscope notice copied source body")
			}
			if !strings.Contains(string(body.Data), "旧版本 1") || message.Source.Kind != work.ActorSystem {
				t.Fatal("correction did not identify obsolete version as system information")
			}
		}
		if _, e := tx.GetMessage("memory-correction:" + entry.ID + ":2:c-expert"); e != ErrNotFound {
			t.Fatal("private role memory disclosed to unrelated role")
		}
		return nil
	})
	reopened, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		inputs, e := tx.ListInbox("s2")
		if e != nil {
			return e
		}
		count := 0
		for _, input := range inputs {
			if strings.HasPrefix(input.MessageID, "memory-correction:") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("correction not durable exactly once: %d", count)
		}
		return nil
	})
}

func TestMemoryCorrectionCanBeClaimedByTaskWorker(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	entry := memory("task-correction", work.MemoryScope{Kind: work.MemoryProject, ProjectID: "project"})
	requireTx(t, s, func(tx *Tx) error {
		task := testTask("active-task", "r")
		if err := tx.PutTask(task, 0); err != nil {
			return err
		}
		wc := work.WorkContext{ID: "active-context", ScopeID: "s", RoleID: "r", TaskID: task.ID, Workstream: "task:" + task.ID, Revision: 1}
		if err := tx.PutContext(wc, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "task-worker", ContextID: wc.ID, ScopeID: "s", RoleID: "r", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "task-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		if err := tx.PutMemory(entry, 0); err != nil {
			return err
		}
		entry.State = work.MemoryInvalidated
		entry.Revision++
		entry.UpdatedAt = time.Now().UTC()
		if err := tx.PutMemory(entry, 1); err != nil {
			return err
		}
		input, err := tx.GetInbox("memory-correction:task-correction:2:active-context:input")
		if err != nil {
			return err
		}
		claim := testClaim()
		claim.Attempt.TaskID = task.ID
		claim.Attempt.FrozenRequirementsRevision = task.RequirementsRevision
		claim.Attempt.FrozenAssignmentGeneration = task.AssignmentGeneration
		claim.Attempt.ContextID = wc.ID
		claim.Attempt.RunRef.WorkerID = worker.ID
		claim.Attempt.InputIDs = []string{input.ID}
		claim.InstanceID = worker.InstanceID
		claim.StartOperation.ContextID = wc.ID
		claim.StartOperation.TargetID = worker.ID
		return tx.ClaimAttempt(claim)
	})
}
