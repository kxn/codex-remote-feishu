package workruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestIdleGoalGetConfirmsActualResultWithoutInventingAttemptOrGrant(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	f.report()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	owner := f.carrier.Owner
	command := agentproto.Command{CommandID: "idle-get", Kind: agentproto.CommandThreadGoalGet, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasAnyCarrierOccupancy(f.carrier)
		if err != nil {
			return err
		}
		if occupied {
			t.Fatal("fixture did not become idle")
		}
		op, err := f.operation(tx, command)
		if err != nil {
			return err
		}
		op.Kind = "native.user.goal"
		op.Actor = work.Actor{Kind: work.ActorSystem, ID: "native-user-command"}
		op.State = work.OperationSending
		_, _, err = tx.PutOperation(op, 0)
		return err
	})
	if err := f.coordinator.RegisterCommand(context.Background(), f.carrier.Incarnation, command); err != nil {
		t.Fatal(err)
	}
	budget := int64(600)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "rpc-goal-get", ThreadID: "session", Execution: &owner, ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: "observed existing goal", Status: "active", TokenBudget: &budget, UpdatedAt: 4}}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(command.CommandID)
		if err != nil {
			return err
		}
		if op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
			t.Fatal("real get result was never confirmed")
		}
		if _, err = tx.GetContinuationGrant(f.carrier.Incarnation, "session"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("idle read minted a continuation grant")
		}
		return nil
	})
	command.CommandID = "wrong-session"
	command.Target.ThreadID = "other-history"
	if err := f.coordinator.RegisterCommand(context.Background(), f.carrier.Incarnation, command); err == nil {
		t.Fatal("idle goal read crossed native session ownership")
	}
}
