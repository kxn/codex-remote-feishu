package workruntime

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
)

func TestIndependentLateOwnerGoalReadOrClearPreservesNewGrant(t *testing.T) {
	for _, kind := range []string{"get-paused", "get-missing", "clear", "get-paused-frozen", "get-missing-frozen", "clear-frozen"} {
		t.Run(kind, func(t *testing.T) {
			baseKind := strings.TrimSuffix(kind, "-frozen")
			f := idleKnownGoal(t, "paused")
			original := freezeIdleGoal(t, f)
			budget := original.TokenBudget
			owner := f.carrier.Owner
			command := agentproto.Command{CommandID: "old-owner-command", Kind: agentproto.CommandThreadGoalGet, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}}
			if baseKind == "clear" {
				command.Kind = agentproto.CommandThreadGoalClear
			}
			if strings.HasSuffix(kind, "-frozen") {
				command.Goal.Expected = original
			}
			prepareOwnerGoal(t, f, command)
			if err := f.coordinator.RegisterCommand(t.Context(), "inc-1", command); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				grant, err := tx.GetContinuationGrant("inc-1", "session")
				if err != nil {
					return err
				}
				grant.GoalVersion, grant.Status, grant.Active, grant.Reconciled = 100, "active", true, true
				return tx.PutContinuationGrant(grant)
			})
			event := agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "old-owner-rpc", Execution: &owner, ThreadID: "session"}
			switch baseKind {
			case "clear":
				event.GoalCleared = true
			case "get-missing":
				event.GoalMissing = true
			default:
				event.ThreadGoal = &agentproto.ThreadGoalUpdate{Objective: original.Objective, Status: "paused", TokenBudget: &budget, UpdatedAt: original.GoalVersion}
			}
			if err := f.handle(event); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				grant, err := tx.GetContinuationGrant("inc-1", "session")
				if err != nil {
					return err
				}
				if grant.GoalVersion != 100 || grant.Status != "active" || !grant.Active || !grant.Reconciled || grant.Objective != original.Objective || grant.TokenBudget != budget {
					t.Fatalf("late %s overwrote newer grant: version=%d status=%s active=%v reconciled=%v objective=%q budget=%d", kind, grant.GoalVersion, grant.Status, grant.Active, grant.Reconciled, grant.Objective, grant.TokenBudget)
				}
				return nil
			})
		})
	}
}

func TestIndependentFailedGoalReadCannotGateRunningWork(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	f.start()
	command := f.command
	command.CommandID = "running-read-command"
	command.Kind = agentproto.CommandThreadGoalGet
	f.tx(func(tx *workstore.Tx) error {
		operation, err := f.operation(tx, command)
		if err != nil {
			return err
		}
		scope, err := tx.GetScope("scope")
		if err != nil {
			return err
		}
		operation.Kind = "native.user.goal"
		operation.Actor = work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}
		operation.State = work.OperationSending
		_, _, err = tx.PutOperation(operation, 0)
		return err
	})
	if err := f.coordinator.RegisterCommand(t.Context(), f.carrier.Incarnation, command); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "failed-read-rpc", Execution: command.Execution, ThreadID: "session", ErrorMessage: "query temporarily unavailable"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		gate, err := tx.GetRuntimeGate("worker")
		if !errors.Is(err, workstore.ErrNotFound) {
			t.Fatalf("failed read installed execution gate: %+v %v", gate, err)
		}
		return nil
	})
}
