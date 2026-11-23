package workteamadmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// This is a real management worker, with its own carrier and claimed turn;
// it revises while the task worker's old attempt remains occupied.
func TestReviseTaskArchivedThenResumesNewVersion(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(fmt.Sprintf("stopped_without_report=%t", stopped), func(t *testing.T) { runRevisionJourney(t, stopped) })
	}
}

func runRevisionJourney(t *testing.T, stopped bool) {
	f := newTeamFixture(t, work.BackendCodex, true)
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "manager-context", RoleID: "role", WorkerSessionID: "manager-worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "manager-inc", OriginInstanceID: "manager-instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.tx(t, func(tx *workstore.Tx) error {
		if e := tx.PutContext(work.WorkContext{ID: "manager-context", ScopeID: "scope", RoleID: "role", Workstream: "manager", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutLocation(work.ExecutionLocation{ID: "manager-location", HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutWorker(work.WorkerSession{ID: "manager-worker", ContextID: "manager-context", ScopeID: "scope", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "manager-instance", InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "manager-inc", LocationID: "manager-location", LocationGeneration: 1, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutInbox(work.Inbox{ID: "manager-input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "manager-context", EventID: "manager-event", State: work.InboxPending, Revision: 1}, 0)
	})
	if err := f.coord.RegisterCarrier(t.Context(), carrier); err != nil {
		t.Fatal(err)
	}
	if err := f.coord.HandleHello(t.Context(), agentproto.Hello{Protocol: agentproto.WireProtocol, Instance: agentproto.InstanceHello{InstanceID: "manager-instance", Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: "manager-inc"}); err != nil {
		t.Fatal(err)
	}
	binding := owner
	binding.AttemptID = "manager-attempt"
	binding.RunRef = "manager-run"
	cmd := agentproto.Command{CommandID: "manager-start", Kind: agentproto.CommandPromptSend, Execution: &binding, InstructionRevision: "v1", Target: agentproto.Target{ThreadID: "manager-session", CWD: "/workspace"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "revise task"}}}}
	f.tx(t, func(tx *workstore.Tx) error {
		raw, _ := json.Marshal(cmd)
		hash := sha256.Sum256(raw)
		content := work.NewContent("manager-request", "scope", "application/json", raw, time.Now().UTC())
		if e := tx.PutContent(content); e != nil {
			return e
		}
		worker, e := tx.GetWorker("manager-worker")
		if e != nil {
			return e
		}
		op := work.Operation{ID: "manager-start", ScopeID: "scope", ContextID: "manager-context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(cmd.Kind), TargetID: "manager-worker", TargetGeneration: 1, IdempotencyKey: "manager-start", RequestHash: hex.EncodeToString(hash[:]), RequestRef: content.ID, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "manager-attempt", ContextID: "manager-context", ScopeID: "scope", RunRef: work.RunRef{WorkerID: "manager-worker", Generation: 1, RunID: "manager-run"}, State: work.AttemptDispatchPending, InputIDs: []string{"manager-input"}, CommandID: "manager-start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "manager-instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
	if err := f.coord.RegisterCommand(t.Context(), "manager-inc", cmd); err != nil {
		t.Fatal(err)
	}
	event := agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "manager-start", ThreadID: "manager-session", TurnID: "manager-turn", Seq: 1}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "manager-inc", Sequence: 1, PayloadHash: hash}
	if err = f.coord.HandleEvent(context.Background(), "manager-instance", event); err != nil {
		t.Fatal(err)
	}
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(contextID, workerID string) string {
		token, e := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "role", BotGeneration: 1, ScopeID: "scope", ContextID: contextID, WorkerID: workerID, SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		return token
	}
	managerToken := issue("manager-context", "manager-worker")
	oldToken := issue("context", "worker")
	revision, err := f.lane.service.ReviseTask(t.Context(), managerToken, "manager-run", workservice.ReviseTaskRequest{Key: "real-revision", TaskID: "task", ExpectedRevision: 1, Title: "new requirements", OwnerRoleID: "role"})
	if err != nil || revision.Revision != 2 {
		t.Fatalf("revision %+v: %v", revision, err)
	}
	if _, err = f.lane.service.ReviseTask(t.Context(), oldToken, "run", workservice.ReviseTaskRequest{Key: "stale-write", TaskID: "task", ExpectedRevision: 2, Title: "old worker must not write", OwnerRoleID: "role"}); err == nil {
		t.Fatal("stale task worker could still revise")
	}
	status, expected := "completed", work.AttemptCompleted
	if stopped {
		stops := workcontrol.NewStops(f.store, f.driver)
		if err = stops.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		sent := len(f.driver.sent)
		if sent != 2 {
			t.Fatalf("want exact goal-clear and interrupt, got %+v", f.driver.sent)
		}
		for _, command := range f.driver.sent {
			if command.Execution == nil || command.Execution.AttemptID != "attempt" || command.Target.ThreadID != "session" {
				t.Fatalf("wrong stop binding: %+v", command)
			}
		}
		// A reconstructed control driver must not resend an uncertain stop or free
		// occupancy merely because the prior socket send returned successfully.
		if err = workcontrol.NewStops(f.store, f.driver).RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(f.driver.sent) != sent {
			t.Fatal("restart repeated stop")
		}
		f.tx(t, func(tx *workstore.Tx) error {
			if _, e := tx.GetQuiescenceReceipt("attempt"); e == nil {
				return fmt.Errorf("send forged quiescence")
			}
			worker, e := tx.GetWorker("worker")
			if e == nil && worker.State == work.WorkerReady {
				return fmt.Errorf("worker freed before native evidence")
			}
			return e
		})
		status, expected = "interrupted", work.AttemptInterrupted
	} else if _, err = f.lane.service.ReportOutcome(t.Context(), oldToken, "run", workservice.ReportOutcomeRequest{Key: "archive", ProcessedInputIDs: []string{"initial"}, Summary: "Old-version work archived", Checkpoint: "old checkpoint", WorkDisposition: "complete"}); err != nil {
		t.Fatal(err)
	}
	f.event(t, agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: status, TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime})
	if err = workcontrol.NewStops(f.store, f.driver).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	f.tx(t, func(tx *workstore.Tx) error {
		task, e := tx.GetTask("task")
		if e != nil {
			return e
		}
		if task.RequirementsRevision != 2 {
			return fmt.Errorf("wrong current revision %d", task.RequirementsRevision)
		}
		old, e := tx.GetAttempt("attempt")
		if e != nil {
			return e
		}
		if old.State != expected {
			return fmt.Errorf("old attempt not completed: %s", old.State)
		}
		if _, e = tx.GetQuiescenceReceipt("attempt"); e != nil {
			return fmt.Errorf("missing quiescence: %w", e)
		}
		wc, e := tx.GetContext("context")
		if e != nil {
			return e
		}
		if wc.Checkpoint == "old checkpoint" {
			return fmt.Errorf("stale checkpoint was adopted")
		}
		inputs, e := tx.ListPendingInputs("scope", "role")
		if e != nil {
			return e
		}
		inputIDs := make([]string, 0, len(inputs))
		for _, input := range inputs {
			if input.TaskID == "task" {
				inputIDs = append(inputIDs, input.ID)
			}
		}
		if stopped && len(inputIDs) != 2 {
			return fmt.Errorf("original input/revision notice not both retained: %+v", inputs)
		}
		var nextInput work.Inbox
		for _, input := range inputs {
			if input.TaskID == "task" && input.MessageID != "" {
				message, e := tx.GetMessage(input.MessageID)
				if e != nil {
					return e
				}
				if message.Source.Kind == work.ActorSystem && message.Source.ID == "task-revision" {
					nextInput = input
				}
			}
		}
		if nextInput.ID == "" {
			return fmt.Errorf("revision input missing")
		}
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		newBinding := owner
		newBinding.ContextID = "context"
		newBinding.WorkerSessionID = "worker"
		newBinding.AttemptID = "new-attempt"
		newBinding.RunRef = "new-run"
		next := agentproto.Command{CommandID: "new-start", Kind: agentproto.CommandPromptSend, Execution: &newBinding, InstructionRevision: "v1", Target: agentproto.Target{ThreadID: "session", CWD: "/workspace"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "new requirements"}}}}
		raw, _ := json.Marshal(next)
		sum := sha256.Sum256(raw)
		body := work.NewContent("new-request", "scope", "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: "new-start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(next.Kind), TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "new-start", RequestHash: hex.EncodeToString(sum[:]), RequestRef: body.ID, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "new-attempt", ContextID: "context", ScopeID: "scope", TaskID: "task", FrozenRequirementsRevision: task.RequirementsRevision, FrozenAssignmentGeneration: task.AssignmentGeneration, RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "new-run"}, State: work.AttemptDispatchPending, InputIDs: inputIDs, CommandID: "new-start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
}
