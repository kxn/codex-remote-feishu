package workruntime

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestFirstExactNativeSessionCompletesBusyWorkerIdentityWithoutRebinding(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.NativeSessionID != "session" || worker.NativeNamespace != "codex" || worker.Generation != 1 || worker.State != work.WorkerBusy {
			t.Fatalf("missing initial native identity: %+v", worker)
		}
		return nil
	})
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "command", ThreadID: "unrequested-session", TurnID: "wrong-turn"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.NativeSessionID != "session" || worker.Generation != 1 {
			t.Fatal("conflicting native session replaced the context owner")
		}
		gate, err := tx.GetRuntimeGate("worker")
		if err != nil {
			return err
		}
		if gate.Reason != "native_session_binding_conflict" {
			t.Fatal(gate.Reason)
		}
		return nil
	})
}

func TestEphemeralReviewDoesNotAdoptItsSessionAsContextHistory(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	fork := f.command
	fork.CommandID = "review-command"
	fork.Target.ExecutionMode = agentproto.PromptExecutionModeForkEphemeral
	fork.Target.SourceThreadID = "source"
	fork.Target.ThreadID = "source"
	f.registerExtra(fork)
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: fork.CommandID, ThreadID: "review-session", TurnID: "review-turn"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.NativeSessionID != "" {
			t.Fatal("ephemeral review polluted persistent context session")
		}
		return nil
	})
	f.start()
}
