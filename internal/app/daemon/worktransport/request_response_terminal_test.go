package worktransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
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

func TestUnsentNativeResponseWaitsForConnectedUngatedCarrier(t *testing.T) {
	for _, gated := range []bool{false, true} {
		t.Run(fmt.Sprintf("gated=%v", gated), func(t *testing.T) {
			f := responseIntegrationFixture(t, false)
			f.driver.BindRegistrar(failBeforeResponseRegistration{})
			id, err := f.service.Respond(t.Context(), f.decision)
			if err == nil {
				t.Fatal("expected pre-send failure")
			}
			var revision int64
			f.tx(func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(id)
				if e != nil {
					return e
				}
				before := op.Revision
				op.Revision++
				due := time.Now().UTC().Add(-time.Second)
				op.NextAttemptAt = &due
				if _, _, e = tx.PutOperation(op, before); e != nil {
					return e
				}
				revision = op.Revision
				if gated {
					return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: "worker", ScopeID: "scope", AttemptID: "attempt", Reason: "carrier_disconnected_reconcile", CreatedAt: time.Now().UTC()})
				}
				return tx.SetCarrierConnected("carrier-1", false)
			})
			f.driver.BindRegistrar(f.coordinator)
			for i := 0; i < 2; i++ {
				if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
					t.Fatal(err)
				}
			}
			f.tx(func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(id)
				if e != nil {
					return e
				}
				if op.Revision != revision || op.State != work.OperationRetryable || f.writes != 0 {
					t.Fatal("offline or gated response was retried")
				}
				req, e := tx.GetNativeRequest(f.request.ID)
				if e != nil {
					return e
				}
				if req.State != "response_pending" {
					t.Fatal("uncertain attempt was closed")
				}
				if gated {
					return tx.ClearRuntimeGate("worker", "carrier_disconnected_reconcile")
				}
				return tx.SetCarrierConnected("carrier-1", true)
			})
			if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
				t.Fatal(err)
			}
			if f.writes != 1 {
				t.Fatal("saved response did not resume after carrier became eligible")
			}
		})
	}
}

func TestLegacyUnregisteredResponseRepairsLocallyWhileCarrierOffline(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	id := f.legacyUnregisteredResponse(t, false)
	f.tx(func(tx *workstore.Tx) error { return tx.SetCarrierConnected("carrier-1", false) })
	if err := f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	var revision int64
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.State != work.OperationRetryable || op.TargetID != "worker" || len(op.Steps) != 1 || op.Steps[0].ID != "local_registration_target_repair" || f.writes != 0 {
			t.Fatal("offline legacy target repair did not preserve unsent decision")
		}
		revision = op.Revision
		return nil
	})
	if err := f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.Revision != revision {
			t.Fatal("offline retry modified repaired decision")
		}
		return tx.SetCarrierConnected("carrier-1", true)
	})
	if err := f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 {
		t.Fatal("repaired response did not send after reconnect")
	}
}

func TestRetiredPrivateControlCancellationClosesOnlyAfterExactProcessExit(t *testing.T) {
	f := responseIntegrationFixture(t, false, true)
	f.service.SetLocalHostID("local")
	id := f.legacyUnregisteredResponse(t, false, true)
	var err error
	child := exec.Command(os.Args[0], "-test.run=^TestRetiredControlProcessHelper$")
	child.Env = append(os.Environ(), "FSC_RETIRED_CONTROL_HELPER=1")
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	identity, err := hostinventory.CaptureProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		carrier, e := tx.GetCarrier("carrier-1")
		if e != nil {
			return e
		}
		snapshot := agentproto.RuntimeLaunchSnapshot{HostID: "local", Version: 1, Owner: carrier.Owner, CWD: t.TempDir(), InstructionBundle: agentproto.InstructionBundle{Revision: "v1", Text: "test"}, JournalPath: filepath.Join(t.TempDir(), "runtime.db"), CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
		raw, e := json.Marshal(struct {
			Snapshot agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
		}{snapshot})
		if e != nil {
			return e
		}
		body := work.NewContent("launch-proof", "scope", "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: "launch", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "host"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "launch", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, ExternalReceipt: "test-launch", Revision: 1}, 0)
		if e != nil {
			return e
		}
		if e = tx.PutCarrierProcess("carrier-1", "instance", agentproto.RuntimeProcessIdentity{HostID: "local", Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity}); e != nil {
			return e
		}
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		old := worker.Revision
		worker.Revision++
		worker.State = work.WorkerRecovering
		if e = tx.PutWorker(worker, old); e != nil {
			return e
		}
		if e = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: "worker", ScopeID: "scope", AttemptID: "attempt", Reason: "carrier_disconnected_reconcile", CreatedAt: time.Now().UTC()}); e != nil {
			return e
		}
		if e = tx.SetCarrierConnected("carrier-1", false); e != nil {
			return e
		}
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		old = op.Revision
		op.Revision++
		due := time.Now().UTC().Add(-time.Second)
		op.NextAttemptAt = &due
		_, _, e = tx.PutOperation(op, old)
		return e
	})
	if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		r, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		if r.State != "response_pending" {
			t.Fatal("live process closed question")
		}
		return nil
	})
	// Simulate a crash after a retry was durably marked sending but before registration.
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.State != work.OperationRetryable || len(op.Steps) != 1 || op.Steps[0].ID != "local_registration_target_repair" {
			t.Fatal("missing proven unsent decision")
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationSending
		_, _, e = tx.PutOperation(op, old)
		return e
	})
	if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		r, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		if r.State != "response_pending" || f.writes != 0 {
			t.Fatal("live carrier sending response was settled or replayed")
		}
		return nil
	})
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err == nil {
		t.Fatal("expected killed process")
	}
	if err = f.service.ProjectForms(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e != nil {
			return e
		}
		var closed workstore.UIView
		for _, view := range views {
			if !view.Invalidated && view.TargetID == f.request.ID {
				closed = view
			}
		}
		if closed.ReplacesViewID != "native-view" {
			t.Fatal("retired request did not update original card")
		}
		publication, e := tx.GetOperation(closed.OperationID)
		if e != nil {
			return e
		}
		projection, e := workservice.ReadMessageProjection(tx, publication)
		if e != nil {
			return e
		}
		if projection.ReplaceMessageID != "native-card" || !strings.Contains(string(projection.Card), "取消未送达") {
			t.Fatal("original card lacks truthful unsent cancellation status")
		}
		r, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		if r.State != "stale" || r.ResolvedEvidenceRef == "" {
			t.Fatal("exited control request remains open")
		}
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.State != work.OperationTerminal {
			t.Fatal("unsent cancel not terminated")
		}
		attempt, e := tx.GetAttempt("attempt")
		if e != nil {
			return e
		}
		if attempt.State != work.AttemptRunning {
			t.Fatal("exit falsely resolved attempt")
		}
		turn, e := tx.GetNativeTurn("carrier-1", "session", "turn")
		if e != nil {
			return e
		}
		if turn.TerminalStatus != "" {
			t.Fatal("exit fabricated native completion")
		}
		_, e = tx.GetRuntimeGate("worker")
		return e
	})
}

func TestRetiredControlProcessHelper(t *testing.T) {
	if os.Getenv("FSC_RETIRED_CONTROL_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestNativeTerminalPrecedesLegacyDecisionRepair(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	id := f.legacyUnregisteredResponse(t, false, true)
	if err := f.coordinator.HandleEvent(t.Context(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "start", ThreadID: "session", TurnID: "turn", Status: "interrupted"})); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "changed", IdentityGeneration: 2}, 1)
	})
	if err := f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		r, e := tx.GetNativeRequest(f.request.ID)
		if e != nil {
			return e
		}
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if r.State != "stale" || op.State != work.OperationTerminal {
			t.Fatal("terminal event was blocked by obsolete decision repair")
		}
		return nil
	})
}

func TestSendingResponseWithUnsentHistoryIsNeverReplayedOnConnectedCarrier(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	f.driver.BindRegistrar(failBeforeResponseRegistration{})
	id, err := f.service.Respond(t.Context(), f.decision)
	if err == nil {
		t.Fatal("expected unsent registration")
	}
	var revision int64
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationSending
		op.NextAttemptAt = nil
		_, _, e = tx.PutOperation(op, old)
		revision = op.Revision
		return e
	})
	f.driver.BindRegistrar(f.coordinator)
	for i := 0; i < 2; i++ {
		if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
			t.Fatal(err)
		}
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.Revision != revision || op.State != work.OperationSending || f.writes != 0 {
			t.Fatal("live sending operation was replayed")
		}
		_, e = tx.GetNativeCommand(id)
		if !errors.Is(e, workstore.ErrNotFound) {
			t.Fatal("live sending operation was registered by recovery")
		}
		return nil
	})
}
