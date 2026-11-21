package workservice

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func reviewBootstrap(t *testing.T) (*Service, string, string) {
	t.Helper()
	service, _, _ := runningService(t)
	now := service.now()
	err := service.store.Within(t.Context(), func(tx *workstore.Tx) error {
		scope := work.WorkScope{ID: "bootstrap-scope", Kind: work.ScopeBootstrap, RoleID: "leader", PrincipalID: "human", Tenant: "tenant", ChatID: "bootstrap-chat", BootstrapOperationID: "bootstrap-create", Revision: 1}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		op := work.Operation{ID: "bootstrap-create", ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, Kind: "project.bootstrap", TargetID: scope.ChatID, TargetGeneration: 1, IdempotencyKey: "bootstrap-create", RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		if _, _, err := tx.PutOperation(op, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "bootstrap-context", ScopeID: scope.ID, RoleID: "leader", Workstream: "establish", Revision: 1}, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "bootstrap-worker", ContextID: "bootstrap-context", ScopeID: scope.ID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "bootstrap-carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		body := work.NewContent("bootstrap-body", scope.ID, "text/plain", []byte("Please establish a new project"), now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: "bootstrap-message", ScopeID: scope.ID, ConversationID: "bootstrap-chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "bootstrap-chat", MessageID: "platform-bootstrap"}, BodyRef: body.ID, Revision: 1}
		input := work.Inbox{ID: "bootstrap-input", ScopeID: scope.ID, RecipientRoleID: "leader", RecipientContextID: "bootstrap-context", EventID: "message:bootstrap-message", State: work.InboxPending, Revision: 1}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{input}); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: "bootstrap-attempt", ScopeID: scope.ID, ContextID: worker.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "bootstrap-run"}, State: work.AttemptDispatchPending, CommandID: "bootstrap-start", InputIDs: []string{input.ID}, Revision: 1}
		start := work.Operation{ID: attempt.CommandID, ScopeID: scope.ID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: start, Now: now}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "bootstrap-scope", ContextID: "bootstrap-context", WorkerID: "bootstrap-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return service, token, "bootstrap-run"
}
func TestReviewBootstrapAccessIsLimitedAndInstructionsExcludePrivateHistory(t *testing.T) {
	service, token, run := reviewBootstrap(t)
	if err := service.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutContent(work.NewContent("private-secret", "scope", "text/plain", []byte("PRIVATE OWNER FACT"), service.now()))
	}); err != nil {
		t.Fatal(err)
	}
	view, err := service.BootstrapContext(t.Context(), token, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Inputs) != 1 || view.Inputs[0].Text != "Please establish a new project" {
		t.Fatalf("wrong bootstrap inputs %+v", view.Inputs)
	}
	if _, err = service.Context(t.Context(), token, run); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("bootstrap entered general collaboration: %v", err)
	}
	if _, err = service.AssignTask(t.Context(), token, run, AssignTaskRequest{Key: "escape", Title: "must not exist", OwnerRoleID: "leader"}); err == nil {
		t.Fatal("bootstrap assigned regular work")
	}
	bundle, err := service.BuildInstructions(t.Context(), "bootstrap-context")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bundle.Text, "PRIVATE OWNER FACT") || strings.Contains(bundle.Text, token) {
		t.Fatal("private fact or credential reached bootstrap instructions")
	}
	if _, err = service.ReadContent(t.Context(), token, run, ContentReadRequest{ID: "private-secret"}); err == nil {
		t.Fatal("bootstrap read another scope's private content")
	}
}
func TestReviewBootstrapCanReadOnlyItsDurableInputContent(t *testing.T) {
	service, token, run := reviewBootstrap(t)
	page, err := service.ReadContent(t.Context(), token, run, ContentReadRequest{ID: "bootstrap-body"})
	if err != nil {
		t.Fatalf("bootstrap cannot use its promised current-scope content reader: %v", err)
	}
	if page.Text != "Please establish a new project" {
		t.Fatalf("wrong bootstrap content %+v", page)
	}
}
func TestReviewOldBootstrapRunCannotPublishOrMutateNewProject(t *testing.T) {
	service, token, run := reviewBootstrap(t)
	if _, err := service.PublishMessage(t.Context(), token, "different-run", PublishMessageRequest{Key: "stale", Body: "must not be sent"}); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("stale run publish: %v", err)
	}
	if _, err := service.ReportOutcome(t.Context(), token, run, ReportOutcomeRequest{Key: "preserve", Summary: "establishment inspected", ProcessedInputIDs: []string{"bootstrap-input"}}); err != nil {
		t.Fatal(err)
	}
}
