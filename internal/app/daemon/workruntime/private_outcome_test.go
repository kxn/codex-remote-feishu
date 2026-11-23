package workruntime

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func privateFinalEvent(f *fixture, text string) {
	f.t.Helper()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "session", TurnID: "turn-1", ItemKind: "agent_message", ItemID: "private-final", Metadata: map[string]any{"text": text, "phase": "final_answer"}}); err != nil {
		f.t.Fatal(err)
	}
}
func completePrivateTurn(f *fixture) error {
	return f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"})
}

func TestPrivatePlainFinalSettlesInputAndAllowsNextRequest(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.start()
	privateFinalEvent(f, "**Actual private answer**\n\nThe requested work is ready.")
	if err := completePrivateTurn(f); err != nil {
		t.Fatal(err)
	}
	assertPrivateSucceeded(t, f, 1)
	sdk := startPrivateSDK(t, false)
	publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	drainPrivatePublisher(t, publisher)
	requests := sdk.requests()
	if len(requests) != 1 || !strings.Contains(requests[0].body, "Actual private answer") {
		t.Fatalf("plain final did not reach the actual SDK: %+v", requests)
	}
	f.tx(func(tx *workstore.Tx) error {
		scope, err := tx.GetScope("scope")
		if err != nil {
			return err
		}
		body := work.NewContent("second-human-body", scope.ID, "text/plain", []byte("a second ordinary request"), f.now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: "second-human-message", ScopeID: scope.ID, ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "private-chat", MessageID: "om_second_user"}, Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: f.now}}, BodyRef: body.ID, Revision: 1, CreatedAt: f.now}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{{ID: "second-input", ScopeID: scope.ID, RecipientContextID: "context", RecipientRoleID: "role", EventID: "second-human-event", MessageID: message.ID, State: work.InboxPending, Revision: 1}}); err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		command := f.command
		command.CommandID = "second-command"
		binding := *command.Execution
		binding.AttemptID, binding.RunRef = "second-attempt", "second-run"
		command.Execution = &binding
		operation, err := f.operation(tx, command)
		if err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: binding.AttemptID, ScopeID: scope.ID, ContextID: "context", RunRef: work.RunRef{WorkerID: worker.ID, Generation: worker.Generation, RunID: binding.RunRef}, InputIDs: []string{"second-input"}, CommandID: command.CommandID, State: work.AttemptDispatchPending, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: worker.ControllerGeneration, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration, StartOperation: operation, Now: f.now})
	})
}

func assertPrivateSucceeded(t *testing.T, f *fixture, inputs int) {
	t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptCompleted {
			t.Fatalf("plain private final did not settle: %+v", attempt)
		}
		report, err := tx.GetProcessingReport(attempt.ID)
		if err != nil {
			return err
		}
		if len(report.ProcessedInputIDs) != inputs || report.WorkDisposition != "unknown" {
			t.Fatalf("wrong exact accepted inputs: %+v", report)
		}
		for _, id := range report.ProcessedInputIDs {
			input, err := tx.GetInbox(id)
			if err != nil {
				return err
			}
			if input.State != work.InboxProcessed {
				t.Fatalf("input remains runnable: %+v", input)
			}
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, attempt.ID)
		if err != nil {
			return err
		}
		if occupied {
			t.Fatal("fully stopped private answer retained occupancy")
		}
		if gate, err := tx.GetRuntimeGate("worker"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatalf("plain answer installed a recovery gate: %+v %v", gate, err)
		}
		return nil
	})
}

func TestPrivatePlainFinalWaitsForUnknownEffectsAndBackground(t *testing.T) {
	for _, kind := range []string{"background", "effect"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.start()
			if kind == "background" {
				f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
					observation := stoppedObservation(request)
					observation.BackgroundWorkStopped = false
					return observation, nil
				}
			} else {
				f.tx(func(tx *workstore.Tx) error {
					command := f.command
					command.CommandID = "unknown-business-effect"
					command.Kind = agentproto.CommandKind("resource.write")
					operation, err := f.operation(tx, command)
					if err != nil {
						return err
					}
					operation.State = work.OperationUnknown
					_, _, err = tx.PutOperation(operation, 0)
					return err
				})
			}
			privateFinalEvent(f, "actual final while independent effects remain unproven")
			if err := completePrivateTurn(f); err == nil {
				t.Fatal("private final bypassed unresolved effects")
			}
			f.tx(func(tx *workstore.Tx) error {
				if _, err := tx.GetProcessingReport("attempt"); !errors.Is(err, workstore.ErrNotFound) {
					t.Fatalf("unsafe private result claimed processing: %v", err)
				}
				input, err := tx.GetInbox("input")
				if err != nil {
					return err
				}
				if input.State != work.InboxClaimed {
					t.Fatalf("unknown work was released or consumed: %+v", input)
				}
				occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
				if err != nil {
					return err
				}
				if !occupied {
					t.Fatal("unknown effects lost occupancy")
				}
				return nil
			})
			f.driver.inspect = nil
			if kind == "effect" {
				f.tx(func(tx *workstore.Tx) error {
					operation, err := tx.GetOperation("unknown-business-effect")
					if err != nil {
						return err
					}
					before := operation.Revision
					operation.Revision++
					operation.State = work.OperationConfirmed
					operation.ExternalReceipt = "verified-business-effect"
					_, _, err = tx.PutOperation(operation, before)
					return err
				})
			}
			if err := f.coordinator.Reconcile(t.Context(), "attempt"); err != nil {
				t.Fatal(err)
			}
			assertPrivateSucceeded(t, f, 1)
		})
	}
}

func TestPrivateSuccessFallbackExcludesNonhumanAndControl(t *testing.T) {
	for _, kind := range []string{"nonhuman", "control"} {
		t.Run(kind, func(t *testing.T) {
			private := kind != "nonhuman"
			f := newFixture(t, work.BackendCodex, false, private, kind == "control")
			f.start()
			privateFinalEvent(f, "not a normal completed private response")
			if err := completePrivateTurn(f); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				if _, err := tx.GetProcessingReport("attempt"); !errors.Is(err, workstore.ErrNotFound) {
					t.Fatalf("ineligible result acquired a synthetic success report: %v", err)
				}
				input, err := tx.GetInbox("input")
				if err != nil {
					return err
				}
				if input.State == work.InboxProcessed {
					t.Fatalf("ineligible input silently consumed: %+v", input)
				}
				gate, err := tx.GetRuntimeGate("worker")
				if err != nil {
					return err
				}
				if gate.Reason != "missing_processing_report" {
					t.Fatalf("existing business evidence gate was bypassed: %+v", gate)
				}
				return nil
			})
		})
	}
}

func TestProjectScopeCannotUsePrivateSuccessFallback(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "role", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "project-scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "project", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutProject(work.Project{ID: "project", Name: "Project", ScopeID: "project-scope", TeamID: "team", LeaderRoleID: "role", State: work.ProjectActive, Revision: 1}, 0); err != nil {
			return err
		}
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		attempt.ScopeID = "project-scope"
		ids, err := privateOutcomeHumanInputs(tx, attempt, false)
		if err != nil {
			return err
		}
		if len(ids) != 0 {
			t.Fatal("project inputs bypassed explicit business reports")
		}
		return nil
	})
}

func TestPrivateImageOnlyAndEmptyCompletedTurnsSettleAsConversationOutcomes(t *testing.T) {
	for _, kind := range []string{"image", "empty"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.start()
			if kind == "image" {
				if err := f.handle(agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "session", TurnID: "turn-1", ItemKind: "image_generation", ItemID: "image-only", Status: "completed", Metadata: map[string]any{"imageBase64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADElEQVR42mP8z8BQDwAFhQIAW7+nMgAAAABJRU5ErkJggg=="}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := completePrivateTurn(f); err != nil {
				t.Fatal(err)
			}
			assertPrivateSucceeded(t, f, 1)
			sdk := startPrivateSDK(t, false)
			publisher, err := workpublish.New(f.store, privateSDKResolver{sdk.api}, func() time.Time { return f.now })
			if err != nil {
				t.Fatal(err)
			}
			drainPrivatePublisher(t, publisher)
			found := false
			for _, request := range sdk.requests() {
				if kind == "image" && request.kind == "image" || kind == "empty" && strings.Contains(request.body, "本轮没有返回可显示的答复") {
					found = true
				}
			}
			if !found {
				t.Fatalf("actual private result did not reach SDK: %+v", sdk.requests())
			}
		})
	}
}
