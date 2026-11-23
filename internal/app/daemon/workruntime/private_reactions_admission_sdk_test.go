package workruntime

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateReactionSDKSteerAcknowledgesOnlyExactConfirmedAdmission(t *testing.T) {
	for _, status := range []string{"accepted", "failed", "unknown"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			sdk := newReactionSDK(t)
			f.start()
			command := beginAuthenticatedPrivateAdmission(t, f)
			reactionRun(t, f, sdk, f.store)
			if !sdk.own("OneSecond") || sdk.own("THUMBSUP") {
				t.Fatal("unconfirmed steer was acknowledged")
			}
			event := agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: command.CommandID, RequestID: "private-steer-rpc", ThreadID: "session", TurnID: "turn-1", Status: status, Execution: command.Execution}
			if status != "accepted" {
				event.Kind = agentproto.EventNativeTurnInputRejected
				event.ErrorMessage = "actual native result"
			}
			if e := f.handle(event); e != nil {
				t.Fatal(e)
			}
			f.now = f.now.Add(2 * time.Second)
			reactionRun(t, f, sdk, f.store)
			if sdk.own("THUMBSUP") != (status == "accepted") {
				t.Fatal("steer reaction does not match exact receipt")
			}
			if sdk.own("OneSecond") != (status != "accepted") {
				t.Fatal("rejected/unknown steer lost pending presentation")
			}
			if !sdk.own("THINKING") {
				t.Fatal("steer modified original executing source")
			}
		})
	}
}

func TestPrivateReactionSDKStagedImageClearsQueueWhileTextOwnsThinking(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true)
	sdk := newReactionSDK(t)
	f.start()
	f.report()
	if e := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); e != nil {
		t.Fatal(e)
	}
	reactionRun(t, f, sdk, f.store)
	queueReactionInput(t, f, "staged-image", true)
	reactionRun(t, f, sdk, f.store)
	if !sdk.own("OneSecond") || sdk.own("THINKING") {
		t.Fatal("staged image started a turn")
	}
	queueReactionInput(t, f, "image-instruction", false)
	f.tx(func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		body := work.NewContent("media-start-body", "scope", "application/json", []byte(`{"read":"fixture"}`), f.now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		operation := work.Operation{ID: "media-command", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorSystem, ID: "fixture"}, Kind: "native.prompt.send", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: "media-command", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "media-attempt", ScopeID: "scope", ContextID: "context", RunRef: work.RunRef{WorkerID: worker.ID, Generation: worker.Generation, RunID: "media-run"}, InputIDs: []string{"staged-image", "image-instruction"}, CommandID: operation.ID, State: work.AttemptDispatchPending, Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: worker.ControllerGeneration, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration, StartOperation: operation, Now: f.now})
	})
	reactionRun(t, f, sdk, f.store)
	if sdk.own("OneSecond") || !sdk.own("THINKING") {
		t.Fatal("claimed image+text failed to advance reaction lifecycle")
	}
	sdk.mu.Lock()
	for id, item := range sdk.items {
		if item.Emoji == "THINKING" && sdk.messages[id] != "om_image-instruction" {
			t.Error("attachment rather than text owns THINKING")
		}
	}
	sdk.mu.Unlock()
	f.tx(func(tx *workstore.Tx) error {
		attempt, e := tx.GetAttempt("media-attempt")
		if e != nil {
			return e
		}
		previous := attempt.Revision
		attempt.Revision++
		attempt.State = work.AttemptInterrupted
		attempt.TerminalEvidence = []work.SourceRef{{Kind: "fixture-stop", ID: "media-stop", Version: "1", EvidenceRef: "media-start-body"}}
		return tx.PutAttempt(attempt, previous)
	})
	f.now = f.now.Add(2 * time.Second)
	reactionRun(t, f, sdk, f.store)
	if sdk.own("OneSecond") || sdk.own("THINKING") {
		t.Fatal("stop did not clear both source messages")
	}
}
