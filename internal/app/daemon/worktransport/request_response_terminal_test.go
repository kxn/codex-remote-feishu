package worktransport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func confirmNativeRequestView(f *nativeResponseFixture, id string) {
	f.t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		view, e := tx.GetUIView(id)
		if e != nil {
			return e
		}
		op, e := tx.GetOperation(view.OperationID)
		if e != nil {
			return e
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationSending
		if _, _, e = tx.PutOperation(op, before); e != nil {
			return e
		}
		before = op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = "native-card"
		if _, _, e = tx.PutOperation(op, before); e != nil {
			return e
		}
		before = view.Revision
		view.Revision++
		view.ExternalMessageID = "native-card"
		return tx.PutUIView(view, before)
	})
}

func TestNativeResponseTurnInterruptThenOfflineClosesLegacyWithoutSending(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	id := f.legacyUnregisteredResponse(t, false, true)
	f.tx(func(tx *workstore.Tx) error {
		return tx.PutInbox(work.Inbox{ID: "original-pending", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "context", EventID: "original", State: work.InboxPending, Revision: 1}, 0)
	})
	if err := f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "interrupted"})); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.Disconnect(t.Context(), "instance"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("old cancelled question was resent after proven shutdown terminal")
	}
	f.tx(func(tx *workstore.Tx) error {
		request, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if request.State != "stale" || request.ResolvedEvidenceRef == "" || op.State != work.OperationTerminal {
			t.Fatal("old unsent response remained a false pending request")
		}
		if _, e = tx.GetNativeCommand(id); !errors.Is(e, workstore.ErrNotFound) {
			t.Fatal("terminal closure registered or sent old response")
		}
		input, e := tx.GetInbox("original-pending")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			t.Fatal("closing an old question consumed original pending input")
		}
		occupied, e := tx.GetOccupiedAttemptForWorker("worker")
		if e != nil || occupied.ID != "attempt" {
			t.Fatal("question closure released native business occupancy")
		}
		views, e := tx.ListUIViews("scope")
		if e != nil {
			return e
		}
		var closed workstore.UIView
		for _, view := range views {
			if !view.Invalidated && view.TargetID == request.ID {
				closed = view
			}
		}
		if closed.ReplacesViewID != "native-view" {
			t.Fatal("terminal status appended instead of patching old confirmed question")
		}
		publication, e := tx.GetOperation(closed.OperationID)
		if e != nil {
			return e
		}
		projection, e := workservice.ReadMessageProjection(tx, publication)
		if e != nil {
			return e
		}
		if projection.ReplaceMessageID != "native-card" || !strings.Contains(string(projection.Card), "不再等待答复") {
			t.Fatal("terminal UI did not close actual original card")
		}
		return nil
	})
	if err := f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.writes != 0 {
		t.Fatal("repeated terminal projection resent old human response")
	}
}

func TestNativeResponseHistoricalTerminalClosesAfterControllerGenerationChanged(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	id := f.legacyUnregisteredResponse(t, false, true)
	if err := f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "interrupted"})); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		before := worker.Revision
		worker.ControllerGeneration++
		worker.Revision++
		return tx.PutWorker(worker, before)
	})
	if err := f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		request, err := tx.GetNativeRequest(f.request.ID)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if request.State != "stale" || request.ResolvedEvidenceRef == "" || op.State != work.OperationTerminal || f.writes != 0 {
			t.Fatal("historical terminal did not close old request without resending")
		}
		return nil
	})
}

func TestNativeResponsePendingAndResolvedUseSameConfirmedQuestionCard(t *testing.T) {
	f := responseIntegrationFixture(t, true)
	if _, err := f.service.Respond(t.Context(), f.decision); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	var pendingID string
	f.tx(func(tx *workstore.Tx) error {
		request, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		view, e := tx.CurrentNativeRequestView("scope", request.ID, "request:"+request.ID)
		if e != nil {
			return e
		}
		pendingID = view.ID
		if view.ReplacesViewID != "native-view" {
			t.Fatal("pending answer appended extra card")
		}
		op, e := tx.GetOperation(view.OperationID)
		if e != nil {
			return e
		}
		p, e := workservice.ReadMessageProjection(tx, op)
		if e != nil {
			return e
		}
		if p.ReplaceMessageID != "native-card" {
			t.Fatal("pending answer patched wrong message")
		}
		return nil
	})
	confirmNativeRequestView(f, pendingID)
	if err := f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventRequestResolved, ThreadID: "session", TurnID: "turn", RequestID: f.request.NativeRequestID})); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		view, e := tx.CurrentNativeRequestView("scope", f.request.ID, "request:"+f.request.ID)
		if e != nil {
			return e
		}
		if view.ReplacesViewID != pendingID {
			t.Fatal("native resolution did not update current confirmed card")
		}
		op, e := tx.GetOperation(view.OperationID)
		if e != nil {
			return e
		}
		p, e := workservice.ReadMessageProjection(tx, op)
		if e != nil {
			return e
		}
		if p.ReplaceMessageID != "native-card" {
			t.Fatal("native resolution appended or retargeted card")
		}
		return nil
	})
}

func TestNativeResponseTerminalDoesNotReplayUnknownQuestionPublication(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	f.tx(func(tx *workstore.Tx) error {
		previous, e := tx.GetUIView("native-view")
		if e != nil {
			return e
		}
		next := previous
		next.ID = "uncertain-question-update"
		next.ReplacesViewID = previous.ID
		next.ExternalMessageID = ""
		next.OperationID = ""
		next.BodyRef = ""
		card, e := workui.Card(next.ID, workui.Panel{Title: "Question", Paragraphs: []string{"frozen pending update"}})
		if e != nil {
			return e
		}
		next, e = workui.Queue(tx, next, card)
		if e != nil {
			return e
		}
		op, e := tx.GetOperation(next.OperationID)
		if e != nil {
			return e
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationSending
		if _, _, e = tx.PutOperation(op, before); e != nil {
			return e
		}
		before = op.Revision
		op.Revision++
		op.State = work.OperationUnknown
		_, _, e = tx.PutOperation(op, before)
		return e
	})
	if err := f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "interrupted"})); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e != nil {
			return e
		}
		if len(views) != 2 {
			t.Fatal("terminal emitted a replacement despite uncertain original update")
		}
		op, e := tx.GetOperation("uncertain-question-update:publish")
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatal("unknown publication history was rewritten")
		}
		request, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		if request.State != "stale" {
			t.Fatal("delivery uncertainty prevented terminal question closure")
		}
		return nil
	})
}

type failBeforeResponseRegistration struct{}

func (failBeforeResponseRegistration) RegisterCommand(_ context.Context, _ string, _ agentproto.Command) error {
	return errors.New("local registration unavailable")
}

func TestNativeResponseRegistrationFailureIsUnsentAndSafelyRetried(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	f.driver.BindRegistrar(failBeforeResponseRegistration{})
	id, err := f.service.Respond(t.Context(), f.decision)
	if err == nil || f.writes != 0 {
		t.Fatal("local registration error reached native write")
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.State != work.OperationRetryable || len(op.Steps) != 1 {
			t.Fatal("registration error was treated as unknown remote effect")
		}
		before := op.Revision
		op.Revision++
		due := time.Now().UTC().Add(-time.Second)
		op.NextAttemptAt = &due
		_, _, e = tx.PutOperation(op, before)
		return e
	})
	f.driver.BindRegistrar(f.coordinator)
	if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("proven unsent saved cancellation was not retried")
	}
}
