package wrapper

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivatePendingRequestInspectionUsesExactJournalSnapshot(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	app.markWorkBootstrap([]byte(`{"id":"init","method":"initialize"}`), []byte(`{"id":"init","result":{}}`))
	owner := app.privateNative.snapshot.Owner
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "exec"}
	started := agentproto.Event{Kind: agentproto.EventRequestStarted, CommandID: "source", RequestID: "req", ThreadID: "thread", TurnID: "turn", NativeBinding: &binding}
	app.inspectionState().events([]agentproto.Event{started})
	events, err := app.privateNative.journal.PersistEvents([]agentproto.Event{started})
	if err != nil {
		t.Fatal(err)
	}
	ownerBinding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	command := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &ownerBinding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: owner.InstanceGeneration, ExpectedProcessGeneration: owner.ProcessGeneration, IncludePendingRequests: true}}
	event := app.inspectNative(context.Background(), command, nil)
	inventory := event.RuntimeInspection.PendingRequests
	if inventory == nil || !inventory.Complete || len(inventory.Requests) != 1 || inventory.Requests[0].StartedHash != events[0].Journal.PayloadHash || inventory.EventSequence != events[0].Seq {
		t.Fatalf("inventory=%+v", inventory)
	}
	command.RuntimeInspection.StopBackground = true
	event = app.inspectNative(context.Background(), command, nil)
	if event.RuntimeInspection.PendingRequests != nil || event.RuntimeInspection.Problem == nil {
		t.Fatalf("stop accepted inventory: %+v", event.RuntimeInspection)
	}
}

func TestPrivateRequestAnnotationRequiresExactAcceptedTurn(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	owner := app.privateNative.snapshot.Owner
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "review-exec"}
	command := agentproto.Command{CommandID: "review", Kind: agentproto.CommandReviewStart, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "parent"}}
	if _, _, err := app.privateNative.journal.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	accepted := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: "review", RequestID: "rpc", ThreadID: "parent", TurnID: "turn", Status: "accepted", Action: "review.start", Metadata: map[string]any{"reviewThreadID": "thread"}, NativeBinding: &binding}
	if _, err := app.privateNative.journal.PersistEvents([]agentproto.Event{accepted}); err != nil {
		t.Fatal(err)
	}
	events := []agentproto.Event{{Kind: agentproto.EventRequestStarted, ThreadID: "thread", TurnID: "turn", RequestID: "request"}, {Kind: agentproto.EventRequestStarted, ThreadID: "other", TurnID: "turn", RequestID: "foreign"}}
	app.privateNative.annotate(events)
	if events[0].NativeBinding == nil || *events[0].NativeBinding != binding || events[0].CommandID != "" {
		t.Fatalf("exact turn not bound: %+v", events[0])
	}
	if events[1].NativeBinding != nil {
		t.Fatalf("unproven turn bound: %+v", events[1])
	}
}
