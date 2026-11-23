package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type activeSourceDirectory struct{ role string }

func (d activeSourceDirectory) ResolveGateway(context.Context, string, string) (work.IngressGateway, error) {
	return work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: d.role, BotGeneration: 1}, nil
}
func (d activeSourceDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return d.role, nil
}
func (d activeSourceDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "", nil
}

// The source is a real prepared carrier with a durably claimed input. An
// inspection without the old turn's terminal proof must never complete it.
func activePersonalSourceFixture(t *testing.T) (*App, *workstore.Store, work.ExecutionAttempt, time.Time) {
	t.Helper()
	a, store, _ := shutdownFixtureWithWitness(t, true, true)
	r := a.teamRuntime
	r.runCancel()
	r.stops = workcontrol.NewStops(store, r.driver)
	ctx := context.Background()
	now := time.Now().UTC()
	ingress, e := workingress.New(store, workingress.Config{Directory: activeSourceDirectory{role: teamID("role", "gateway")}, Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"preserve this"}`, ReceivedAt: now}
	if _, e = ingress.Receive(ctx, raw); e != nil {
		t.Fatal(e)
	}
	result, e := ingress.ProcessPending(ctx, 10)
	if e != nil || len(result.Failures) != 0 {
		t.Fatalf("source SDK ingress: %v %v", result.Failures, e)
	}
	var attempt work.ExecutionAttempt
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		workers, e := tx.ListWorkers("scope")
		if e != nil {
			return e
		}
		if len(workers) != 1 {
			t.Fatal("missing prepared worker")
		}
		w := workers[0]
		inputs, e := tx.ListInbox("scope")
		if e != nil {
			return e
		}
		var input work.Inbox
		for _, candidate := range inputs {
			message, e := tx.GetMessage(candidate.MessageID)
			if e != nil {
				return e
			}
			if message.PlatformKey != nil && message.PlatformKey.MessageID == "message" {
				input = candidate
				break
			}
		}
		if input.ID == "" {
			t.Fatal("processed SDK source input missing")
		}

		attempt = work.ExecutionAttempt{ID: "attempt", ContextID: w.ContextID, ScopeID: "scope", RunRef: work.RunRef{WorkerID: w.ID, Generation: w.Generation, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{input.ID}, CommandID: "start", Revision: 1, CreatedAt: now, UpdatedAt: now}
		op := work.Operation{ID: "start", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorRole, ID: w.RoleID}, ContextID: w.ContextID, Kind: "start", TargetID: w.ID, TargetGeneration: w.Generation, IdempotencyKey: "start", RequestHash: "hash", State: work.OperationPrepared, Revision: 1}
		claim := workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: w.Revision, ControllerGeneration: w.ControllerGeneration, InstanceID: w.InstanceID, InstanceGeneration: w.InstanceGeneration, ProcessGeneration: w.ProcessGeneration, LocationGeneration: w.LocationGeneration, StartOperation: op, Now: now}
		if e = tx.ClaimAttempt(claim); e != nil {
			return e
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	}); err != nil {
		t.Fatal(err)
	}
	return a, store, attempt, now
}
func TestPersonalDrainActiveAttemptKeepsUnverifiedInput(t *testing.T) {
	a, store, attempt, now := activePersonalSourceFixture(t)
	r := a.teamRuntime
	ctx := context.Background()
	_, _ = r.advancePersonalSourceDrain(ctx, "scope", "migration", now)
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetAttempt(attempt.ID)
		if e != nil {
			return e
		}
		if current.State == work.AttemptCompleted {
			t.Fatal("unverified attempt fabricated completed")
		}
		input, e := tx.GetInbox(attempt.InputIDs[0])
		if e != nil {
			return e
		}
		if input.State != work.InboxClaimed {
			t.Fatal("claimed source input was lost or replayed")
		}
		status, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if status.UnsettledAttempts == 0 && status.HeldExecutionAttempts == 0 {
			t.Fatal("unverified execution disappeared from drain accounting")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
