package worktransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workrequests"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type nativeResponseFixture struct {
	*transportFixture
	service    *workrequests.Service
	decision   workrequests.Decision
	request    workstore.NativeRequest
	translator *codex.Translator
	writes     int
	frames     [][]byte
}

func responseIntegrationFixture(t *testing.T, question bool) *nativeResponseFixture {
	t.Helper()
	store, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "requests.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	f := &nativeResponseFixture{transportFixture: &transportFixture{t: t, store: store, relay: &fakeRelay{}}, translator: codex.NewTranslator("instance")}
	f.driver, err = New(store, f.relay)
	if err != nil {
		t.Fatal(err)
	}
	f.coordinator = workruntime.New(store, workruntime.Options{Driver: f.driver, ApplyNativeRequest: workrequests.Apply, EnqueueReconcile: func(string) {}})
	f.driver.BindRegistrar(f.coordinator)
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
	raw := work.HumanInteractionEnvelope{Kind: "card", EventID: "identity-intro", GatewayRef: "gateway", AppID: "app", Tenant: "tenant", ChatID: "chat", MessageID: "intro-message", SenderIDs: work.SenderIDs{OpenID: "human"}, ReceivedAt: time.Now().UTC(), Values: map[string]any{"team_view": "intro"}}
	var principal string
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "carrier-1", OriginInstanceID: "instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.tx(func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Assistant", Description: "Work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		intro, _, e := tx.AcceptHumanInteraction(raw, gateway)
		if e != nil {
			return e
		}
		principal = intro.PrincipalID
		if e = tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: principal, Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutContext(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", Workstream: "main", Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutLocation(work.ExecutionLocation{ID: "location", HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutWorker(work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "context", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", CarrierIncarnation: "carrier-1", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: "location", LocationGeneration: 1, Revision: 1}, 0)
	})
	if err = f.coordinator.RegisterCarrier(t.Context(), carrier); err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.HandleHello(t.Context(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: "carrier-1"}); err != nil {
		t.Fatal(err)
	}
	binding := owner
	binding.AttemptID = "attempt"
	binding.RunRef = "run"
	command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, Execution: &binding, Target: agentproto.Target{ThreadID: "session"}}
	f.tx(func(tx *workstore.Tx) error {
		if e := tx.PutInbox(work.Inbox{ID: "input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "context", EventID: "event", State: work.InboxPending, Revision: 1}, 0); e != nil {
			return e
		}
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		data, _ := json.Marshal(command)
		body := work.NewContent("start-body", "scope", "application/json", data, time.Now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: "start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(command.Kind), TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "start", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "attempt", ScopeID: "scope", ContextID: "context", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{"input"}, CommandID: "start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now()})
	})
	if err = f.driver.SendCommand(t.Context(), "instance", command); err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "start", ThreadID: "session", TurnID: "turn"})); err != nil {
		t.Fatal(err)
	}
	rpc := `{"id":"native-rpc","method":"item/commandExecution/requestApproval","params":{"threadId":"session","turnId":"turn","itemId":"tool","command":"echo fixture","availableDecisions":["accept","decline","cancel"]}}`
	if question {
		rpc = `{"id":"native-rpc","method":"item/tool/requestUserInput","params":{"threadId":"session","turnId":"turn","itemId":"tool","questions":[{"id":"choice","header":"Choice","question":"Which?","options":[{"label":"A"},{"label":"B"}]}]}}`
	}
	observed, err := f.translator.ObserveServer([]byte(rpc))
	if err != nil || len(observed.Events) != 1 {
		t.Fatalf("native request translator: %v %#v", err, observed.Events)
	}
	if err = f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(observed.Events[0])); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		requests, e := tx.ListNativeRequests("scope")
		if e != nil {
			return e
		}
		if len(requests) != 1 {
			t.Fatalf("requests=%d", len(requests))
		}
		f.request = requests[0]
		view := workstore.UIView{ID: "native-view", ScopeID: "scope", ContextID: "context", SurfaceKey: "request:" + f.request.ID, Kind: "native.request", TargetID: f.request.ID, TargetRevision: f.request.Revision, Binding: work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "chat", PrincipalID: principal, ExternalMessageID: "native-card"}
		card, e := workui.Card(view.ID, workui.Panel{Title: "Native question", Buttons: []workui.Button{{Label: "Cancel", Action: "native.response"}}})
		if e != nil {
			return e
		}
		view, e = workui.Queue(tx, view, card)
		if e != nil {
			return e
		}
		publication, e := tx.GetOperation(view.OperationID)
		if e != nil {
			return e
		}
		previous := publication.Revision
		publication.Revision++
		publication.State = work.OperationSending
		if _, _, e = tx.PutOperation(publication, previous); e != nil {
			return e
		}
		previous = publication.Revision
		publication.Revision++
		publication.State = work.OperationConfirmed
		publication.ExternalReceipt = "native-card"
		if _, _, e = tx.PutOperation(publication, previous); e != nil {
			return e
		}

		raw.EventID = "native-human-decision"
		raw.MessageID = "native-card"
		raw.Values = map[string]any{"team_view": view.ID, "action": "native.response"}
		human, _, e := tx.AcceptHumanInteraction(raw, gateway)
		if e != nil {
			return e
		}
		data, _ := json.Marshal(human)
		if e = tx.PutContent(work.NewContent(human.ID, "scope", "application/json", data, time.Now())); e != nil {
			return e
		}
		f.decision = workrequests.Decision{RequestID: f.request.ID, ExpectedRevision: f.request.Revision, PrincipalID: principal, EvidenceRef: human.ID, Response: agentproto.Request{RequestID: f.request.NativeRequestID, Response: map[string]any{"type": "approval", "decision": "cancel"}, InterruptOnDecline: true}}
		if question {
			f.decision.Response = agentproto.Request{RequestID: f.request.NativeRequestID, Response: map[string]any{"answers": map[string]any{"choice": map[string]any{"answers": []any{"A"}}}}}
		}
		data, _ = json.Marshal(f.decision)
		return tx.PutContent(work.NewContent(human.ID+":decision", "scope", "application/json", data, time.Now()))
	})
	f.service, err = workrequests.New(store, f.driver, func(ctx context.Context, r workstore.NativeRequest, d workrequests.Decision) error {
		return store.Within(ctx, func(tx *workstore.Tx) error {
			human, e := tx.GetHumanInteraction(d.EvidenceRef)
			if e != nil {
				return e
			}
			view, e := tx.GetUIView("native-view")
			if e != nil {
				return e
			}
			if human.PrincipalID != d.PrincipalID || d.RequestID != r.ID {
				return workstore.ErrScopeDenied
			}
			if view.TargetID != r.ID || view.PrincipalID != human.PrincipalID {
				return workstore.ErrScopeDenied
			}
			if r.ResponseOperationID != "" {
				op, e := tx.GetOperation(r.ResponseOperationID)
				if e != nil {
					return e
				}
				if op.Actor == (work.Actor{Kind: work.ActorPrincipal, ID: human.PrincipalID}) {
					return nil
				}
			}
			return tx.ValidateViewInteraction(view, human)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	f.relay.send = func(instance string, c agentproto.Command) error {
		f.writes++
		f.tx(func(tx *workstore.Tx) error {
			op, e := tx.GetOperation(c.CommandID)
			if e != nil {
				return e
			}
			native, e := tx.GetNativeCommand(c.CommandID)
			if e != nil {
				return e
			}
			if op.TargetID != "worker" || native.Binding != binding || c.Request.RequestID != f.request.NativeRequestID {
				t.Fatal("response ownership was not registered before actual transport")
			}
			return nil
		})
		var e error
		f.frames, e = f.translator.TranslateCommand(c)
		return e
	}
	return f
}

func TestNativeResponseRealDriverRegistersApprovalCancelAndQuestion(t *testing.T) {
	for _, question := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval-cancel", true: "question-answer"}[question], func(t *testing.T) {
			f := responseIntegrationFixture(t, question)
			id, err := f.service.Respond(t.Context(), f.decision)
			if err != nil {
				t.Fatal(err)
			}
			if f.writes != 1 || len(f.frames) == 0 {
				t.Fatal("accepted native response never reached real transport")
			}
			var wire map[string]any
			if err = json.Unmarshal(f.frames[0], &wire); err != nil {
				t.Fatal(err)
			}
			if wire["id"] != "native-rpc" {
				t.Fatal("response lost original native RPC correlation")
			}
			if !question && wire["result"].(map[string]any)["decision"] != "cancel" {
				t.Fatal("human cancellation was changed")
			}
			if _, err = f.service.Respond(t.Context(), f.decision); err != nil {
				t.Fatal(err)
			}
			if f.writes != 1 {
				t.Fatal("pending response was resent")
			}
			if err = f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventRequestResolved, ThreadID: "session", TurnID: "turn", RequestID: f.request.NativeRequestID})); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(id)
				if e != nil {
					return e
				}
				request, e := tx.GetNativeRequest(f.request.ID)
				if e != nil {
					return e
				}
				if op.State != work.OperationConfirmed || request.State != "resolved" {
					t.Fatal("only real native resolution may complete response")
				}
				return nil
			})
		})
	}
}

func TestNativeResponseRegisteredTransportFailureRemainsUnknown(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	f.relay.send = func(string, agentproto.Command) error {
		f.writes++
		return errors.New("transport lost after possible write")
	}
	id, err := f.service.Respond(t.Context(), f.decision)
	if err == nil {
		t.Fatal("expected transport uncertainty")
	}
	if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Respond(t.Context(), f.decision); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("uncertain registered response was resent")
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatal("unknown transport was mislabeled unsent")
		}
		return nil
	})
}

func (f *nativeResponseFixture) legacyUnregisteredResponse(t *testing.T, changedAnswer bool, retainOriginalView ...bool) string {
	t.Helper()
	raw, _ := json.Marshal([]string{f.request.ID, "response"})
	sum := sha256.Sum256(raw)
	id := "native_request_" + hex.EncodeToString(sum[:20])
	command := agentproto.Command{CommandID: id, Kind: agentproto.CommandRequestRespond, Execution: agentproto.CloneExecutionBinding(&f.request.Binding), Origin: agentproto.Origin{UserID: f.decision.PrincipalID, MessageID: f.decision.EvidenceRef}, Target: agentproto.Target{ThreadID: f.request.NativeSessionID, TurnID: f.request.NativeTurnID}, Request: f.decision.Response}
	if changedAnswer {
		command.Request.Response = map[string]any{"type": "approval", "decision": "accept"}
	}
	f.tx(func(tx *workstore.Tx) error {
		raw, _ := json.Marshal(command)
		body := work.NewContent(id+":body", "scope", "application/json", raw, time.Now())
		if e := tx.PutContent(body); e != nil {
			return e
		}
		_, _, e := tx.PutOperation(work.Operation{ID: id, ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorPrincipal, ID: f.decision.PrincipalID}, Kind: "native.request_response", TargetID: f.request.ID, TargetGeneration: f.request.Binding.SessionGeneration, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		if e != nil {
			return e
		}
		request, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		before := request.Revision
		request.Revision++
		request.State = "response_pending"
		request.ResponseOperationID = id
		return tx.PutNativeRequest(request, before)
	})
	// Reproduce the actual old bug through the real registrar. It must fail
	// before the relay is called, rather than simulating a lost remote receipt.
	err := f.driver.SendCommand(t.Context(), "instance", command)
	var unsent interface{ NativeCommandUnsent() bool }
	if err == nil || !errors.As(err, &unsent) || !unsent.NativeCommandUnsent() || f.writes != 0 {
		t.Fatalf("legacy registration failure was not proven before write: %v", err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationUnknown
		if _, _, e = tx.PutOperation(op, before); e != nil {
			return e
		}
		human, e := tx.GetHumanInteraction(f.decision.EvidenceRef)
		if e != nil {
			return e
		}
		if e = tx.FinishHumanInteraction(human.ID, human.Revision, id); e != nil {
			return e
		}
		if len(retainOriginalView) > 0 && retainOriginalView[0] {
			return nil
		}
		view, e := tx.GetUIView("native-view")
		if e != nil {
			return e
		}
		before = view.Revision
		view.Revision++
		view.Invalidated = true
		return tx.PutUIView(view, before)
	})
	return id
}

func TestNativeResponseRecoversLegacyUnregisteredHumanDecision(t *testing.T) {
	for _, question := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval-cancel", true: "question-answer"}[question], func(t *testing.T) {
			f := responseIntegrationFixture(t, question)
			id := f.legacyUnregisteredResponse(t, false)
			if err := f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
				t.Fatal(err)
			}
			if f.writes != 1 {
				t.Fatalf("saved human decision was not recovered: writes=%d", f.writes)
			}
			f.tx(func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(id)
				if e != nil {
					return e
				}
				native, e := tx.GetNativeCommand(id)
				if e != nil {
					return e
				}
				if op.ID != id || op.TargetID != "worker" || op.Actor.ID != f.decision.PrincipalID || native.CommandID != id || len(op.Steps) != 1 || op.Steps[0].ID != "local_registration_target_repair" {
					t.Fatal("repair changed human/native identity or lost local evidence")
				}
				proof, e := tx.GetContent("scope", op.Steps[0].Receipt)
				if e != nil {
					return e
				}
				if len(proof.Data) == 0 {
					t.Fatal("repair proof missing")
				}
				return nil
			})
			if err := f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
				t.Fatal(err)
			}
			if f.writes != 1 {
				t.Fatal("repair resent a now-registered uncertain response")
			}
			var wire map[string]any
			if err := json.Unmarshal(f.frames[0], &wire); err != nil {
				t.Fatal(err)
			}
			if !question && wire["result"].(map[string]any)["decision"] != "cancel" {
				t.Fatal("saved human cancellation was replaced")
			}
		})
	}
}

func TestNativeResponseLegacyRepairRejectsChangedDecisionAndBot(t *testing.T) {
	for _, scenario := range []string{"changed-answer", "changed-bot"} {
		t.Run(scenario, func(t *testing.T) {
			f := responseIntegrationFixture(t, false)
			id := f.legacyUnregisteredResponse(t, scenario == "changed-answer")
			if scenario == "changed-bot" {
				f.tx(func(tx *workstore.Tx) error {
					return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "different-app", IdentityGeneration: 2}, 1)
				})
			}
			if scenario == "terminal-turn" {
				if err := f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "interrupted"})); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.service.RecoverUnsentResponses(t.Context(), 32); err == nil {
				t.Fatal("obsolete or changed decision was recovered")
			}
			if f.writes != 0 {
				t.Fatal("invalid legacy recovery reached native transport")
			}
			f.tx(func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(id)
				if e != nil {
					return e
				}
				if op.State != work.OperationUnknown || op.TargetID != f.request.ID {
					t.Fatal("failed authority check partially committed repair")
				}
				return nil
			})
		})
	}
}
