package workruntime

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func beginNativeAdmission(t *testing.T, f *fixture) agentproto.Command {
	t.Helper()
	command := f.command
	command.CommandID = "steer-command"
	command.Kind = agentproto.CommandTurnSteer
	command.Target.TurnID = "automatic-turn"
	command.NativeInputAdmission = &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "admission", InputIDs: []string{"late-input"}}
	f.tx(func(tx *workstore.Tx) error {
		scope, err := tx.GetScope("scope")
		if err != nil {
			return err
		}
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "steer-event", Tenant: "tenant", ChatID: "private-chat", ChatType: "p2p", MessageID: "steer-human-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "review-human"}, MessageType: "text", RawContent: `{"text":"/steerall"}`, ReceivedAt: f.now}
		ingress, _, err := tx.AcceptRawIngress(raw, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if err != nil {
			return err
		}
		if err = tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "steer-human-command", IngressID: ingress.ID, ScopeID: "scope", ContextID: "review-source", PrincipalID: scope.PrincipalID, RoleID: "role", GatewayRef: "gateway", Text: "/steerall", State: "sending", Revision: 1}, 0); err != nil {
			return err
		}
		if err = tx.PutContent(work.NewContent("late-body", "scope", "text/plain", []byte("new authorized input"), f.now)); err != nil {
			return err
		}
		_, _, err = tx.AcceptMessage(work.Message{ID: "late-message", ScopeID: "scope", ConversationID: "review-source", Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, BodyRef: "late-body", Revision: 1, CreatedAt: f.now}, []work.Inbox{{ID: "late-input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "review-source", MessageID: "late-message", EventID: "late-human-input", State: work.InboxPending, Revision: 1}})
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker(command.Execution.WorkerSessionID)
		if err != nil {
			return err
		}
		_, _, err = tx.BeginAttemptInputAdmission(workstore.BeginAttemptInputAdmissionRequest{ID: "admission", AttemptID: command.Execution.AttemptID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: worker.Generation, RunID: command.Execution.RunRef}, ExpectedWorkerRevision: worker.Revision, InputIDs: []string{"late-input"}, Command: command, Source: workstore.PrivateActionSource{PersonalScopeID: "scope", CommandID: "steer-human-command"}, Now: f.now})
		return err
	})
	if err := f.coordinator.RegisterCommand(context.Background(), f.carrier.Incarnation, command); err != nil {
		t.Fatal(err)
	}
	return command
}
func TestNativeInputAdmissionCommitsOnlyExactRPCAndRetainsUnknown(t *testing.T) {
	for _, status := range []string{"accepted", "failed", "unknown"} {
		t.Run(status, func(t *testing.T) {
			f := automaticFixture(t)
			command := beginNativeAdmission(t, f)
			event := agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: "steer-native-rpc", ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Status: status, Execution: command.Execution}
			if status != "accepted" {
				event.Kind = agentproto.EventNativeTurnInputRejected
				event.ErrorMessage = "native steer result"
			}
			if err := f.handle(event); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				ids, err := tx.EffectiveAttemptInputIDs(command.Execution.AttemptID)
				if err != nil {
					return err
				}
				if (len(ids) == 2) != (status == "accepted") {
					t.Fatalf("effective input IDs: %v", ids)
				}
				input, err := tx.GetInbox("late-input")
				if err != nil {
					return err
				}
				expected := work.InboxClaimed
				if status == "failed" {
					expected = work.InboxPending
				}
				if input.State != expected {
					t.Fatalf("input state=%s", input.State)
				}
				op, err := tx.GetOperation(command.CommandID)
				if err != nil {
					return err
				}
				expectedOp := work.OperationConfirmed
				if status == "failed" {
					expectedOp = work.OperationTerminal
				}
				if status == "unknown" {
					expectedOp = work.OperationUnknown
				}
				if op.State != expectedOp {
					t.Fatalf("op state=%s", op.State)
				}
				return nil
			})
		})
	}
}

func TestUnknownSteerSettlesOnlyAfterThreeDomainStopAndKeepsRecoveryGate(t *testing.T) {
	f := automaticFixture(t)
	command := beginNativeAdmission(t, f)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventNativeTurnInputRejected, CommandID: command.CommandID, RequestID: "native-rpc", ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Status: "unknown", Execution: command.Execution}); err != nil {
		t.Fatal(err)
	}
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		o := stoppedObservation(request)
		o.BackgroundWorkStopped = false
		return o, nil
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime}); err == nil {
		t.Fatal("uncertain background admitted recovery")
	}
	f.tx(func(tx *workstore.Tx) error {
		input, e := tx.GetInbox("late-input")
		if e != nil {
			return e
		}
		if input.State != work.InboxClaimed {
			t.Fatal("input freed before actual stop")
		}
		return nil
	})
	f.driver.inspect = nil
	if err := f.coordinator.Reconcile(context.Background(), command.Execution.AttemptID); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		input, e := tx.GetInbox("late-input")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			t.Fatal("uncertain input was consumed")
		}
		admission, e := tx.GetAttemptInputAdmissionByCommand(command.CommandID)
		if e != nil {
			return e
		}
		if admission.State != work.InputAdmissionQuiescedUnknown {
			t.Fatalf("admission=%s", admission.State)
		}
		gate, e := tx.GetRuntimeGate(command.Execution.WorkerSessionID)
		if e != nil {
			return e
		}
		if gate.RecoveryReason != "native_input_delivery_unknown" {
			t.Fatalf("gate=%+v", gate)
		}
		_, e = tx.GetQuiescenceReceipt(command.Execution.AttemptID)
		return e
	})
	// Late RPC evidence remains audit, never claims the now-pending input again.
	if err := f.handle(agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: "native-rpc-late", ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Status: "accepted", Execution: command.Execution}); err != nil {
		t.Fatal(err)
	}
}
