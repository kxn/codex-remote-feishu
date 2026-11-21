package workengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPreferenceChangeBetweenPlanAndClaimKeepsInputPendingUntilReplanned(t *testing.T) {
	ctx := t.Context()
	s, _ := fixture(t)
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "leader", RoleID: "leader", BotGeneration: 1}
	var menu workstore.MenuInteraction
	transaction(t, s, func(tx *workstore.Tx) error {
		var err error
		menu, _, err = tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{
			GatewayRef: "gateway", AppID: "leader", Tenant: "tenant", EventID: "menu", EventKey: "menu",
			SenderIDs: work.SenderIDs{OpenID: "person"}, ReceivedAt: testNow,
		}, gateway)
		if err != nil {
			return err
		}
		sending, _, err := tx.BeginMenuDestination(menu.ID, menu.Revision, "SDK destination receipt")
		if err != nil {
			return err
		}
		if err = tx.FinishMenuDestination(menu.ID, sending.Revision, "private-chat", "entry-message", "", true); err != nil {
			return err
		}
		return tx.PutWorker(work.WorkerSession{
			ID: "worker", ContextID: menu.ContextID, ScopeID: menu.ScopeID, RoleID: gateway.RoleID, Backend: work.BackendCodex,
			State: work.WorkerReady, Generation: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1,
			CarrierIncarnation: "carrier", Controller: work.ControllerAutomatic, ControllerGeneration: 1, Revision: 1,
		}, 0)
	})
	enqueue(t, s, "input", menu.ScopeID, gateway.RoleID, "", menu.ContextID)
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, s, 1, planner, dispatcher)
	candidates, _, _, err := e.snapshot(ctx)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate count=%d err=%v", len(candidates), err)
	}
	candidate := candidates[0]
	stale, err := planner.Plan(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	source := workstore.PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID}
	var preferences work.ContextPreferences
	transaction(t, s, func(tx *workstore.Tx) error {
		_, ref, err := tx.ValidatePrivateAction(source)
		if err != nil {
			return err
		}
		access := agentproto.AccessModeConfirm
		return tx.PutContextPreferences(work.ContextPreferences{
			ContextID: menu.ContextID, ScopeID: menu.ScopeID, AccessMode: &access,
			UpdatedBy: work.Actor{Kind: work.ActorPrincipal, ID: menu.PrincipalID},
			Source:    ref, Revision: 1, UpdatedAt: testNow,
		}, 0, source)
	})
	if _, err = e.admit(ctx, candidate, stale); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("stale plan acquired new input: %v", err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending {
			t.Fatal("preference race consumed input")
		}
		preferences, err = tx.GetContextPreferences(menu.ContextID)
		return err
	})
	fresh, err := planner.Plan(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	fresh.PreferencesRevision = preferences.Revision
	fresh.Command.Overrides.AccessMode = *preferences.AccessMode
	pending, err := e.admit(ctx, candidate, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if outcome := e.send(ctx, pending); outcome.Err != nil || outcome.State != work.OperationConfirmed {
		t.Fatalf("fresh plan did not start: %+v", outcome)
	}
	if dispatcher.count() != 1 || dispatcher.calls[0].command.Overrides.AccessMode != agentproto.AccessModeConfirm {
		t.Fatal("replanned input did not use the current context preferences")
	}
}

func TestSharedCarrierCannotReceiveTwoClaims(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker:a:expert")
		if err != nil {
			return err
		}
		leader, err := tx.GetWorker("worker:a:leader")
		if err != nil {
			return err
		}
		worker.InstanceID = leader.InstanceID
		worker.CarrierIncarnation = leader.CarrierIncarnation
		worker.Generation++
		worker.Revision++
		return tx.PutWorker(worker, 1)
	})
	enqueue(t, s, "leader", "a", "leader", "", "")
	enqueue(t, s, "expert", "a", "expert", "", "")
	dispatcher := &testDispatcher{}
	r, err := engine(t, s, 2, &testPlanner{}, dispatcher).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 1 || dispatcher.count() != 1 {
		t.Fatalf("shared carrier overcommit %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox("a")
		if err != nil {
			return err
		}
		pending := 0
		for _, input := range inputs {
			if input.State == work.InboxPending {
				pending++
			}
		}
		if pending != 1 {
			t.Fatalf("rejected claim consumed inbox: %+v", inputs)
		}
		return nil
	})
}
func TestBusyWorkstreamKeepsLateInputForNextAttempt(t *testing.T) {
	s, _ := fixture(t, "a")
	enqueue(t, s, "first", "a", "leader", "", "")
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, s, 2, planner, dispatcher)
	first, err := e.Tick(t.Context())
	if err != nil || len(first.Dispatched) != 1 {
		t.Fatalf("initial tick %+v %v", first, err)
	}
	enqueue(t, s, "late", "a", "leader", "", "")
	second, err := e.Tick(t.Context())
	if err != nil || len(second.Dispatched) != 0 {
		t.Fatalf("busy run duplicated %+v %v", second, err)
	}
	finish(t, s, first.Dispatched[0].AttemptID)
	third, err := e.Tick(t.Context())
	if err != nil || len(third.Dispatched) != 1 {
		t.Fatalf("late input not resumed %+v %v", third, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(third.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if len(attempt.InputIDs) != 1 || attempt.InputIDs[0] != "late" {
			t.Fatalf("wrong frozen input set %+v", attempt.InputIDs)
		}
		return nil
	})
}

type testProvisioner struct{ needs chan WorkerNeed }

func (p *testProvisioner) Prepare(_ context.Context, need WorkerNeed) error {
	p.needs <- need
	return nil
}
func TestMissingTaskWorkerStaysPendingAndRequestsPreparation(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		return tx.PutTask(work.Task{ID: "task", ScopeID: "a", Title: "independent", OwnerRoleID: "expert", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0)
	})
	enqueue(t, s, "task-input", "a", "expert", "task", "")
	provisioner := &testProvisioner{needs: make(chan WorkerNeed, 4)}
	dispatcher := &testDispatcher{}
	e, err := New(s, Config{MaxConcurrent: 2, Now: func() time.Time { return testNow }, Provisioner: provisioner}, &testPlanner{}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Tick(t.Context())
	if err != nil || len(r.Dispatched) != 0 {
		t.Fatalf("missing worker %+v %v", r, err)
	}
	select {
	case need := <-provisioner.needs:
		if need.TaskID != "task" || dispatcher.count() != 0 {
			t.Fatal("task collapsed into role main worker")
		}
	case <-time.After(time.Second):
		t.Fatal("missing task did not receive bounded asynchronous preparation")
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("task-input")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending {
			t.Fatal("provisioning falsely consumed input")
		}
		return nil
	})
}
func TestPauseIsRecheckedAfterPlanning(t *testing.T) {
	s, _ := fixture(t, "a")
	enqueue(t, s, "input", "a", "leader", "", "")
	planner := &testPlanner{hook: func(Candidate) {
		transaction(t, s, func(tx *workstore.Tx) error {
			return tx.PutPauseGate(work.PauseGate{ID: "pause", ScopeID: "a", Source: work.GateProject, SourceID: "human", State: work.GateClosed, Revision: 1}, 0)
		})
	}}
	dispatcher := &testDispatcher{}
	r, err := engine(t, s, 2, planner, dispatcher).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 0 || dispatcher.count() != 0 {
		t.Fatalf("pause lost between plan and admission %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		operations, err := tx.ListOperations("a")
		if err != nil {
			return err
		}
		if len(operations) != 0 {
			t.Fatal("failed paused admission leaked command")
		}
		return nil
	})
}
func TestQueueCarriesExplicitTaskInsteadOfParsingEventText(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		input := work.Inbox{ID: "event-input", ScopeID: "a", RecipientRoleID: "leader", EventID: "task:missing-child:do-not-parse", State: work.InboxPending, Revision: 1}
		return tx.PutInbox(input, 0)
	})
	r, err := engine(t, s, 1, &testPlanner{}, &testDispatcher{}).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 1 {
		t.Fatalf("event text accidentally parsed as task %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(r.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if attempt.TaskID != "" {
			t.Fatal("event ID fabricated task linkage")
		}
		return nil
	})
}

func TestAdmissionRefreshesRoleSettingsAfterPlanningBeforeClaim(t *testing.T) {
	ctx := t.Context()
	store, _ := fixture(t, "a")
	enqueue(t, store, "input", "a", "leader", "", "context:a:leader")
	transaction(t, store, func(tx *workstore.Tx) error {
		return tx.SyncRoleSettingsReference("leader", "planned")
	})
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, store, 1, planner, dispatcher)
	candidates, _, _, err := e.snapshot(ctx)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate count=%d err=%v", len(candidates), err)
	}
	candidate := candidates[0]
	plan, err := planner.Plan(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an authoritative profile write after Plan, before the periodic
	// projection. The admission guard refreshes the role before the claim.
	e.config.SettingsAdmission = func(_ context.Context, roleID string, commit func() error) error {
		if roleID != candidate.Role.ID {
			t.Fatalf("admission role = %q, want %q", roleID, candidate.Role.ID)
		}
		transaction(t, store, func(tx *workstore.Tx) error {
			return tx.SyncRoleSettingsReference(roleID, "changed")
		})
		return commit()
	}
	if _, err := e.admit(ctx, candidate, plan); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("stale role plan acquired input: %v", err)
	}
	transaction(t, store, func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending || dispatcher.count() != 0 {
			t.Fatal("role settings race consumed input")
		}
		return nil
	})
}

func TestRecoveringWorkerWithUnsentLaunchCanReenterPreparation(t *testing.T) {
	ctx := t.Context()
	store, _ := fixture(t, "a")
	enqueue(t, store, "input", "a", "leader", "", "context:a:leader")
	transaction(t, store, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker:a:leader")
		if err != nil {
			return err
		}
		worker.State = work.WorkerRecovering
		worker.Revision++
		if err := tx.PutWorker(worker, worker.Revision-1); err != nil {
			return err
		}
		content := work.NewContent("prepared-content", worker.ScopeID, "application/json", []byte("{}"), testNow)
		if err := tx.PutContent(content); err != nil {
			return err
		}
		_, _, err = tx.PutOperation(work.Operation{
			ID: "prepared-launch", Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"},
			ScopeID: worker.ScopeID, ContextID: worker.ContextID, Kind: "worker.launch",
			TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: "prepared-launch",
			RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared,
			Revision: 1, CreatedAt: testNow, UpdatedAt: testNow,
		}, 0)
		return err
	})
	engine := engine(t, store, 1, &testPlanner{}, &testDispatcher{})
	candidates, _, needs, err := engine.snapshot(ctx)
	if err != nil || len(candidates) != 0 || len(needs) != 1 || needs[0].ContextID != "context:a:leader" {
		t.Fatalf("unsent launch was not eligible for preparation: candidates=%d needs=%+v err=%v", len(candidates), needs, err)
	}
}
