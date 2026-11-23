package daemon

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateShellStandaloneTurnBoundariesPersistUntilExactItem(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "terminal-before-item"}[reverse], func(t *testing.T) {
			f := newReactionActionFixture(t, work.BackendCodex)
			req, command := privateShellStoreRequest(t, f)
			pickerTx(t, f.a, func(tx *workstore.Tx) error { _, _, e := tx.BeginPrivateShellOperation(req); return e })
			f.relay.send = func(string, agentproto.Command) error { return nil }
			if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e != nil {
				t.Fatal(e)
			}
			sequence := uint64(1000)
			coordinator := workruntime.New(f.a.teamRuntime.store, workruntime.Options{EnqueueReconcile: func(string) {}})
			emit := func(event agentproto.Event) {
				t.Helper()
				sequence++
				event.Seq = sequence
				hash, e := agentproto.RuntimeEventPayloadHash(event)
				if e != nil {
					t.Fatal(e)
				}
				event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "steer-carrier", Sequence: sequence, PayloadHash: hash}
				if e = coordinator.HandleEvent(t.Context(), "steer-instance", event); e != nil {
					t.Fatal(e)
				}
			}
			start := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: command.Target.ThreadID, TurnID: "native-standalone-shell"}
			terminal := agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: command.Target.ThreadID, TurnID: start.TurnID, Status: "completed"}
			emit(start)
			if reverse {
				emit(terminal)
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				n, e := tx.CountUnresolvedNativeEvents("steer-carrier")
				if e != nil {
					return e
				}
				want := 1
				if reverse {
					want = 2
				}
				if n != want {
					t.Fatal("unbound shell boundary was guessed or dropped")
				}
				if _, e = tx.GetNativeTurn("steer-carrier", start.ThreadID, start.TurnID); !errors.Is(e, workstore.ErrNotFound) {
					t.Fatal("unproven boundary became model turn")
				}
				return nil
			})
			// The next process has no wrapper/coordinator boundary buffer to recover.
			coordinator = workruntime.New(f.a.teamRuntime.store, workruntime.Options{EnqueueReconcile: func(string) {}})
			emit(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: start.ThreadID, TurnID: start.TurnID, ItemID: "actual-fixed-cat-item", Action: "started", Status: "running", Execution: command.Execution})
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				op, e := tx.GetPrivateShellOperation(req.ID)
				if e != nil {
					return e
				}
				if op.TurnStartedSource == nil || (reverse && op.TurnTerminalSource == nil) {
					t.Fatal("exact item did not bind persisted independent boundaries")
				}
				n, e := tx.CountUnresolvedNativeEvents("steer-carrier")
				if e != nil {
					return e
				}
				if n != 0 {
					t.Fatal("proven shell boundaries remained unresolved")
				}
				if _, e = tx.GetRuntimeGate(command.Execution.WorkerSessionID); !errors.Is(e, workstore.ErrNotFound) {
					t.Fatal("shell proof did not remove only its provisional gate")
				}
				pending, e := tx.HasPendingPrivateShellTurns("steer-carrier")
				if e != nil {
					return e
				}
				if pending == reverse {
					t.Fatal("independent terminal inventory was not preserved")
				}
				return nil
			})
			emit(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: start.ThreadID, TurnID: start.TurnID, ItemID: "actual-fixed-cat-item", Action: "completed", Status: "completed", Execution: command.Execution, Metadata: map[string]any{"exitCode": 0}})
			if !reverse {
				emit(terminal)
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				pending, e := tx.HasPendingPrivateShellTurns("steer-carrier")
				if e != nil {
					return e
				}
				if pending {
					t.Fatal("actual independent terminal left permanent inventory")
				}
				model, e := tx.GetNativeTurn("steer-carrier", start.ThreadID, "steer-turn")
				if e != nil {
					return e
				}
				if model.TerminalStatus != "" {
					t.Fatal("standalone shell terminal ended original model turn")
				}
				source, e := tx.GetInbox(req.InputID)
				if e != nil {
					return e
				}
				if source.State != work.InboxProcessed || source.ClaimedAttemptID != "" {
					t.Fatal("shell input did not independently finish")
				}
				return nil
			})
		})
	}
}

func TestPrivateShellAuxiliaryItemDoesNotClaimExistingModelBoundaries(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	pickerTx(t, f.a, func(tx *workstore.Tx) error { _, _, e := tx.BeginPrivateShellOperation(req); return e })
	f.relay.send = func(string, agentproto.Command) error { return nil }
	if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e != nil {
		t.Fatal(e)
	}
	if e := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, ItemID: "aux-shell-item", Action: "completed", Status: "completed", Execution: command.Execution, Metadata: map[string]any{"exitCode": 0}}); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, e := tx.GetPrivateShellOperation(req.ID)
		if e != nil {
			return e
		}
		if op.TurnStartedSource != nil || op.TurnTerminalSource != nil {
			t.Fatal("auxiliary item took ownership of model boundaries")
		}
		pending, e := tx.HasPendingPrivateShellTurns(op.Incarnation)
		if e != nil {
			return e
		}
		if pending {
			t.Fatal("auxiliary completed item invented standalone terminal wait")
		}
		return nil
	})
}

func TestPrivateShellFailedActualReadRetainsSourceButClosesStandaloneInventory(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	pickerTx(t, f.a, func(tx *workstore.Tx) error { _, _, e := tx.BeginPrivateShellOperation(req); return e })
	f.relay.send = func(string, agentproto.Command) error { return nil }
	if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e != nil {
		t.Fatal(e)
	}
	if e := f.event(agentproto.Event{Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "failed-shell-turn", ItemID: "failed-fixed-cat", Action: "completed", Status: "failed", Execution: command.Execution, Metadata: map[string]any{"exitCode": 1, "userShellOutput": "possibly partial bytes"}}); e != nil {
		t.Fatal(e)
	}
	if e := f.event(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: command.Target.ThreadID, TurnID: "failed-shell-turn", Status: "failed"}); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		op, e := tx.GetPrivateShellOperation(req.ID)
		if e != nil {
			return e
		}
		input, e := tx.GetInbox(req.InputID)
		if e != nil {
			return e
		}
		if op.State != "unknown" || op.ReceiptRef == "" || op.TurnTerminalSource == nil || input.ShellOperationID != op.ID || input.State != work.InboxPending {
			t.Fatal("failed partial injection was consumed or automatically requeued")
		}
		pending, e := tx.HasPendingPrivateShellTurns(op.Incarnation)
		if e != nil {
			return e
		}
		if pending {
			t.Fatal("proven failed standalone terminal blocked model reconciliation forever")
		}
		return nil
	})
}

func TestPrivateShellProvisionalBoundariesReplayForLaterExactModelBinding(t *testing.T) {
	f := newReactionActionFixture(t, work.BackendCodex)
	req, command := privateShellStoreRequest(t, f)
	pickerTx(t, f.a, func(tx *workstore.Tx) error { _, _, e := tx.BeginPrivateShellOperation(req); return e })
	f.relay.send = func(string, agentproto.Command) error { return nil }
	if e := f.a.teamRuntime.driver.SendCommand(t.Context(), "steer-instance", command); e != nil {
		t.Fatal(e)
	}
	for _, event := range []agentproto.Event{{Kind: agentproto.EventTurnStarted, ThreadID: command.Target.ThreadID, TurnID: "later-model-proof"}, {Kind: agentproto.EventTurnCompleted, ThreadID: command.Target.ThreadID, TurnID: "later-model-proof", Status: "completed"}, {Kind: agentproto.EventNativeShellItemObserved, CommandID: command.CommandID, ThreadID: command.Target.ThreadID, TurnID: "later-model-proof", ItemID: "aux-in-unbound-model", Action: "completed", Status: "completed", Execution: command.Execution, Metadata: map[string]any{"exitCode": 0}}} {
		if e := f.event(event); e != nil {
			t.Fatal(e)
		}
	}
	var shell workstore.PrivateShellOperation
	var model work.ExecutionAttempt
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		var e error
		shell, e = tx.GetPrivateShellOperation(req.ID)
		if e != nil {
			return e
		}
		model, e = tx.GetAttempt(shell.FrozenExecution.AttemptID)
		return e
	})
	if e := f.event(agentproto.Event{Kind: agentproto.EventNativeTurnBindingObserved, CommandID: model.CommandID, RequestID: "exact-original-model-rpc", ThreadID: command.Target.ThreadID, TurnID: "later-model-proof", Execution: &shell.FrozenExecution}); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		turn, e := tx.GetNativeTurn(shell.Incarnation, shell.SessionID, "later-model-proof")
		if e != nil {
			return e
		}
		if turn.Binding != shell.FrozenExecution || !turn.Started || turn.TerminalStatus != "completed" {
			t.Fatal("provisional shell classification swallowed later proven model boundaries")
		}
		pending, e := tx.HasPendingPrivateShellTurns(shell.Incarnation)
		if e != nil {
			return e
		}
		if pending {
			t.Fatal("later exact model binding left shell inventory stranded")
		}
		return nil
	})
}
