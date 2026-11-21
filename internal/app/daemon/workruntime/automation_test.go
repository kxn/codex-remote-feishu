package workruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func automaticFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, work.BackendClaude, true)
	owner := f.carrier.Owner
	owner.WorkerSessionID = "automatic-worker"
	owner.ContextID = "review-source"
	carrier := workstore.CarrierRegistration{Incarnation: "automatic-carrier", OriginInstanceID: "automatic-instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.tx(func(tx *workstore.Tx) error {
		scope, err := tx.GetScope("scope")
		if err != nil {
			return err
		}
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "auto-policy-event", Tenant: "tenant", ChatID: "private-chat", ChatType: "p2p", MessageID: "auto-policy-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "review-human"}, MessageType: "text", RawContent: `{"text":"/autocontinue on"}`, ReceivedAt: f.now}
		ingress, _, err := tx.AcceptRawIngress(raw, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if err != nil {
			return err
		}
		if err = tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "automatic-policy", IngressID: ingress.ID, ScopeID: "scope", ContextID: "review-source", PrincipalID: scope.PrincipalID, RoleID: "role", GatewayRef: "gateway", Text: "/autocontinue on", State: "sending", Revision: 1}, 0); err != nil {
			return err
		}
		authority := workstore.PrivateActionSource{PersonalScopeID: "scope", CommandID: "automatic-policy"}
		_, policySource, e := tx.ValidatePrivateAction(authority)
		if e != nil {
			return e
		}
		if err = tx.PutAutomationPolicy(work.AutomationPolicy{ContextID: "review-source", ScopeID: "scope", Kind: work.AutomationContinue, Enabled: true, Source: policySource, Revision: 1, UpdatedAt: f.now}, 0, authority); err != nil {
			return err
		}
		if err = tx.PutContent(work.NewContent("automatic-human-body", "scope", "text/plain", []byte("finish the existing authorized work"), f.now)); err != nil {
			return err
		}
		_, _, err = tx.AcceptMessage(work.Message{ID: "automatic-human-message", ScopeID: "scope", ConversationID: "review-source", Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, BodyRef: "automatic-human-body", Revision: 1, CreatedAt: f.now}, []work.Inbox{{ID: "automatic-input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "review-source", MessageID: "automatic-human-message", EventID: "authorized-work", State: work.InboxPending, Revision: 1}})
		if err != nil {
			return err
		}
		if err = tx.PutLocation(work.ExecutionLocation{ID: "automatic-location", HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutWorker(work.WorkerSession{ID: owner.WorkerSessionID, ContextID: owner.ContextID, ScopeID: "scope", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: carrier.OriginInstanceID, CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationID: "automatic-location", LocationGeneration: 1, Revision: 1}, 0)
	})
	if err := f.coordinator.RegisterCarrier(context.Background(), carrier); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleHello(context.Background(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: carrier.OriginInstanceID, Managed: true, Source: "headless", Backend: agentproto.BackendCodex}, CarrierIncarnation: carrier.Incarnation, WorkerProtocolVersion: 1, JournalProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	binding := owner
	binding.AttemptID = "automatic-attempt"
	binding.RunRef = "automatic-run"
	command := agentproto.Command{CommandID: "automatic-command", Kind: agentproto.CommandPromptSend, InstructionRevision: "instructions-v1", Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "authorized work"}}}, Execution: &binding, Target: agentproto.Target{ThreadID: "automatic-session", CWD: "/workspace"}}
	f.tx(func(tx *workstore.Tx) error {
		raw, _ := json.Marshal(command)
		body := work.NewContent("automatic-command-body", "scope", "application/json", raw, f.now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: command.CommandID, ScopeID: "scope", ContextID: owner.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(command.Kind), TargetID: owner.WorkerSessionID, TargetGeneration: 1, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		worker, err := tx.GetWorker(owner.WorkerSessionID)
		if err != nil {
			return err
		}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: binding.AttemptID, ContextID: owner.ContextID, ScopeID: "scope", RunRef: work.RunRef{WorkerID: owner.WorkerSessionID, Generation: 1, RunID: binding.RunRef}, State: work.AttemptDispatchPending, InputIDs: []string{"automatic-input"}, CommandID: command.CommandID, Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: carrier.OriginInstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: f.now})
	})
	f.carrier, f.command = carrier, command
	if err := f.coordinator.RegisterCommand(context.Background(), carrier.Incarnation, command); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: command.CommandID, ThreadID: "automatic-session", TurnID: "automatic-turn"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAutomaticNativeFailureWaitsForFullProofAndPreservesHumanInputs(t *testing.T) {
	f := automaticFixture(t)
	f.driver.inspect = func(r InspectionRequest) (NativeObservation, error) {
		o := stoppedObservation(r)
		o.BackgroundWorkStopped = false
		return o, nil
	}
	terminal := f.durable(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "automatic-session", TurnID: "automatic-turn", Status: "failed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime, Problem: &agentproto.ErrorInfo{Code: "responseStreamDisconnected", Layer: "codex"}})
	if err := f.coordinator.HandleEvent(context.Background(), f.carrier.OriginInstanceID, terminal); err == nil {
		t.Fatal("unknown background allowed automatic recovery")
	}
	f.tx(func(tx *workstore.Tx) error {
		if _, err := tx.GetProcessingReport("automatic-attempt"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("failure receipt was invented before full quiescence")
		}
		return nil
	})
	f.driver.inspect = nil
	if err := f.coordinator.Reconcile(context.Background(), "automatic-attempt"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		report, err := tx.GetProcessingReport("automatic-attempt")
		if err != nil {
			return err
		}
		if len(report.ProcessedInputIDs) != 0 {
			t.Fatal("retry failure falsely processed human work")
		}
		input, err := tx.GetInbox("automatic-input")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending {
			t.Fatal("automatic retry discarded the original work")
		}
		episode, err := tx.GetAutomationEpisode("automation:autocontinue:automatic-attempt")
		if err != nil {
			return err
		}
		if episode.Count != 1 || episode.ContextID != "review-source" {
			t.Fatal("automatic retry lost bounded source authorization")
		}
		worker, err := tx.GetWorker("automatic-worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerReady {
			t.Fatal("proven bounded recovery permanently gated its carrier")
		}
		return nil
	})
}
