package workruntime

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func idleKnownGoal(t *testing.T, status string) *fixture {
	t.Helper()
	f := newFixture(t, work.BackendCodex, false, true)
	f.start()
	budget := int64(600)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalUpdated, ThreadID: "session", TurnID: "turn-1", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: "existing authorized work", Status: status, TokenBudget: &budget, UpdatedAt: 42}}); err != nil {
		t.Fatal(err)
	}
	if status == "active" {
		f.tx(func(tx *workstore.Tx) error {
			grant, e := tx.GetContinuationGrant("inc-1", "session")
			if e != nil {
				return e
			}
			grant.Active = false
			return tx.PutContinuationGrant(grant)
		})
	}
	f.report()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, e := tx.HasAnyCarrierOccupancy(f.carrier)
		if occupied {
			t.Fatal("fixture not idle")
		}
		return e
	})
	return f
}

func freezeIdleGoal(t *testing.T, f *fixture) *agentproto.GoalPrecondition {
	t.Helper()
	var expected *agentproto.GoalPrecondition
	f.tx(func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		grant, e := tx.GetContinuationGrant("inc-1", "session")
		if e != nil {
			return e
		}
		expected = &agentproto.GoalPrecondition{Incarnation: "inc-1", SessionID: "session", WorkerGeneration: worker.Generation, ControllerGeneration: worker.ControllerGeneration, GoalVersion: grant.GoalVersion, CommandID: grant.CommandID, Objective: grant.Objective, TokenBudget: grant.TokenBudget, Status: grant.Status}
		return nil
	})
	return expected
}

func prepareOwnerGoal(t *testing.T, f *fixture, command agentproto.Command) {
	t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		op, e := f.operation(tx, command)
		if e != nil {
			return e
		}
		op.Kind = "native.user.goal"
		op.Actor = work.Actor{Kind: work.ActorSystem, ID: "user-session-goal"}
		op.State = work.OperationSending
		_, _, e = tx.PutOperation(op, 0)
		return e
	})
}

func TestIdleGoalPauseIsStateOnlyExactAndReceiptBound(t *testing.T) {
	f := idleKnownGoal(t, "active")
	expected := freezeIdleGoal(t, f)
	budget := expected.TokenBudget
	owner := f.carrier.Owner
	command := agentproto.Command{CommandID: "idle-pause", Kind: agentproto.CommandThreadGoalSet, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}, Goal: agentproto.GoalCommand{Status: "paused", Objective: expected.Objective, TokenBudget: &budget, Expected: expected}}
	prepareOwnerGoal(t, f, command)
	if err := f.coordinator.RegisterCommand(t.Context(), "inc-1", command); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "pause-rpc", Execution: &owner, ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: expected.Objective, Status: "active", TokenBudget: &budget, UpdatedAt: 43}}); err == nil {
		t.Fatal("active result confirmed a pause")
	}
	// A wrong response rolls back without confirming success; its sequence is
	// followed by the actual matching native response.
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "pause-rpc", Execution: &owner, ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: expected.Objective, Status: "paused", TokenBudget: &budget, UpdatedAt: 44}}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(command.CommandID)
		if e != nil {
			return e
		}
		if op.State != work.OperationConfirmed {
			t.Fatal("matching pause not confirmed")
		}
		grant, e := tx.GetContinuationGrant("inc-1", "session")
		if e != nil {
			return e
		}
		if grant.Active || grant.Status != "paused" || grant.GoalVersion != 44 || grant.Objective != expected.Objective || grant.TokenBudget != budget {
			t.Fatalf("pause changed goal or granted continuation: %+v", grant)
		}
		occupied, e := tx.HasAnyCarrierOccupancy(f.carrier)
		if occupied {
			t.Fatal("idle pause started an execution")
		}
		return e
	})
}

func TestIdleGoalPauseRejectsStaleOrEffectfulOwnerMutation(t *testing.T) {
	for _, mutation := range []string{"version", "session", "objective", "budget", "active"} {
		t.Run(mutation, func(t *testing.T) {
			f := idleKnownGoal(t, "active")
			expected := freezeIdleGoal(t, f)
			budget := expected.TokenBudget
			owner := f.carrier.Owner
			command := agentproto.Command{CommandID: "pause-" + mutation, Kind: agentproto.CommandThreadGoalSet, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}, Goal: agentproto.GoalCommand{Status: "paused", Objective: expected.Objective, TokenBudget: &budget, Expected: expected}}
			switch mutation {
			case "version":
				expected.GoalVersion++
			case "session":
				command.Target.ThreadID = "other"
			case "objective":
				command.Goal.Objective = "replacement goal"
			case "budget":
				budget++
			case "active":
				command.Goal.Status = "active"
			}
			prepareOwnerGoal(t, f, command)
			if err := f.coordinator.RegisterCommand(t.Context(), "inc-1", command); err == nil {
				t.Fatal("unsafe owner mutation accepted")
			}
			f.tx(func(tx *workstore.Tx) error {
				_, e := tx.GetNativeCommand(command.CommandID)
				if !errors.Is(e, workstore.ErrNotFound) {
					t.Fatal("unsafe mutation registered for native dispatch")
				}
				return nil
			})
		})
	}
}

func queueFrozenResume(t *testing.T, f *fixture) workstore.SessionGoalIntent {
	t.Helper()
	expected := freezeIdleGoal(t, f)
	budget := expected.TokenBudget
	var intent workstore.SessionGoalIntent
	f.tx(func(tx *workstore.Tx) error {
		raw, e := tx.GetIngressByPlatformMessage("tenant", "private-chat", "om_user")
		if e != nil {
			return e
		}
		scope, e := tx.GetScope("scope")
		if e != nil {
			return e
		}
		if e = tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "resume-human", IngressID: raw.ID, ScopeID: "scope", ContextID: "context", PrincipalID: scope.PrincipalID, RoleID: "role", GatewayRef: "gateway", Text: "/goal resume", State: "sending", Revision: 1}, 0); e != nil {
			return e
		}
		intent, _, e = tx.QueueSessionGoalIntent("resume-intent", "context", agentproto.GoalCommand{Status: "active", Objective: expected.Objective, TokenBudget: &budget, Expected: expected}, workstore.PrivateActionSource{PersonalScopeID: "scope", CommandID: "resume-human"}, f.now)
		return e
	})
	return intent
}

func resumeClaim(t *testing.T, f *fixture, intent workstore.SessionGoalIntent) (workstore.ClaimRequest, agentproto.Command) {
	t.Helper()
	binding := f.carrier.Owner
	binding.AttemptID = "resume-attempt"
	binding.RunRef = "resume-run"
	command := agentproto.Command{CommandID: "resume-start", Kind: agentproto.CommandPromptSend, Execution: &binding, Target: agentproto.Target{ThreadID: "session"}, InstructionRevision: "v1", Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "Resume exact existing goal"}}}}
	var request workstore.ClaimRequest
	f.tx(func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		op, e := f.operation(tx, command)
		if e != nil {
			return e
		}
		request = workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: binding.AttemptID, ScopeID: "scope", ContextID: "context", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: binding.RunRef}, State: work.AttemptDispatchPending, InputIDs: []string{intent.InputID}, CommandID: command.CommandID, Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance-1", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: f.now}
		return nil
	})
	return request, command
}

func TestIdleGoalResumePinsExistingGoalBeforeClaimAndNativePrompt(t *testing.T) {
	for _, phase := range []string{"before-claim", "rebuilt-before-claim", "before-prompt", "unchanged"} {
		t.Run(phase, func(t *testing.T) {
			f := idleKnownGoal(t, "paused")
			intent := queueFrozenResume(t, f)
			request, command := resumeClaim(t, f, intent)
			change := func() {
				f.tx(func(tx *workstore.Tx) error {
					grant, e := tx.GetContinuationGrant("inc-1", "session")
					if e != nil {
						return e
					}
					grant.GoalVersion++
					return tx.PutContinuationGrant(grant)
				})
			}
			if phase == "before-claim" {
				change()
			}
			if phase == "rebuilt-before-claim" {
				f.tx(func(tx *workstore.Tx) error {
					worker, e := tx.GetWorker("worker")
					if e != nil {
						return e
					}
					before := worker.Revision
					worker.Generation++
					worker.Revision++
					return tx.PutWorker(worker, before)
				})
			}
			err := f.store.Within(t.Context(), func(tx *workstore.Tx) error { return tx.ClaimAttempt(request) })
			if phase == "before-claim" || phase == "rebuilt-before-claim" {
				if err == nil {
					t.Fatal("stale resume claimed execution")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if phase == "before-prompt" {
				change()
			}
			err = f.coordinator.RegisterCommand(context.Background(), "inc-1", command)
			if phase == "before-prompt" {
				if err == nil {
					t.Fatal("stale resume reached native prompt")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: command.CommandID, ThreadID: "session", TurnID: "resume-turn"}); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				saved, e := tx.GetSessionGoalIntent(intent.ID)
				if e != nil {
					return e
				}
				if saved.CommandID == "" || saved.AttemptID != "resume-attempt" {
					t.Fatal("resume did not bind to explicit bounded attempt")
				}
				body, e := tx.GetOperation(saved.CommandID)
				if e != nil {
					return e
				}
				goal, e := tx.NativeCommandPayload(body.ID, "inc-1")
				if e != nil {
					return e
				}
				if goal.Goal.Expected == nil || goal.Goal.Status != "active" || goal.Goal.Objective != intent.Goal.Objective || *goal.Goal.TokenBudget != *intent.Goal.TokenBudget || goal.Target.ThreadID != "session" {
					t.Fatal("resume substituted another goal/session")
				}
				return nil
			})
		})
	}
}

func TestIdlePauseNotificationBeforeRPCOnlyConfirmsMatchingSnapshot(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "newer-goal"}[changed], func(t *testing.T) {
			f := idleKnownGoal(t, "active")
			expected := freezeIdleGoal(t, f)
			budget := expected.TokenBudget
			owner := f.carrier.Owner
			command := agentproto.Command{CommandID: "notified-pause", Kind: agentproto.CommandThreadGoalSet, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}, Goal: agentproto.GoalCommand{Status: "paused", Objective: expected.Objective, TokenBudget: &budget, Expected: expected}}
			prepareOwnerGoal(t, f, command)
			if err := f.coordinator.RegisterCommand(t.Context(), "inc-1", command); err != nil {
				t.Fatal(err)
			}
			if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalUpdated, ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: expected.Objective, Status: "paused", TokenBudget: &budget, UpdatedAt: 43}}); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(command.CommandID)
				if e != nil {
					return e
				}
				if op.State != work.OperationSending {
					t.Fatal("notification confirmed RPC without correlation")
				}
				if _, e = tx.GetRuntimeGate("worker"); !errors.Is(e, workstore.ErrNotFound) {
					t.Fatal("idle paused notification opened false recovery gate")
				}
				if changed {
					grant, e := tx.GetContinuationGrant("inc-1", "session")
					if e != nil {
						return e
					}
					grant.Objective = "newer goal"
					grant.GoalVersion = 44
					return tx.PutContinuationGrant(grant)
				}
				return nil
			})
			err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "correlated-rpc", Execution: &owner, ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: expected.Objective, Status: "paused", TokenBudget: &budget, UpdatedAt: 43}})
			if (err != nil) != changed {
				t.Fatalf("pause receipt error=%v changed=%v", err, changed)
			}
		})
	}
}

func TestIdleGoalClearCannotBeResumedAndCancelsQueuedResume(t *testing.T) {
	f := idleKnownGoal(t, "paused")
	intent := queueFrozenResume(t, f)
	owner := f.carrier.Owner
	command := agentproto.Command{CommandID: "idle-clear", Goal: agentproto.GoalCommand{Expected: freezeIdleGoal(t, f)}, Kind: agentproto.CommandThreadGoalClear, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}}
	prepareOwnerGoal(t, f, command)
	if err := f.coordinator.RegisterCommand(t.Context(), "inc-1", command); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command.CommandID, RequestID: "clear-rpc", Execution: &owner, ThreadID: "session", GoalCleared: true}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		grant, e := tx.GetContinuationGrant("inc-1", "session")
		if e != nil {
			return e
		}
		if grant.Active || grant.Status != "cleared" || grant.GoalVersion != 0 || grant.Objective != "" || grant.TokenBudget != 0 {
			t.Fatalf("clear left resumable goal: %+v", grant)
		}
		rejected, e := tx.CancelStaleSessionGoalResumes(f.now)
		if e != nil {
			return e
		}
		if len(rejected) != 1 || rejected[0].ID != intent.ID || rejected[0].RejectionReason == "" {
			t.Fatalf("stale queued resume not terminally rejected: %+v", rejected)
		}
		input, e := tx.GetInbox(intent.InputID)
		if e != nil {
			return e
		}
		if input.State != work.InboxCancelled {
			t.Fatal("stale goal remains pending")
		}
		again, e := tx.CancelStaleSessionGoalResumes(f.now)
		if e == nil && len(again) != 0 {
			t.Fatal("stale rejection repeated")
		}
		return e
	})
}
