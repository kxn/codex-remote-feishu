package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslateReviewStartBuildsDetachedPayload(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandReviewStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-main"},
		Review: agentproto.ReviewRequest{
			Delivery: agentproto.ReviewDeliveryDetached,
			Target: agentproto.ReviewTarget{
				Kind: agentproto.ReviewTargetKindUncommittedChanges,
			},
		},
	})
	if err != nil {
		t.Fatalf("translate review start: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one review/start payload, got %#v", commands)
	}

	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal review/start payload: %v", err)
	}
	if payload["method"] != "review/start" {
		t.Fatalf("expected review/start payload, got %#v", payload)
	}
	if payload["id"] != "relay-review-start-0" {
		t.Fatalf("unexpected request id: %#v", payload["id"])
	}
	params, _ := payload["params"].(map[string]any)
	if params["threadId"] != "thread-main" || params["delivery"] != "detached" {
		t.Fatalf("unexpected review/start params: %#v", params)
	}
	target, _ := params["target"].(map[string]any)
	if target["type"] != "uncommittedChanges" {
		t.Fatalf("unexpected review target payload: %#v", target)
	}
}

func TestObserveReviewStartResultMarksDetachedReviewThreadAndTurnInitiator(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandReviewStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-main"},
		Review: agentproto.ReviewRequest{
			Delivery: agentproto.ReviewDeliveryDetached,
			Target:   agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
		},
	}); err != nil {
		t.Fatalf("translate review start: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-review-start-0","result":{"turn":{"id":"turn-review-1"},"reviewThreadId":"thread-review"}}`))
	if err != nil {
		t.Fatalf("observe review/start result: %v", err)
	}
	if !result.Suppress || len(result.Events) != 0 {
		t.Fatalf("expected suppressed review/start response, got %#v", result)
	}

	started, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-review","cwd":"/tmp/project"}}}`))
	if err != nil {
		t.Fatalf("observe review thread started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one thread discovered event, got %#v", started.Events)
	}
	event := started.Events[0]
	if event.Kind != agentproto.EventThreadDiscovered || event.ThreadID != "thread-review" {
		t.Fatalf("unexpected thread discovered event: %#v", event)
	}
	if event.Metadata["forkedFromId"] != "thread-main" {
		t.Fatalf("expected fork parent metadata, got %#v", event.Metadata)
	}
	source, ok := event.Metadata["threadSource"].(*agentproto.ThreadSourceRecord)
	if !ok || source == nil || source.Kind != agentproto.ThreadSourceKindReview || source.ParentThreadID != "thread-main" {
		t.Fatalf("expected detached review thread source metadata, got %#v", event.Metadata["threadSource"])
	}

	turnStarted, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-review","turn":{"id":"turn-review-1"}}}`))
	if err != nil {
		t.Fatalf("observe review turn started: %v", err)
	}
	if len(turnStarted.Events) != 1 {
		t.Fatalf("expected one turn started event, got %#v", turnStarted.Events)
	}
	if turnStarted.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface || turnStarted.Events[0].Initiator.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected review turn initiator: %#v", turnStarted.Events[0].Initiator)
	}
}

func TestObserveReviewLifecycleItemsNormalizeAndExtractReviewText(t *testing.T) {
	tr := NewTranslator("inst-1")

	entered, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-review","turnId":"turn-review-1","item":{"id":"review-enter","type":"enteredReviewMode","review":"变更审阅"}}}`))
	if err != nil {
		t.Fatalf("observe entered review item: %v", err)
	}
	if len(entered.Events) != 1 {
		t.Fatalf("expected one entered review event, got %#v", entered.Events)
	}
	if entered.Events[0].ItemKind != "entered_review_mode" || entered.Events[0].Metadata["review"] != "变更审阅" {
		t.Fatalf("unexpected entered review event: %#v", entered.Events[0])
	}

	exited, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-review","turnId":"turn-review-1","item":{"id":"review-exit","type":"exited_review_mode","result":{"review":"建议先拆出 translator 测试"}}}}`))
	if err != nil {
		t.Fatalf("observe exited review item: %v", err)
	}
	if len(exited.Events) != 1 {
		t.Fatalf("expected one exited review event, got %#v", exited.Events)
	}
	if exited.Events[0].ItemKind != "exited_review_mode" || exited.Events[0].Metadata["review"] != "建议先拆出 translator 测试" {
		t.Fatalf("unexpected exited review event: %#v", exited.Events[0])
	}
}

func TestTranslateThreadsRefreshKeepsReviewThreadSourceFromThreadRead(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate threads refresh: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one thread/list request, got %#v", commands)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"relay-threads-refresh-0","result":{"data":[{"id":"thread-review","preview":"审阅中"},{"id":"thread-main","name":"主线程","preview":"主线任务"}]}}`))
	if err != nil {
		t.Fatalf("observe thread/list: %v", err)
	}
	if !refreshed.Suppress || len(refreshed.OutboundToCodex) != 2 {
		t.Fatalf("expected thread/read followups, got %#v", refreshed)
	}

	firstRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-1","result":{"thread":{"id":"thread-review","cwd":"/tmp/project","forkedFromId":"thread-main","source":{"subAgent":"review"}}}}`))
	if err != nil {
		t.Fatalf("observe review thread/read: %v", err)
	}
	if !firstRead.Suppress || len(firstRead.Events) != 0 {
		t.Fatalf("expected first thread/read to stay buffered, got %#v", firstRead)
	}

	secondRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-2","result":{"thread":{"id":"thread-main","cwd":"/tmp/project","name":"主线程","preview":"主线任务"}}}`))
	if err != nil {
		t.Fatalf("observe main thread/read: %v", err)
	}
	if !secondRead.Suppress || len(secondRead.Events) != 1 {
		t.Fatalf("expected final snapshot event, got %#v", secondRead)
	}
	snapshot := secondRead.Events[0]
	if snapshot.Kind != agentproto.EventThreadsSnapshot || len(snapshot.Threads) != 2 {
		t.Fatalf("unexpected snapshot payload: %#v", snapshot)
	}
	reviewThread := snapshot.Threads[0]
	if reviewThread.ThreadID != "thread-review" || reviewThread.ForkedFromID != "thread-main" {
		t.Fatalf("expected review thread parent to survive refresh, got %#v", reviewThread)
	}
	if reviewThread.Source == nil || reviewThread.Source.Kind != agentproto.ThreadSourceKindReview {
		t.Fatalf("expected review thread source in snapshot, got %#v", reviewThread.Source)
	}
}
