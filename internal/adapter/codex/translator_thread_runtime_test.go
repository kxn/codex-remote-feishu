package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerThreadStartedCarriesRuntimeStatus(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","cwd":"/tmp/project","status":{"type":"active","activeFlags":["waitingOnApproval"]}}}}`))
	if err != nil {
		t.Fatalf("observe thread/started: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one thread discovered event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventThreadDiscovered || event.ThreadID != "thread-1" {
		t.Fatalf("unexpected thread discovered event: %#v", event)
	}
	if event.RuntimeStatus == nil || event.RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeActive {
		t.Fatalf("expected active runtime status, got %#v", event.RuntimeStatus)
	}
	if !event.RuntimeStatus.HasFlag(agentproto.ThreadActiveFlagWaitingOnApproval) {
		t.Fatalf("expected waitingOnApproval active flag, got %#v", event.RuntimeStatus)
	}
	if !event.Loaded || event.Status != "running" {
		t.Fatalf("expected loaded running thread start, got %#v", event)
	}
}

func TestObserveServerThreadStatusChangedProducesRuntimeStatusEvent(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"thread/status/changed","params":{"threadId":"thread-1","status":{"type":"active","activeFlags":["waitingOnUserInput"]}}}`))
	if err != nil {
		t.Fatalf("observe thread/status/changed: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one runtime status event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventThreadRuntimeStatusUpdated || event.ThreadID != "thread-1" {
		t.Fatalf("unexpected runtime status event: %#v", event)
	}
	if event.RuntimeStatus == nil || event.RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeActive {
		t.Fatalf("expected active runtime status, got %#v", event.RuntimeStatus)
	}
	if !event.RuntimeStatus.HasFlag(agentproto.ThreadActiveFlagWaitingOnUserInput) {
		t.Fatalf("expected waitingOnUserInput active flag, got %#v", event.RuntimeStatus)
	}
	if !event.Loaded || event.Status != "running" {
		t.Fatalf("expected running loaded runtime update, got %#v", event)
	}
}

func TestTranslateThreadsRefreshParsesStructuredRuntimeStatus(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native command, got %d", len(commands))
	}

	var list map[string]any
	if err := json.Unmarshal(commands[0], &list); err != nil {
		t.Fatalf("unmarshal thread/list: %v", err)
	}
	if list["method"] != "thread/list" {
		t.Fatalf("expected thread/list refresh, got %#v", list)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"relay-threads-refresh-0","result":{"data":[{"id":"thread-2","preview":"整理日志","status":{"type":"notLoaded"}},{"id":"thread-1","name":"修复登录流程","preview":"修登录","status":{"type":"active","activeFlags":["waitingOnApproval"]}}]}}`))
	if err != nil {
		t.Fatalf("observe thread/list response: %v", err)
	}
	if !refreshed.Suppress || len(refreshed.OutboundToCodex) != 2 {
		t.Fatalf("expected suppressed thread/read followups, got %#v", refreshed)
	}

	firstRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-1","result":{"thread":{"id":"thread-2","cwd":"/data/dl/droid","status":{"type":"notLoaded"}}}}`))
	if err != nil {
		t.Fatalf("observe first thread/read: %v", err)
	}
	if !firstRead.Suppress || len(firstRead.Events) != 0 {
		t.Fatalf("expected intermediate thread/read to stay suppressed, got %#v", firstRead)
	}

	secondRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-2","result":{"thread":{"id":"thread-1","cwd":"/data/dl/droid","name":"修复登录流程","preview":"修登录","status":{"type":"active","activeFlags":["waitingOnApproval"]}}}}`))
	if err != nil {
		t.Fatalf("observe second thread/read: %v", err)
	}
	if !secondRead.Suppress || len(secondRead.Events) != 1 {
		t.Fatalf("expected final snapshot event, got %#v", secondRead)
	}
	event := secondRead.Events[0]
	if event.Kind != agentproto.EventThreadsSnapshot || len(event.Threads) != 2 {
		t.Fatalf("unexpected snapshot payload: %#v", event)
	}
	if event.Threads[0].RuntimeStatus == nil || event.Threads[0].RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeNotLoaded || event.Threads[0].Loaded {
		t.Fatalf("expected thread-2 to remain visible but not loaded, got %#v", event.Threads[0])
	}
	if event.Threads[1].RuntimeStatus == nil || event.Threads[1].RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeActive {
		t.Fatalf("expected thread-1 active runtime status, got %#v", event.Threads[1])
	}
	if !event.Threads[1].RuntimeStatus.HasFlag(agentproto.ThreadActiveFlagWaitingOnApproval) || event.Threads[1].State != "running" || !event.Threads[1].Loaded {
		t.Fatalf("expected thread-1 running approval status, got %#v", event.Threads[1])
	}
}
