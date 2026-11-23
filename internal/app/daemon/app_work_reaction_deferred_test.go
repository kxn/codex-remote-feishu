package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func deferredReactionSource(t *testing.T, f *reactionActionFixture, processFirst bool, sourceKinds ...string) (*workingress.Ingress, string, string) {
	t.Helper()
	ingress, e := workingress.New(f.a.teamRuntime.store, workingress.Config{Directory: steerSDKDirectory{f.source.Gateway}, Expander: steerMediaExpander{}})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	raw := work.RawIngressEnvelope{GatewayRef: f.source.Gateway.GatewayRef, AppID: f.source.Gateway.AppID, EventID: "deferred-text", Tenant: f.source.Tenant, ChatID: f.source.ChatID, ChatType: "p2p", MessageID: "deferred-text", SenderType: "user", SenderIDs: f.source.SenderIDs, MessageType: "text", RawContent: `{"text":"actual deferred instruction"}`, ReceivedAt: now}
	if len(sourceKinds) > 0 {
		switch sourceKinds[0] {
		case "image":
			raw.MessageType = "image"
			raw.RawContent = `{"image_key":"fixture"}`
		case "slash":
			raw.RawContent = `{"text":"/menu"}`
		}
	}
	if _, e = ingress.Receive(t.Context(), raw); e != nil {
		t.Fatal(e)
	}
	inputID := ""
	if processFirst {
		if result, e := ingress.ProcessPending(t.Context(), 16); e != nil || len(result.Failures) > 0 {
			t.Fatalf("expand failed: %+v %v", result, e)
		}
		pickerTx(t, f.a, func(tx *workstore.Tx) error {
			inputs, e := tx.ListPendingInputs(f.source.ScopeID, f.source.RoleID)
			if e != nil {
				return e
			}
			for _, input := range inputs {
				message, e := tx.GetMessage(input.MessageID)
				if e != nil {
					return e
				}
				if message.PlatformKey.MessageID == raw.MessageID {
					inputID = input.ID
					before := input.Revision
					input.Revision++
					input.ContentState = work.InputContentPending
					return tx.PutInbox(input, before)
				}
			}
			t.Fatal("missing newly processed input")
			return nil
		})
	}
	observationID := ""
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		event := feishu.PlatformIngressEvent{GatewayRef: raw.GatewayRef, AppID: raw.AppID, EventID: "reaction-before-expand", Tenant: raw.Tenant, ChatID: raw.ChatID, MessageID: raw.MessageID, Kind: "reaction.created", OperatorType: "user", OperatorIDs: raw.SenderIDs, Emoji: "THUMBSUP", OccurredAt: now, ReceivedAt: now.Add(time.Millisecond)}
		facts, e := json.Marshal(event)
		if e != nil {
			return e
		}
		id, _, e := tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: f.source.Gateway, EventID: event.EventID, Tenant: event.Tenant, ChatID: event.ChatID, MessageID: event.MessageID, Kind: event.Kind, Facts: facts, ReceivedAt: event.ReceivedAt})
		if e != nil {
			return e
		}
		observationID = id
		if e = tx.RegisterPrivateReactionAction(id); e != nil {
			return e
		}
		action, e := tx.GetPrivateReactionAction(id)
		if e != nil {
			return e
		}
		if action.State != "waiting_source" || action.InputID != "" || action.RawIngressID == "" || action.Execution.AttemptID == "" || action.TurnID != "steer-turn" {
			t.Fatal("reaction did not freeze source route and arrival execution before expansion")
		}
		return nil
	})
	return ingress, observationID, inputID
}

func TestDeferredPrivateReactionWaitsForOriginalBodyWithoutChoosingNewTarget(t *testing.T) {
	for _, pendingBody := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-message-expansion", true: "existing-content-pending"}[pendingBody], func(t *testing.T) {
			f := newReactionActionFixture(t, work.BackendCodex)
			ingress, id, inputID := deferredReactionSource(t, f, pendingBody)
			sends := 0
			f.relay.send = func(_ string, command agentproto.Command) error {
				sends++
				if command.Target.ThreadID != "steer-session" || command.Target.TurnID != "steer-turn" {
					t.Fatal("deferred reaction selected another native target")
				}
				return nil
			}
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
			if sends != 0 {
				t.Fatal("pending body was injected before it became ready")
			}
			if pendingBody {
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					input, e := tx.GetInbox(inputID)
					if e != nil {
						return e
					}
					before := input.Revision
					input.Revision++
					input.ContentState = work.InputContentReady
					return tx.PutInbox(input, before)
				})
			} else if result, e := ingress.ProcessPending(t.Context(), 16); e != nil || len(result.Failures) > 0 {
				t.Fatalf("expand failed: %+v %v", result, e)
			}
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
			if sends != 1 {
				t.Fatalf("original reaction disappeared or duplicated: %d", sends)
			}
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
			if sends != 1 {
				t.Fatal("deferred reaction was replayed")
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				action, e := tx.GetPrivateReactionAction(id)
				if e != nil {
					return e
				}
				if action.State != "handled" || action.InputID == "" || len(action.InputIDs) != 1 {
					t.Fatal("source did not resolve exactly once")
				}
				return nil
			})
		})
	}
}

func TestDeferredPrivateReactionDoesNotTransferToLaterTurn(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	ingress, id, _ := deferredReactionSource(t, f, false)
	terminal := agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "steer-session", TurnID: "steer-turn", Status: "completed", Seq: 100}
	hash, e := agentproto.RuntimeEventPayloadHash(terminal)
	if e != nil {
		t.Fatal(e)
	}
	terminal.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "steer-carrier", Sequence: 100, PayloadHash: hash}
	coordinator := workruntime.New(f.a.teamRuntime.store, workruntime.Options{EnqueueReconcile: func(string) {}})
	if e = coordinator.HandleEvent(t.Context(), "steer-instance", terminal); e != nil {
		t.Fatal(e)
	}
	if result, e := ingress.ProcessPending(t.Context(), 16); e != nil || len(result.Failures) > 0 {
		t.Fatalf("expand failed: %+v %v", result, e)
	}
	sends := 0
	f.relay.send = func(string, agentproto.Command) error { sends++; return nil }
	if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
		t.Fatal(e)
	}
	if sends != 0 {
		t.Fatal("late body moved old reaction into a newer turn")
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		action, e := tx.GetPrivateReactionAction(id)
		if e != nil {
			return e
		}
		if action.State != "handled" || action.LastError == "" || action.TurnID != "steer-turn" {
			t.Fatal("old frozen reaction target was replaced instead of refused")
		}
		return nil
	})
}

func TestDeferredPrivateReactionDoesNotActivateStandaloneMediaOrSlash(t *testing.T) {
	for _, kind := range []string{"image", "slash"} {
		t.Run(kind, func(t *testing.T) {
			f := newReactionActionFixture(t, work.BackendCodex)
			ingress, id, _ := deferredReactionSource(t, f, false, kind)
			if result, e := ingress.ProcessPending(t.Context(), 16); e != nil || len(result.Failures) > 0 {
				t.Fatalf("process failed: %+v %v", result, e)
			}
			sends := 0
			f.relay.send = func(string, agentproto.Command) error { sends++; return nil }
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
			if sends != 0 {
				t.Fatal("standalone media or Slash became unsolicited native input")
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				action, e := tx.GetPrivateReactionAction(id)
				if e != nil {
					return e
				}
				if action.State != "handled" || action.LastError == "" {
					t.Fatal("ineligible deferred source was not durably closed")
				}
				return nil
			})
		})
	}
}
