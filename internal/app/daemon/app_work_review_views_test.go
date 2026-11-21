package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

type reviewUIInspector struct{}

func (reviewUIInspector) SendCommand(context.Context, string, agentproto.Command) error { return nil }
func (reviewUIInspector) Inspect(_ context.Context, r workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	probe := func(id, method string, result any) agentproto.RPCProbe {
		request, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": map[string]any{"threadId": r.SessionID}})
		response, _ := json.Marshal(map[string]any{"id": id, "result": result})
		return agentproto.RPCProbe{Request: request, Response: response}
	}
	return workruntime.ObserveProtocol(r, workruntime.ProtocolInspection{Native: probe("read", "thread/read", map[string]any{"thread": map[string]any{"id": r.SessionID, "status": map[string]any{"type": "idle"}}}), Continuation: probe("goal", "thread/goal/get", map[string]any{"goal": nil}), Background: agentproto.BackgroundInspection{CarrierIncarnation: r.Carrier.Incarnation, ProcessGeneration: r.Carrier.ProcessGeneration, LocationGeneration: r.LocationGeneration, Complete: true, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true, Evidence: json.RawMessage(`{"external_fixture":"verified empty host inventory"}`)}, ObservedAt: time.Now().UTC()})
}

// The review is produced through real journal receipt, native turn mapping,
// result bridge, processing outcome and reconciliation APIs. No result row is
// fabricated and the paused source never needs to run to present its result.
func completedReviewUIFixture(t *testing.T, failures ...bool) (*App, teamHumanSource, string, string) {
	t.Helper()
	a, source := teamPickerFixture(t)
	now := time.Now().UTC()
	var wc work.WorkContext
	pickerTx(t, a, func(tx *workstore.Tx) error {
		menu, _, e := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "ui-review", Tenant: source.Tenant, SenderIDs: source.SenderIDs, EventKey: "review", ReceivedAt: now}, source.Gateway)
		if e != nil {
			return e
		}
		sending, _, e := tx.BeginMenuDestination(menu.ID, menu.Revision, "Actual SDK private entry fixture")
		if e != nil {
			return e
		}
		if e = tx.FinishMenuDestination(menu.ID, sending.Revision, source.ChatID, "review-entry", "", true); e != nil {
			return e
		}
		source.Key = menu.ID
		source.Authority = workstore.PrivateActionSource{PersonalScopeID: source.ScopeID, MenuInteractionID: menu.ID}
		if e = tx.PutLocation(work.ExecutionLocation{ID: "ui-review-location", HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); e != nil {
			return e
		}
		body := work.NewContent("ui-review-selection", source.ScopeID, "text/plain", []byte("Human selected directory fixture"), now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		return tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: source.ScopeID, SelectedLocationIDs: []string{"ui-review-location"}, DefaultLocationID: "ui-review-location", SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, SourceRef: body.ID, Revision: 1}, 0)
	})
	if _, e := a.queueTeamReview(t.Context(), source, control.Action{Kind: control.ActionReviewCommand, Text: "/review commit frozen-commit"}); e != nil {
		t.Fatal(e)
	}
	coordinator := workruntime.New(a.teamRuntime.store, workruntime.Options{Driver: reviewUIInspector{}})
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: source.ScopeID, ContextID: teamID("review-context", source.Key), RoleID: source.RoleID, WorkerSessionID: "ui-review-worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "ui-review-carrier", OriginInstanceID: "ui-review-instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var e error
		wc, e = tx.GetContext(owner.ContextID)
		if e != nil {
			return e
		}
		return tx.PutWorker(work.WorkerSession{ID: owner.WorkerSessionID, ScopeID: owner.ScopeID, ContextID: owner.ContextID, RoleID: owner.RoleID, Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: carrier.OriginInstanceID, InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: carrier.Incarnation, LocationID: "ui-review-location", LocationGeneration: 1, Revision: 1}, 0)
	})
	if e := coordinator.RegisterCarrier(t.Context(), carrier); e != nil {
		t.Fatal(e)
	}
	if e := coordinator.HandleHello(t.Context(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: carrier.OriginInstanceID, Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: carrier.Incarnation}); e != nil {
		t.Fatal(e)
	}
	binding := owner
	binding.AttemptID = "ui-review-attempt"
	binding.RunRef = "ui-review-run"
	command := agentproto.Command{CommandID: "ui-review-command", Kind: agentproto.CommandPromptSend, Execution: &binding, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Review frozen commit."}}}, IssuedAt: now}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(owner.WorkerSessionID)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(command)
		body := work.NewContent("ui-review-command-body", owner.ScopeID, "application/json", raw, now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: command.CommandID, ScopeID: owner.ScopeID, ContextID: owner.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: owner.RoleID}, Kind: "native.review", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: command.CommandID, RequestHash: body.SHA256, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: binding.AttemptID, ScopeID: owner.ScopeID, ContextID: owner.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: binding.RunRef}, State: work.AttemptDispatchPending, InputIDs: []string{wc.ID + ":input"}, CommandID: command.CommandID, Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
	if e := coordinator.RegisterCommand(t.Context(), carrier.Incarnation, command); e != nil {
		t.Fatal(e)
	}
	final := strings.Repeat("原样评审正文。", 900)
	events := []agentproto.Event{{Kind: agentproto.EventTurnStarted}, {Kind: agentproto.EventItemCompleted, ItemKind: "agent_message", ItemID: "final-item", Metadata: map[string]any{"text": final}}, {Kind: agentproto.EventTurnCompleted, Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime}}
	failed := len(failures) > 0 && failures[0]
	if failed {
		events = []agentproto.Event{{Kind: agentproto.EventTurnStarted}, {Kind: agentproto.EventTurnCompleted, Status: "failed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime}}
	}
	for n, event := range events {
		event.CommandID = command.CommandID
		event.RequestID = "native-review-request"
		event.ThreadID = "ui-review-session"
		event.TurnID = "ui-review-turn"
		event.Execution = agentproto.CloneExecutionBinding(&binding)
		event.Seq = uint64(n + 1)
		hash, e := agentproto.RuntimeEventPayloadHash(event)
		if e != nil {
			t.Fatal(e)
		}
		event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: carrier.Incarnation, Sequence: event.Seq, PayloadHash: hash}
		if e = coordinator.HandleEvent(t.Context(), carrier.OriginInstanceID, event); e != nil {
			t.Fatal(e)
		}
	}
	if e := coordinator.Reconcile(t.Context(), binding.AttemptID); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if e := tx.PutPauseGate(work.PauseGate{ID: "paused-source", ScopeID: source.ScopeID, ContextID: wc.Review.SourceContextID, Source: work.GateContext, SourceID: source.PrincipalID, State: work.GateClosed, Revision: 1}, 0); e != nil {
			return e
		}
		if failed {
			_, e := tx.GetMessage("review-failure:" + binding.AttemptID)
			return e
		}
		result, e := tx.GetReviewResult(binding.AttemptID)
		if e != nil {
			return e
		}
		if !result.Delivered {
			return fmt.Errorf("review did not actually reconcile")
		}
		return nil
	})
	return a, source, binding.AttemptID, final
}

func TestReviewResultUIIndependentOfPausedFrontAndPaginationKeepsOriginal(t *testing.T) {
	a, source, attemptID, original := completedReviewUIFixture(t)
	if e := a.runTeamReviewViews(t.Context()); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, e := tx.GetUIView("review-presentation:" + attemptID)
		if e != nil {
			return e
		}
		if view.PrincipalID != source.PrincipalID || view.ChatID != source.ChatID {
			t.Fatal("review escaped original personal destination")
		}
		body, e := tx.GetContent(source.ScopeID, view.BodyRef)
		if e != nil {
			return e
		}
		if !strings.Contains(string(body.Data), "frozen-commit") || !strings.Contains(string(body.Data), "原样评审正文") {
			t.Fatal("result lost frozen scope or original body")
		}
		p, e := loadTeamReviewPresentation(tx, attemptID)
		if e != nil {
			return e
		}
		saved, e := tx.GetContent(source.ScopeID, p.BodyRef)
		if e != nil {
			return e
		}
		if string(saved.Data) != original {
			t.Fatal("presentation modified original findings")
		}
		nav, e := tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		if nav.FrontContextID != view.ContextID {
			t.Fatal("presentation changed source navigation")
		}
		return nil
	})
	view, interaction := pickerAction(t, a, source, "review-presentation:"+attemptID, "review.page")
	if e := a.handleTeamReviewView(t.Context(), interaction, view, "review.page"); e != nil {
		t.Fatal(e)
	}
}

func TestReviewResultRejectsOtherPrincipalAndForwardedCard(t *testing.T) {
	a, source, attemptID, _ := completedReviewUIFixture(t)
	if e := a.runTeamReviewViews(t.Context()); e != nil {
		t.Fatal(e)
	}
	view, _ := pickerAction(t, a, source, "review-presentation:"+attemptID, "review.return")
	for n, test := range []struct{ openID, chat string }{{"different-person", view.ChatID}, {source.SenderIDs.OpenID, "forwarded-private-chat"}} {
		var interaction workstore.HumanInteraction
		pickerTx(t, a, func(tx *workstore.Tx) error {
			var e error
			interaction, _, e = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: fmt.Sprint("review-foreign", n), Tenant: source.Tenant, SenderIDs: work.SenderIDs{OpenID: test.openID}, ChatID: test.chat, MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": view.ID, "action": "review.return"}, ReceivedAt: time.Now().UTC()}, source.Gateway)
			return e
		})
		if e := a.handleTeamReviewView(t.Context(), interaction, view, "review.return"); e == nil {
			t.Fatal("foreign/forwarded review card changed private navigation")
		}
	}
}
func TestReviewReturnOnlyNavigatesAndFailureHasIndependentReadableNotice(t *testing.T) {
	a, source, attemptID, _ := completedReviewUIFixture(t, true)
	if e := a.runTeamReviewViews(t.Context()); e != nil {
		t.Fatal(e)
	}
	_, e := a.teamRuntime.service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "new-front-before-return", Source: source.Authority, Action: "new", ExpectedRevision: source.NavigationRevision})
	if e != nil {
		t.Fatal(e)
	}
	view, interaction := pickerAction(t, a, source, "review-presentation:"+attemptID, "review.return")
	if e = a.handleTeamReviewView(t.Context(), interaction, view, "review.return"); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, e := tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		if nav.FrontContextID != view.ContextID {
			t.Fatal("did not navigate to actual source work")
		}
		gate, e := tx.GetPauseGate("paused-source")
		if e != nil {
			return e
		}
		if gate.State != work.GateClosed {
			t.Fatal("review return resumed paused work")
		}
		if _, e = tx.GetReviewResult(attemptID); e != workstore.ErrNotFound {
			t.Fatal("failed review fabricated successful findings")
		}
		attempts, e := tx.ListAttempts(source.ScopeID)
		if e != nil {
			return e
		}
		if len(attempts) != 1 {
			t.Fatal("navigation started extra work")
		}
		return nil
	})
}
func TestReviewOldBotDestinationNotReusedButFreshOwnerHistoryCanRead(t *testing.T) {
	a, source, attemptID, _ := completedReviewUIFixture(t)
	var fresh teamHumanSource
	pickerTx(t, a, func(tx *workstore.Tx) error {
		_, _, e := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "stable-user-evidence", Tenant: source.Tenant, SenderIDs: work.SenderIDs{OpenID: source.SenderIDs.OpenID, UserID: "verified-platform-user"}, EventKey: "menu", ReceivedAt: time.Now().UTC()}, source.Gateway)
		if e != nil {
			return e
		}
		binding, e := tx.GetRoleBotBinding(source.RoleID)
		if e != nil {
			return e
		}
		binding.IdentityGeneration++
		if e = tx.PutRoleBotBinding(binding, 1); e != nil {
			return e
		}
		gateway := source.Gateway
		gateway.BotGeneration++
		menu, _, e := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "fresh-owner", Tenant: source.Tenant, SenderIDs: work.SenderIDs{OpenID: "new-generation-open", UserID: "verified-platform-user"}, EventKey: "history", ReceivedAt: time.Now().UTC()}, gateway)
		if e != nil {
			return e
		}
		if menu.PrincipalID != source.PrincipalID {
			t.Fatal("verified stable platform user did not preserve principal")
		}
		sending, _, e := tx.BeginMenuDestination(menu.ID, menu.Revision, "Actual SDK new private entry")
		if e != nil {
			return e
		}
		if e = tx.FinishMenuDestination(menu.ID, sending.Revision, "new-private-chat", "new-entry", "", true); e != nil {
			return e
		}
		fresh = source
		fresh.Key = menu.ID
		fresh.Gateway = gateway
		fresh.ChatID = "new-private-chat"
		fresh.SenderIDs = menu.Envelope.SenderIDs
		fresh.Authority = workstore.PrivateActionSource{PersonalScopeID: source.ScopeID, MenuInteractionID: menu.ID}
		return nil
	})
	if e := a.runTeamReviewViews(t.Context()); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if _, e := tx.GetUIView("review-presentation:" + attemptID); e != workstore.ErrNotFound {
			t.Fatal("old App generation silently reused return chat")
		}
		return nil
	})
	reviewID := teamID("review-context", source.Key)
	handled, _, e := a.queueTeamReviewHistory(t.Context(), fresh, reviewID, 0)
	if e != nil || !handled {
		t.Fatalf("current verified owner could not read saved history: %v %v", handled, e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, e := tx.GetUIView(workui.ID("review-history", attemptID, fresh.Key, "0"))
		if e != nil {
			return e
		}
		if view.ChatID != fresh.ChatID || view.Binding.IdentityGeneration != fresh.Gateway.BotGeneration {
			t.Fatal("fresh history used stale destination")
		}
		return nil
	})
}
