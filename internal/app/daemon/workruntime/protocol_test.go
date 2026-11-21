package workruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func nativeRequestID(t *testing.T, frame []byte) string {
	t.Helper()
	var value struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(frame, &value); err != nil {
		t.Fatal(err)
	}
	return value.ID
}

func TestCodexProtocolNotificationBeforeResponseAndTwoTurnContinuation(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	translator := codex.NewTranslator("instance-1")
	frames, err := translator.TranslateCommand(f.command)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":"session"}}}`, nativeRequestID(t, frames[0]))))
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.OutboundToCodex) != 1 {
		t.Fatal("resume did not preserve exact turn-start followup")
	}
	observe := func(raw string) {
		t.Helper()
		result, err := translator.ObserveServer([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range result.Events {
			if event.Kind == agentproto.EventTurnStarted || event.Kind == agentproto.EventNativeTurnBindingObserved || event.Kind == agentproto.EventTurnCompleted || event.Kind == agentproto.EventThreadGoalCommandResult || event.Kind == agentproto.EventThreadGoalUpdated {
				if err := f.handle(event); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	observe(`{"method":"turn/started","params":{"threadId":"session","turn":{"id":"turn-1"}}}`)
	f.tx(func(tx *workstore.Tx) error {
		count, err := tx.CountUnresolvedNativeEvents("inc-1")
		if err != nil {
			return err
		}
		if count != 1 {
			t.Fatal("early native notification was guessed into an attempt")
		}
		return nil
	})
	observe(fmt.Sprintf(`{"id":%q,"result":{"turn":{"id":"turn-1"}}}`, nativeRequestID(t, resumed.OutboundToCodex[0])))
	f.report()
	goal := f.command
	goal.CommandID = "goal-set"
	goal.Kind = agentproto.CommandThreadGoalSet
	budget := int64(1000)
	goal.Goal = agentproto.GoalCommand{Objective: "bounded session work", Status: "active", TokenBudget: &budget}
	f.registerExtra(goal)
	frames, err = translator.TranslateCommand(goal)
	if err != nil {
		t.Fatal(err)
	}
	observe(fmt.Sprintf(`{"id":%q,"result":{"goal":{"threadId":"session","objective":"bounded session work","status":"active","tokenBudget":1000,"updatedAt":42}}}`, nativeRequestID(t, frames[0])))
	observe(`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":"turn-1","status":"completed"}}}`)
	f.tx(func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptRunning {
			t.Fatal("first native terminal ended a continuing business attempt")
		}
		return nil
	})
	observe(`{"method":"turn/started","params":{"threadId":"session","turn":{"id":"turn-2"}}}`)
	observe(`{"method":"turn/completed","params":{"threadId":"session","turn":{"id":"turn-2","status":"completed"}}}`)
	clear := goal
	clear.CommandID = "goal-clear"
	clear.Kind = agentproto.CommandThreadGoalClear
	f.registerExtra(clear)
	frames, err = translator.TranslateCommand(clear)
	if err != nil {
		t.Fatal(err)
	}
	observe(fmt.Sprintf(`{"id":%q,"result":{"cleared":true}}`, nativeRequestID(t, frames[0])))
	f.tx(func(tx *workstore.Tx) error {
		turns, err := tx.ListNativeTurns("attempt")
		if err != nil {
			return err
		}
		if len(turns) != 2 || turns[0].Binding.RunRef != "run" || turns[1].Binding.RunRef != "run" || turns[1].ContinuationAuthorization != "goal-set" {
			t.Fatal("continuation lost per-turn evidence or original run authorization")
		}
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptCompleted || worker.State != work.WorkerReady {
			t.Fatal("verified continuation stop did not reach normal ready path")
		}
		return nil
	})
}

func TestClaudeActualStreamResultUsesSyntheticTurnBinding(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	translator := claude.NewTranslator("instance-1")
	if _, err := translator.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"default"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := translator.TranslateCommand(f.command); err != nil {
		t.Fatal(err)
	}
	started, err := translator.ObserveServer([]byte(`{"type":"assistant","session_id":"session","message":{"id":"message-1","role":"assistant","content":[{"type":"text","text":"working"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range started.Events {
		if event.Kind == agentproto.EventTurnStarted {
			if err := f.handle(event); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.report()
	result, err := translator.ObserveServer([]byte(`{"type":"result","subtype":"success","session_id":"session","is_error":false,"result":"done","num_turns":1}`))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range result.Events {
		if event.Kind == agentproto.EventTurnCompleted {
			found = true
			if event.CommandID != "command" || event.ThreadID != "session" || event.TurnID == "" {
				t.Fatal("Claude native result lost exact command/synthetic-turn association")
			}
			if err := f.handle(event); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("Claude result did not produce terminal evidence")
	}
}

func TestAckLossReopenAndOldIncarnationReplayPreserveNewWorker(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	f.report()
	event := f.durable(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"})
	f.coordinator.options.SendReceipt = func(string, agentproto.EventReceipt) error { return fmt.Errorf("ACK transport lost") }
	if err := f.coordinator.HandleEvent(context.Background(), "instance-1", event); err == nil {
		t.Fatal("test did not lose ACK")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workstore.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.store = reopened
	var ackRoute string
	f.coordinator = New(reopened, Options{Driver: f.driver, Clock: func() time.Time { return f.now }, SendReceipt: func(route string, receipt agentproto.EventReceipt) error {
		ackRoute = route
		if receipt.InstanceID != route {
			t.Fatal("receipt used old origin as new transport route")
		}
		return nil
	}})
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		expected := worker.Revision
		worker.Revision++
		worker.Generation++
		worker.InstanceID = "instance-2"
		worker.InstanceGeneration++
		worker.ProcessGeneration++
		worker.CarrierIncarnation = "inc-2"
		worker.State = work.WorkerRecovering
		return tx.PutWorker(worker, expected)
	})
	newCarrier := f.carrier
	newCarrier.Incarnation = "inc-2"
	newCarrier.OriginInstanceID = "instance-2"
	newCarrier.Owner.SessionGeneration = 2
	newCarrier.InstanceGeneration = 2
	newCarrier.ProcessGeneration = 2
	if err := f.coordinator.RegisterCarrier(context.Background(), newCarrier); err != nil {
		t.Fatal(err)
	}
	var before int64
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		before = worker.Revision
		return err
	})
	if err := f.coordinator.HandleEvent(context.Background(), "instance-2", event); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.Revision != before || worker.Generation != 2 || worker.InstanceID != "instance-2" {
			t.Fatal("old incarnation replay changed new worker")
		}
		stored, err := tx.GetRuntimeEvent("inc-1", event.Seq)
		if err != nil {
			return err
		}
		if stored.Journal.PayloadHash != event.Journal.PayloadHash {
			t.Fatal("old durable identity was rewritten")
		}
		return nil
	})
	if ackRoute != "instance-2" {
		t.Fatal("old event ACK did not use current transport")
	}
}

func TestContinuationReconnectNeedsFreshNativeStateProof(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	goal := f.command
	goal.CommandID = "goal-set"
	goal.Kind = agentproto.CommandThreadGoalSet
	f.registerExtra(goal)
	budget := int64(1000)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, RequestID: "goal-set-rpc", CommandID: "goal-set", ThreadID: "session", ThreadGoal: &agentproto.ThreadGoalUpdate{Objective: "bounded", Status: "active", UpdatedAt: 42, TokenBudget: &budget}}); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.Disconnect(context.Background(), "instance-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleHello(context.Background(), f.hello()); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "session", TurnID: "unverified-continuation"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		_, err := tx.GetNativeTurn("inc-1", "session", "unverified-continuation")
		if !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("reconnect blindly restored native Goal authorization")
		}
		return nil
	})
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		observation := stoppedObservation(request)
		observation.GoalActive = true
		observation.GoalVersion = 42
		observation.ContinuationStopped = false
		observation.ContinuationEvidence = json.RawMessage(`{"id":"goal-probe","result":{"goal":{"status":"active","updatedAt":42}}}`)
		return observation, nil
	}
	if err := f.coordinator.ReconcileContinuation(context.Background(), "inc-1", "session"); err != nil {
		t.Fatal(err)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "session", TurnID: "verified-continuation"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		turn, err := tx.GetNativeTurn("inc-1", "session", "verified-continuation")
		if err != nil {
			return err
		}
		if turn.Binding.AttemptID != "attempt" || turn.ContinuationAuthorization != "goal-set" {
			t.Fatal("verified Goal state did not restore prior bounded authorization")
		}
		return nil
	})
}
