package workruntime

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestOwnedNativeGoalToolUsesExactTurnAndPreservesExplicitBudget(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	budget := int64(1200)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalUpdated, ThreadID: "session", TurnID: "turn-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "session", Objective: "continue scoped work", Status: "active", TokenBudget: &budget, UpdatedAt: 42}}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		grant, err := tx.GetContinuationGrant("inc-1", "session")
		if err != nil {
			return err
		}
		if grant.AuthorizationKind != "native_owned_turn" || grant.SourceTurnID != "turn-1" || grant.BudgetSource != "native_explicit" || grant.TokenBudget != 1200 || grant.Source.EvidenceRef == "" {
			t.Fatalf("native tool source was treated as an RPC or guessed: %+v", grant)
		}
		return nil
	})
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "session", TurnID: "native-continuation"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		turn, err := tx.GetNativeTurn("inc-1", "session", "native-continuation")
		if err != nil {
			return err
		}
		if turn.Binding != *f.command.Execution || turn.ContinuationAuthorization == "" {
			t.Fatal("native Goal continuation changed business execution identity")
		}
		return nil
	})
}

func TestNativeGoalWithoutBudgetIsBoundedByRealCanonicalRPCBeforeGrant(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	f.coordinator.options.DefaultGoalTokenBudget = 4321
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalUpdated, ThreadID: "session", TurnID: "turn-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "session", Objective: "continue scoped work", Status: "active", UpdatedAt: 42}}); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "session", TurnID: "early-continuation"}); err != nil {
		t.Fatal(err)
	}
	sends := 0
	f.driver.sendCommand = func(ctx context.Context, instance string, command agentproto.Command) error {
		sends++
		if command.Goal.TokenBudget == nil || *command.Goal.TokenBudget != 4321 || command.Execution == nil || *command.Execution != *f.command.Execution || instance != "instance-1" {
			t.Fatal("policy used an invented execution or budget")
		}
		if err := f.coordinator.RegisterCommand(ctx, "inc-1", command); err != nil {
			return err
		}
		budget := *command.Goal.TokenBudget
		return f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, RequestID: command.CommandID + ":rpc", CommandID: command.CommandID, ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "session", Objective: command.Goal.Objective, Status: "active", TokenBudget: &budget, UpdatedAt: 43}})
	}
	if err := f.coordinator.RunGoalPolicies(context.Background(), 32); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.RunGoalPolicies(context.Background(), 32); err != nil {
		t.Fatal(err)
	}
	if sends != 1 {
		t.Fatal("native policy command was blindly resent")
	}
	f.tx(func(tx *workstore.Tx) error {
		grant, err := tx.GetContinuationGrant("inc-1", "session")
		if err != nil {
			return err
		}
		if grant.TokenBudget != 4321 || grant.AuthorizationKind != "canonical_goal_policy" || grant.BudgetSource != "configured_default" || grant.SourceTurnID != "turn-1" {
			t.Fatalf("policy provenance missing: %+v", grant)
		}
		turn, err := tx.GetNativeTurn("inc-1", "session", "early-continuation")
		if err != nil {
			return err
		}
		if turn.Binding != grant.Binding {
			t.Fatal("notification-before-budget-response guessed a different attempt")
		}
		count, err := tx.CountUnresolvedNativeEvents("inc-1")
		if err == nil && count != 0 {
			t.Fatal("authorized pending continuation was not replayed")
		}
		return err
	})
}

func TestUnownedNativeGoalDoesNotReceiveDefaultBudgetOrGrant(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalUpdated, ThreadID: "session", TurnID: "unknown-turn", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "session", Objective: "unproven", Status: "active", UpdatedAt: 42}}); err != nil {
		t.Fatal(err)
	}
	sends := 0
	f.driver.sendCommand = func(context.Context, string, agentproto.Command) error { sends++; return nil }
	if err := f.coordinator.RunGoalPolicies(context.Background(), 32); err != nil {
		t.Fatal(err)
	}
	if sends != 0 {
		t.Fatal("default budget authorized an unowned native turn")
	}
}
