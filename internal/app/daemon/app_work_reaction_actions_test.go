package daemon

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

type reactionActionFixture struct {
	a      *App
	source teamHumanSource
	relay  *steerOuterRelay
	event  func(agentproto.Event) error
	late   work.Inbox
}

func newReactionActionFixture(t *testing.T, backend work.Backend, media ...bool) *reactionActionFixture {

	a, source := teamPickerFixture(t, isolatedSDKFixtureAppID(t, "reaction-action"))
	return seedReactionActionFixture(t, a, source, backend, media...)
}
func seedReactionActionFixture(t *testing.T, a *App, source teamHumanSource, backend work.Backend, media ...bool) *reactionActionFixture {
	t.Helper()
	ctx := t.Context()
	s := a.teamRuntime.store
	ingress, e := workingress.New(s, workingress.Config{Directory: steerSDKDirectory{source.Gateway}, PrivateCommands: teamPrivateCommands{app: a}, Expander: steerMediaExpander{}})
	if e != nil {
		t.Fatal(e)
	}
	receive := func(id, text string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"text": text})
		if _, e := ingress.Receive(ctx, work.RawIngressEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: id, Tenant: source.Tenant, ChatID: source.ChatID, ChatType: "p2p", MessageID: id, SenderType: "user", SenderIDs: source.SenderIDs, MessageType: "text", RawContent: string(raw), ReceivedAt: time.Now().UTC()}); e != nil {
			t.Fatal(e)
		}
		processed, e := ingress.ProcessPending(ctx, 16)
		if e != nil || len(processed.Failures) > 0 {
			t.Fatalf("actual SDK routing: %+v %v", processed, e)
		}
	}
	receive("original-request", "Original authorized work")
	var front work.WorkContext
	var originalInput work.Inbox
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, e := tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		front, e = tx.GetContext(nav.FrontContextID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListPendingInputs(source.ScopeID, source.RoleID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 {
			return fmt.Errorf("original inputs=%d", len(inputs))
		}
		originalInput = inputs[0]
		return tx.PutLocation(work.ExecutionLocation{ID: "steer-location", HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0)
	})
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: front.ScopeID, ContextID: front.ID, RoleID: front.RoleID, WorkerSessionID: "steer-worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "steer-carrier", OriginInstanceID: "steer-instance", Owner: owner, Backend: backend, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.PutWorker(work.WorkerSession{ID: owner.WorkerSessionID, ScopeID: front.ScopeID, ContextID: front.ID, RoleID: front.RoleID, Backend: backend, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: carrier.OriginInstanceID, CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationID: "steer-location", LocationGeneration: 1, Revision: 1}, 0)
	})
	relay := &steerOuterRelay{}
	driver, e := worktransport.New(s, relay, a.teamRuntime.beginPersonalSourceActivity)
	if e != nil {
		t.Fatal(e)
	}
	a.teamRuntime.driver = driver
	coordinator := workruntime.New(s, workruntime.Options{Driver: driver})
	driver.BindRegistrar(coordinator)
	if e = coordinator.RegisterCarrier(ctx, carrier); e != nil {
		t.Fatal(e)
	}
	if e = coordinator.HandleHello(ctx, agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: carrier.OriginInstanceID, Backend: agentproto.Backend(backend), Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: carrier.Incarnation}); e != nil {
		t.Fatal(e)
	}
	binding := owner
	binding.AttemptID = "steer-attempt"
	binding.RunRef = "steer-run"
	initial := work.ExecutionAttempt{ID: binding.AttemptID, ScopeID: front.ScopeID, ContextID: front.ID, RunRef: work.RunRef{WorkerID: owner.WorkerSessionID, Generation: 1, RunID: binding.RunRef}, InputIDs: []string{originalInput.ID}, CommandID: "initial-command", State: work.AttemptDispatchPending, Revision: 1}
	prompt, e := a.teamRuntime.service.BuildWorkInput(ctx, initial)
	if e != nil {
		t.Fatal(e)
	}
	command := agentproto.Command{CommandID: initial.CommandID, Kind: agentproto.CommandPromptSend, InstructionRevision: "frozen-instructions", Execution: &binding, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: prompt}}}, IssuedAt: time.Now().UTC()}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(owner.WorkerSessionID)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(command)
		body := work.NewContent("initial-native-body", front.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: command.CommandID, ScopeID: front.ScopeID, ContextID: front.ID, Actor: work.Actor{Kind: work.ActorRole, ID: front.RoleID}, Kind: "native.test.prompt", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: initial, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
	if e = coordinator.RegisterCommand(ctx, carrier.Incarnation, command); e != nil {
		t.Fatal(e)
	}
	sequence := uint64(0)
	event := func(v agentproto.Event) error {
		sequence++
		v.Seq = sequence
		hash, e := agentproto.RuntimeEventPayloadHash(v)
		if e != nil {
			return e
		}
		v.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: carrier.Incarnation, Sequence: sequence, PayloadHash: hash}
		return coordinator.HandleEvent(ctx, carrier.OriginInstanceID, v)
	}
	if e = event(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: command.CommandID, RequestID: "native-start-rpc", ThreadID: "steer-session", TurnID: "steer-turn", Execution: &binding}); e != nil {
		t.Fatal(e)
	}
	withMedia := len(media) > 0 && media[0]
	if withMedia {
		for _, id := range []string{"cluster-image-1", "cluster-image-2"} {
			if _, e := ingress.Receive(ctx, work.RawIngressEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: id, Tenant: source.Tenant, ChatID: source.ChatID, ChatType: "p2p", MessageID: id, SenderType: "user", SenderIDs: source.SenderIDs, MessageType: "image", RawContent: `{"image_key":"fixture"}`, ReceivedAt: time.Now().UTC()}); e != nil {
				t.Fatal(e)
			}
		}
		if result, e := ingress.ProcessPending(ctx, 16); e != nil || len(result.Failures) > 0 {
			t.Fatalf("media ingress: %+v %v", result, e)
		}
	}
	receive("late-request", "Late authorized work")
	var late work.Inbox
	pickerTx(t, a, func(tx *workstore.Tx) error {
		inputs, e := tx.ListPendingInputs(front.ScopeID, front.RoleID)
		if e != nil {
			return e
		}
		want := 1
		if withMedia {
			want = 3
		}
		if len(inputs) != want {
			return fmt.Errorf("pending inputs %d", len(inputs))
		}
		for _, input := range inputs {
			if !input.AwaitingText {
				late = input
			}
		}
		return nil
	})
	return &reactionActionFixture{a: a, source: source, relay: relay, event: event, late: late}
}

func (f *reactionActionFixture) reaction(t *testing.T, eventID, emoji string, operator work.SenderIDs, received time.Time) string {
	t.Helper()
	var id string
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		message, e := tx.GetMessage(f.late.MessageID)
		if e != nil {
			return e
		}
		event := feishu.PlatformIngressEvent{GatewayRef: f.source.Gateway.GatewayRef, AppID: f.source.Gateway.AppID, EventID: eventID, Tenant: message.PlatformKey.Tenant, MessageID: message.PlatformKey.MessageID, Kind: "reaction.created", OperatorType: "user", OperatorIDs: operator, Emoji: emoji, OccurredAt: time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC), ReceivedAt: received}
		facts, e := json.Marshal(event)
		if e != nil {
			return e
		}
		id, _, e = tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: f.source.Gateway, EventID: event.EventID, Tenant: event.Tenant, MessageID: event.MessageID, Kind: event.Kind, Facts: facts, ReceivedAt: received})
		if e != nil {
			return e
		}
		return tx.RegisterPrivateReactionAction(id)
	})
	return id
}

func TestPrivateReactionActionUsesExactPendingInputAndNativeReceipt(t *testing.T) {
	for _, emoji := range []string{"ThumbsUp"} {
		for _, outcome := range []string{"accepted", "unknown", "failed"} {
			t.Run(emoji+"/"+outcome, func(t *testing.T) {
				f := newReactionActionFixture(t, work.BackendCodex)
				id := f.reaction(t, "reaction-real-human", emoji, f.source.SenderIDs, time.Now())
				sends := 0
				f.relay.send = func(_ string, command agentproto.Command) error {
					sends++
					expected := agentproto.CommandTurnSteer
					if emoji == "APPLAUSE" {
						expected = agentproto.CommandThreadShellCommand
					}
					if command.Kind != expected || command.NativeInputAdmission == nil || len(command.NativeInputAdmission.InputIDs) != 1 || command.NativeInputAdmission.InputIDs[0] != f.late.ID {
						t.Fatal("reaction admitted different pending input or operation")
					}
					if emoji == "APPLAUSE" && (command.ShellCommand.Command != "" || command.ShellCommand.Payload == "" || !strings.HasPrefix(command.ShellCommand.Payload, "<queued_input_bundle_v1>\n")) {
						t.Fatal("shell reaction bypassed structured private payload")
					}
					pickerTx(t, f.a, func(tx *workstore.Tx) error {
						action, e := tx.GetPrivateReactionAction(id)
						if e != nil {
							return e
						}
						if action.State != "handled" {
							t.Fatal("native send preceded durable action completion")
						}
						return nil
					})
					event := agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: "exact-reaction-native-rpc", ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, Execution: command.Execution, Status: outcome}
					if outcome != "accepted" {
						event.Kind = agentproto.EventNativeTurnInputRejected
					}
					return f.event(event)
				}
				if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
					t.Fatal(e)
				}
				f.reaction(t, "reaction-real-human", emoji, f.source.SenderIDs, time.Now().Add(time.Second))
				if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
					t.Fatal(e)
				}
				if sends != 1 {
					t.Fatalf("reaction/native unknown replayed %d times", sends)
				}
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					receipt, e := tx.GetPrivateReaction("input-reaction:" + f.late.ID)
					if e != nil {
						return e
					}
					desired, _, e := tx.PrivateInputReactionState(receipt)
					if e != nil {
						return e
					}
					want := "THUMBSUP"
					if emoji == "APPLAUSE" {
						want = "APPLAUSE"
					}
					if outcome != "accepted" {
						want = "OneSecond"
					}
					if len(desired) != 1 || desired[0] != want {
						t.Fatalf("native %s reaction %s desired %v", outcome, emoji, desired)
					}
					return nil
				})
			})
		}
	}
}

func TestPrivateReactionActionIgnoresOtherPeopleAndConsumedOrCancelledInput(t *testing.T) {
	for _, kind := range []string{"wrong-user", "cancelled", "unsupported-backend"} {
		t.Run(kind, func(t *testing.T) {
			backend := work.BackendCodex
			if kind == "unsupported-backend" {
				backend = work.BackendOpenCode
			}
			f := newReactionActionFixture(t, backend)
			operator := f.source.SenderIDs
			if kind == "wrong-user" {
				operator = work.SenderIDs{OpenID: "someone-else"}
			}
			if kind == "cancelled" {
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					input, e := tx.GetInbox(f.late.ID)
					if e != nil {
						return e
					}
					previous := input.Revision
					input.Revision++
					input.State = work.InboxCancelled
					return tx.PutInbox(input, previous)
				})
			}
			f.reaction(t, "ignored-reaction", "ThumbsUp", operator, time.Now())
			f.relay.send = func(_ string, _ agentproto.Command) error {
				t.Fatal("invalid reaction caused native effect")
				return nil
			}
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestPrivateReactionActionRejectsNavigationAndBotChangesBeforeDispatch(t *testing.T) {
	for _, change := range []string{"navigation", "bot-identity", "turn-switched"} {
		t.Run(change, func(t *testing.T) {
			f := newReactionActionFixture(t, work.BackendCodex)
			id := f.reaction(t, "pending-user-reaction", "ThumbsUp", f.source.SenderIDs, time.Now())
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				if change == "turn-switched" {
					action, e := tx.GetPrivateReactionAction(id)
					if e != nil {
						return e
					}
					turn, e := tx.GetNativeTurn(action.Incarnation, action.SessionID, action.TurnID)
					if e != nil {
						return e
					}
					turn.TerminalStatus = "completed"
					turn.TerminalSequence = 10
					turn.LastSequence = 10
					if e = tx.PutNativeTurn(turn); e != nil {
						return e
					}
					turn.TurnID = "replacement-turn"
					turn.TerminalStatus = ""
					turn.TerminalSequence = 0
					turn.LastSequence = 11
					return tx.PutNativeTurn(turn)
				}
				if change == "bot-identity" {
					binding, e := tx.GetRoleBotBinding(f.source.RoleID)
					if e != nil {
						return e
					}
					previous := binding.IdentityGeneration
					binding.IdentityGeneration++
					return tx.PutRoleBotBinding(binding, previous)
				}
				nav, e := tx.GetPrivateNavigation(f.source.ScopeID)
				if e != nil {
					return e
				}
				_, e = tx.ChangePrivateNavigation(workstore.ChangePrivateNavigationRequest{Source: f.source.Authority, ExpectedRevision: nav.Revision, Mode: work.PrivatePersonal, FrontContextID: nav.FrontContextID, Now: time.Now()})
				return e
			})
			f.relay.send = func(_ string, _ agentproto.Command) error { t.Fatal("stale reaction caused native effect"); return nil }
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				action, e := tx.GetPrivateReactionAction(id)
				if e != nil {
					return e
				}
				if action.State != "handled" || action.LastError == "" {
					t.Fatal("stale action remained dispatchable")
				}
				input, e := tx.GetInbox(f.late.ID)
				if e != nil {
					return e
				}
				if input.State != work.InboxPending {
					t.Fatal("stale action consumed original input")
				}
				return nil
			})
		})
	}
}
