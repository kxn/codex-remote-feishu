package workruntime

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestIdleGoalReadCannotKeepActiveAuthorizationForPausedObservation(t *testing.T) {
	f := idleKnownGoal(t, "active")
	owner := f.carrier.Owner
	f.tx(func(tx *workstore.Tx) error {
		grant, e := tx.GetContinuationGrant("inc-1", "session")
		if e != nil {
			return e
		}
		grant.Active = true
		return tx.PutContinuationGrant(grant)
	})
	command := agentproto.Command{CommandID: "read-paused", Goal: agentproto.GoalCommand{Expected: freezeIdleGoal(t, f)}, Kind: agentproto.CommandThreadGoalGet, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}}
	prepareOwnerGoal(t, f, command)
	if err := f.coordinator.RegisterCommand(t.Context(), "inc-1", command); err != nil {
		t.Fatal(err)
	}
	expected := freezeIdleGoal(t, f)
	budget := expected.TokenBudget
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "read-rpc", Execution: &owner, ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: expected.Objective, Status: "paused", TokenBudget: &budget, UpdatedAt: 43}}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		grant, e := tx.GetContinuationGrant("inc-1", "session")
		if e == nil && (grant.Active || grant.Status != "paused") {
			t.Fatal("read retained active continuation for paused goal")
		}
		return e
	})
}
