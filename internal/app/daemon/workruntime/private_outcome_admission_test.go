package workruntime

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func beginAuthenticatedPrivateAdmission(t *testing.T, f *fixture) agentproto.Command {
	t.Helper()
	command := f.command
	command.CommandID = "private-steer-command"
	command.Kind = agentproto.CommandTurnSteer
	command.Target.TurnID = "turn-1"
	command.NativeInputAdmission = &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "private-admission", InputIDs: []string{"late-private-input"}}
	f.tx(func(tx *workstore.Tx) error {
		scope, err := tx.GetScope("scope")
		if err != nil {
			return err
		}
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
		raw := work.RawIngressEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "steer-authority-event", Tenant: "tenant", ChatID: "private-chat", ChatType: "p2p", MessageID: "steer-authority-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "review-human"}, MessageType: "text", RawContent: `{"text":"/steerall"}`, ReceivedAt: f.now}
		ingress, _, err := tx.AcceptRawIngress(raw, gateway)
		if err != nil {
			return err
		}
		if err := tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "private-steer-authority", IngressID: ingress.ID, ScopeID: scope.ID, ContextID: "context", PrincipalID: scope.PrincipalID, RoleID: "role", GatewayRef: "gateway", Text: "/steerall", State: "sending", Revision: 1}, 0); err != nil {
			return err
		}
		body := work.NewContent("late-private-body", scope.ID, "text/plain", []byte("include this additional ordinary input"), f.now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: "late-private-message", ScopeID: scope.ID, ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "private-chat", MessageID: "om_late_user"}, Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: f.now}}, BodyRef: body.ID, Revision: 1, CreatedAt: f.now}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{{ID: "late-private-input", ScopeID: scope.ID, RecipientContextID: "context", RecipientRoleID: "role", EventID: "late-private-event", MessageID: message.ID, State: work.InboxPending, Revision: 1}}); err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		_, _, err = tx.BeginAttemptInputAdmission(workstore.BeginAttemptInputAdmissionRequest{ID: "private-admission", AttemptID: command.Execution.AttemptID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: worker.Generation, RunID: command.Execution.RunRef}, ExpectedWorkerRevision: worker.Revision, InputIDs: []string{"late-private-input"}, Command: command, Source: workstore.PrivateActionSource{PersonalScopeID: scope.ID, CommandID: "private-steer-authority"}, Now: f.now})
		return err
	})
	if err := f.coordinator.RegisterCommand(t.Context(), f.carrier.Incarnation, command); err != nil {
		t.Fatal(err)
	}
	return command
}

func TestPrivateSuccessConsumesOnlyProvenNativeSteers(t *testing.T) {
	for _, status := range []string{"accepted", "unknown"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.start()
			command := beginAuthenticatedPrivateAdmission(t, f)
			event := agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: "private-steer-rpc", ThreadID: "session", TurnID: "turn-1", Status: status, Execution: command.Execution}
			if status == "unknown" {
				event.Kind = agentproto.EventNativeTurnInputRejected
			}
			if err := f.handle(event); err != nil {
				t.Fatal(err)
			}
			privateFinalEvent(f, "answer after the native steer receipt")
			err := completePrivateTurn(f)
			if status == "accepted" {
				if err != nil {
					t.Fatal(err)
				}
				assertPrivateSucceeded(t, f, 2)
			} else {
				if err == nil {
					t.Fatal("unknown native admission was treated as delivered")
				}
				f.tx(func(tx *workstore.Tx) error {
					if _, err := tx.GetProcessingReport("attempt"); !errors.Is(err, workstore.ErrNotFound) {
						t.Fatalf("unknown native steer acquired a processing report: %v", err)
					}
					for _, id := range []string{"input", "late-private-input"} {
						input, err := tx.GetInbox(id)
						if err != nil {
							return err
						}
						if input.State != work.InboxClaimed {
							t.Fatalf("uncertain delivery consumed or released input: %+v", input)
						}
					}
					return nil
				})
			}
		})
	}
}
