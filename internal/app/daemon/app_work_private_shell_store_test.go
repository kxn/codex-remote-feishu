package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func privateShellStoreRequest(t *testing.T, f *reactionActionFixture) (workstore.BeginPrivateShellOperationRequest, agentproto.Command) {
	t.Helper()
	id := f.reaction(t, "independent-shell-source", "APPLAUSE", f.source.SenderIDs, time.Now())
	var action workstore.PrivateReactionAction
	pickerTx(t, f.a, func(tx *workstore.Tx) error { var e error; action, e = tx.GetPrivateReactionAction(id); return e })
	owner := action.Execution
	owner.AttemptID = ""
	owner.RunRef = ""
	payload, e := teamReactionShellPayload([]agentproto.Input{{Type: agentproto.InputText, Text: "actual authorized supplementary text"}})
	if e != nil {
		t.Fatal(e)
	}
	command := agentproto.Command{CommandID: "independent-shell-command", Kind: agentproto.CommandThreadShellCommand, Execution: &owner, Target: agentproto.Target{ThreadID: action.SessionID, TurnID: action.TurnID}, ShellCommand: agentproto.ShellCommand{OperationID: "independent-shell", Payload: payload}}
	req := workstore.BeginPrivateShellOperationRequest{ID: "independent-shell", Source: workstore.PrivateActionSource{PersonalScopeID: action.ScopeID, ReactionID: id}, InputID: action.InputID, InputIDs: action.InputIDs, Command: command, Now: time.Now()}
	return req, command
}

func TestPrivateShellReservedInputUsesIndependentActualTurnReceipt(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, created, e := tx.BeginPrivateShellOperation(req)
		if e != nil {
			return e
		}
		if !created || op.State != "sending" {
			t.Fatal("shell was not durably reserved")
		}
		pending, e := tx.ListPendingInputs(op.ScopeID, op.Binding.RoleID)
		if e != nil {
			return e
		}
		for _, input := range pending {
			if input.ID == req.InputID {
				t.Fatal("reserved shell input remained normally dispatchable")
			}
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if input.State != work.InboxPending || input.ClaimedAttemptID != "" || input.ShellOperationID != op.ID {
			t.Fatal("shell invented a model claim or lost source")
		}
		_, added, e := tx.BeginPrivateShellOperation(req)
		if e != nil {
			return e
		}
		if added {
			t.Fatal("replay manufactured another send")
		}
		return nil
	})
	sends := 0
	f.relay.send = func(_ string, got agentproto.Command) error {
		sends++
		if got.Execution.AttemptID != "" || got.NativeInputAdmission != nil {
			t.Fatal("shell remained model-owned")
		}
		return nil
	}
	if err := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); err != nil {
		t.Fatal(err)
	}
	if err := f.event(agentproto.Event{Kind: agentproto.EventNativeShellRPCResult, CommandID: command.CommandID, RequestID: "shell-rpc", ThreadID: command.Target.ThreadID, Status: "acknowledged", Execution: command.Execution}); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, e := tx.GetPrivateShellOperation(command.CommandID)
		if e != nil {
			return e
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if op.State != "unknown" || input.State != work.InboxPending {
			t.Fatal("RPC acknowledgement was mistaken for executed input")
		}
		return nil
	})
	if err := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "actual-independent-shell-turn", ItemID: "actual-shell-item", Action: "started", Status: "running", Execution: command.Execution}); err != nil {
		t.Fatal(err)
	}
	if err := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "actual-independent-shell-turn", ItemID: "actual-shell-item", Action: "completed", Status: "completed", Execution: command.Execution, Metadata: map[string]any{"userShellOutput": "actual fixed-read output", "exitCode": 0}}); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, e := tx.GetPrivateShellOperation(command.CommandID)
		if e != nil {
			return e
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if op.State != "completed" || op.ActualTurnID != "actual-independent-shell-turn" || input.State != work.InboxProcessed || input.ClaimedAttemptID != "" || input.ProcessingOutcomeRef != op.ReceiptRef {
			t.Fatal("actual shell completion did not independently consume original source")
		}
		attempt, e := tx.GetAttempt(op.FrozenExecution.AttemptID)
		if e != nil {
			return e
		}
		if attempt.State != work.AttemptRunning {
			t.Fatal("shell completion ended model turn")
		}
		effective, e := tx.EffectiveAttemptInputIDs(attempt.ID)
		if e != nil {
			return e
		}
		for _, id := range effective {
			if id == input.ID {
				t.Fatal("independent shell leaked into model effective input")
			}
		}
		return nil
	})
	if sends != 1 {
		t.Fatal("independent operation dispatched more than once")
	}
}

func TestPrivateShellDispatchRejectsSourceCancelledBetweenReservationAndRegistration(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		_, _, e := tx.BeginPrivateShellOperation(req)
		if e != nil {
			return e
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		before := input.Revision
		input.Revision++
		input.State = work.InboxCancelled
		return tx.PutInbox(input, before)
	})
	sends := 0
	f.relay.send = func(string, agentproto.Command) error { sends++; return nil }
	if err := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); err == nil {
		t.Fatal("cancelled source was sent")
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		if _, e := tx.GetNativeCommand(command.CommandID); !errors.Is(e, workstore.ErrNotFound) {
			t.Fatal("failed authorization registered native effect")
		}
		if e := tx.RejectPrivateShellBeforeSend(command.CommandID, "source cancelled before dispatch"); e != nil {
			return e
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if input.State != work.InboxCancelled || input.ShellOperationID != "" {
			t.Fatal("rejection lost original cancellation")
		}
		return nil
	})
	if sends != 0 {
		t.Fatal("cancel race reached transport")
	}
}

func TestPrivateShellUncertainDispatchCannotRegisterAgainAndWrongReceiptCannotConsume(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	pickerTx(t, f.a, func(tx *workstore.Tx) error { _, _, e := tx.BeginPrivateShellOperation(req); return e })
	sends := 0
	f.relay.send = func(string, agentproto.Command) error { sends++; return nil }
	if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e != nil {
		t.Fatal(e)
	}
	if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e == nil {
		t.Fatal("registered uncertain native shell was replayed")
	}
	if e := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: "other-session", TurnID: "actual-turn", ItemID: "item", Action: "completed", Status: "completed", Execution: command.Execution}); e == nil {
		t.Fatal("foreign session consumed original input")
	}
	if e := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "actual-turn", ItemID: "uncertain-item", Action: "completed", Status: "unknown", Execution: command.Execution}); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if input.State != work.InboxPending || input.ProcessingOutcomeRef != "" {
			t.Fatal("mismatched receipt changed source")
		}
		forged := command
		forged.Kind = agentproto.CommandModelList
		if _, e = tx.RegisterNativeCommand("steer-carrier", forged); e == nil {
			t.Fatal("private shell exception authorized arbitrary human owner-only RPC")
		}
		return nil
	})
	if sends != 1 {
		t.Fatal("uncertain dispatch crossed transport twice")
	}
}

func TestPrivateShellActualCompletionPreservesLateUserCancellation(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	f.relay.send = func(string, agentproto.Command) error { return nil }
	pickerTx(t, f.a, func(tx *workstore.Tx) error { _, _, e := tx.BeginPrivateShellOperation(req); return e })
	if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		before := input.Revision
		input.Revision++
		input.State = work.InboxCancelled
		return tx.PutInbox(input, before)
	})
	if e := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "actual-independent-shell-turn", ItemID: "completed-item", Action: "completed", Status: "completed", Execution: command.Execution, Metadata: map[string]any{"exitCode": 0}}); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, e := tx.GetPrivateShellOperation(command.CommandID)
		if e != nil {
			return e
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if op.State != "completed" || op.ReceiptRef == "" || input.State != work.InboxCancelled {
			t.Fatal("actual result lost evidence or overwrote human cancellation")
		}
		return nil
	})
}
