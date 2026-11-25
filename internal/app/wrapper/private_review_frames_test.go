package wrapper

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateReviewTwoNativeFramesKeepCommandEvidence(t *testing.T) {
	a := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: a.privateNative.snapshot.Owner, ExecutionID: "review-execution"}
	command := agentproto.Command{CommandID: "review", Kind: agentproto.CommandReviewStart, NativeBinding: &binding, Origin: agentproto.Origin{Surface: "surface"}, Target: agentproto.Target{ThreadID: "parent", CWD: a.privateNative.snapshot.CWD}, Review: agentproto.ReviewRequest{Delivery: agentproto.ReviewDeliveryDetached, Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}}}
	if duplicate, err := a.prepareJournalCommand(command); err != nil || duplicate {
		t.Fatalf("prepare: %v %v", duplicate, err)
	}
	result, err := a.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatalf("initial frames: %d", len(frames))
	}
	firstID := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], a.runtime); err != nil {
		t.Fatal(err)
	}
	observed, err := a.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":"child"}}}`, firstID)))
	if err != nil || len(observed.OutboundToChild) != 1 {
		t.Fatalf("child response: %+v %v", observed, err)
	}
	second := observed.OutboundToChild[0]
	var payload struct {
		Method string `json:"method"`
		Params struct {
			ThreadID string `json:"threadId"`
			Delivery string `json:"delivery"`
		} `json:"params"`
	}
	if err = json.Unmarshal(second, &payload); err != nil || payload.Method != "review/start" || payload.Params.ThreadID != "child" || payload.Params.Delivery != "inline" {
		t.Fatalf("second frame: %s %v", second, err)
	}
	receipt, err := a.runtimeCommandReceipt(command.CommandID)
	if err != nil || receipt.Stage != runtimejournal.StagePartiallyWritten || receipt.PreparedFrames != 2 || receipt.WrittenFrames != 1 || receipt.Frames[0].NativeMethod != "thread/start" || receipt.Frames[1].NativeMethod != "review/start" {
		t.Fatalf("partial receipt: %+v %v", receipt, err)
	}
	if err = writeJournaledChildFrame(io.Discard, second, a.runtime); err != nil {
		t.Fatal(err)
	}
	early, err := a.runtime.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"child","turn":{"id":"review-turn"}}}`))
	if err != nil || len(early.Events) == 0 {
		t.Fatalf("early turn: %+v %v", early, err)
	}
	if early.Events[0].NativeBinding != nil {
		t.Fatal("early turn acquired unproved binding")
	}
	if _, err = a.privateNative.journal.PersistEvents(early.Events); err != nil {
		t.Fatal(err)
	}
	if found, err := a.privateNative.journal.PrivateBindingForTurn("child", "review-turn"); err != nil || found != nil {
		t.Fatalf("early binding=%+v err=%v", found, err)
	}
	secondID := nativeFrameRequestID(second)
	accepted, err := a.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turn":{"id":"review-turn"},"reviewThreadId":"child"}}`, secondID)))
	if err != nil || len(accepted.Events) == 0 {
		t.Fatalf("review response: %+v %v", accepted, err)
	}
	if _, err = a.privateNative.journal.PersistEvents(accepted.Events); err != nil {
		t.Fatal(err)
	}
	if found, err := a.privateNative.journal.PrivateBindingForTurn("child", "review-turn"); err != nil || found == nil || *found != binding {
		t.Fatalf("proved binding=%+v err=%v", found, err)
	}
	receipt, err = a.runtimeCommandReceipt(command.CommandID)
	if err != nil || receipt.WrittenFrames != 2 {
		t.Fatalf("written receipt: %+v %v", receipt, err)
	}
	if duplicate, err := a.prepareJournalCommand(command); err != nil || !duplicate {
		t.Fatalf("duplicate: %v %v", duplicate, err)
	}
}

func TestPrivateReviewRootCompletionDoesNotFabricateSubturnTerminal(t *testing.T) {
	a := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: a.privateNative.snapshot.Owner, ExecutionID: "review"}
	command := agentproto.Command{CommandID: "review", Kind: agentproto.CommandReviewStart, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "parent"}}
	if _, err := a.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	start := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "child", TurnID: "sub", RootTurnID: "root"}
	other := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "other", TurnID: "other-turn"}
	accepted := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: "review", RequestID: "rpc", ThreadID: "parent", TurnID: "root", Action: "review.start", Status: "accepted", NativeBinding: &binding, Metadata: map[string]any{"reviewThreadID": "child"}}
	terminal := agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "child", TurnID: "root", Status: "completed"}
	tracker := newRuntimeTurnTracker()
	tracker.ObserveEvents([]agentproto.Event{start, other})
	if _, err := a.privateNative.journal.PersistEvents([]agentproto.Event{start, accepted}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.retireCompletedReviewSubturns(a.privateNative.journal); err != nil {
		t.Fatal(err)
	}
	if len(tracker.turns) != 2 {
		t.Fatal("subturn retired before root terminal")
	}
	if _, err := a.privateNative.journal.PersistEvents([]agentproto.Event{terminal}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.retireCompletedReviewSubturns(a.privateNative.journal); err != nil {
		t.Fatal(err)
	}
	out := tracker.ReconcileChildExit(nil)
	if len(out) != 1 || out[0].ThreadID != "other" || out[0].TurnID != "other-turn" {
		t.Fatalf("fabricated or lost exit: %+v", out)
	}
}
