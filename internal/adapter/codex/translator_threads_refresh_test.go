package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslateThreadsRefreshBuildsSnapshotWithoutReadsWhenListIsRichEnough(t *testing.T) {
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

	refreshed, err := tr.ObserveServer([]byte(`{"id":"relay-threads-refresh-0","result":{"data":[{"id":"thread-2","cwd":"/data/dl/droid","preview":"整理日志","status":{"type":"notLoaded"}},{"id":"thread-1","name":"修复登录流程","cwd":"/data/dl/droid","preview":"修登录","state":"idle"}]}}`))
	if err != nil {
		t.Fatalf("observe thread/list response: %v", err)
	}
	if !refreshed.Suppress {
		t.Fatalf("expected owned refresh response to stay suppressed, got %#v", refreshed)
	}
	if len(refreshed.OutboundToCodex) != 0 {
		t.Fatalf("expected rich thread/list result to avoid thread/read followups, got %#v", refreshed.OutboundToCodex)
	}
	if len(refreshed.Events) != 1 {
		t.Fatalf("expected immediate snapshot event, got %#v", refreshed.Events)
	}

	event := refreshed.Events[0]
	if event.Kind != agentproto.EventThreadsSnapshot || len(event.Threads) != 2 {
		t.Fatalf("unexpected snapshot event: %#v", event)
	}
	if event.Threads[0].ThreadID != "thread-2" || event.Threads[0].RuntimeStatus == nil || event.Threads[0].RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeNotLoaded {
		t.Fatalf("expected thread-2 runtime status to come directly from thread/list, got %#v", event.Threads[0])
	}
	if event.Threads[1].ThreadID != "thread-1" || event.Threads[1].State != "idle" || event.Threads[1].Name != "修复登录流程" {
		t.Fatalf("expected thread-1 details to come directly from thread/list, got %#v", event.Threads[1])
	}
}

func TestTranslateThreadsRefreshJoinsClientOwnedInflightThreadList(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.ObserveClient([]byte(`{"id":"codex.chatSessionProvider:0","method":"thread/list","params":{"limit":50}}`)); err != nil {
		t.Fatalf("observe client thread/list: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected startup refresh to borrow client thread/list, got %#v", commands)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"codex.chatSessionProvider:0","result":{"data":[{"id":"thread-1","name":"修复登录流程","cwd":"/data/dl/droid","preview":"修登录","state":"idle"}]}}`))
	if err != nil {
		t.Fatalf("observe borrowed thread/list response: %v", err)
	}
	if refreshed.Suppress {
		t.Fatalf("expected borrowed client thread/list response to keep flowing to parent, got %#v", refreshed)
	}
	if len(refreshed.OutboundToCodex) != 0 {
		t.Fatalf("expected borrowed rich thread/list result to avoid followups, got %#v", refreshed.OutboundToCodex)
	}
	if len(refreshed.Events) != 1 {
		t.Fatalf("expected snapshot event from borrowed thread/list, got %#v", refreshed.Events)
	}
	event := refreshed.Events[0]
	if event.Kind != agentproto.EventThreadsSnapshot || len(event.Threads) != 1 {
		t.Fatalf("unexpected borrowed snapshot event: %#v", event)
	}
	if event.Threads[0].ThreadID != "thread-1" || event.Threads[0].Name != "修复登录流程" || event.Threads[0].State != "idle" {
		t.Fatalf("unexpected borrowed snapshot record: %#v", event.Threads[0])
	}
}

func TestTranslateThreadsRefreshNativeResponseSatisfiesLaterClientAliases(t *testing.T) {
	tr := NewTranslator("inst-1")
	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native refresh request, got %#v", commands)
	}

	joined, err := tr.ObserveClient([]byte(`{"id":"codex.chatSessionProvider:0","method":"thread/list","params":{"limit":50,"cursor":null,"sortKey":"created_at","modelProviders":null,"archived":false,"sourceKinds":[]}}`))
	if err != nil {
		t.Fatalf("observe joined thread/list: %v", err)
	}
	if !joined.Suppress {
		t.Fatalf("expected matching client thread/list to join native inflight request, got %#v", joined)
	}

	secondJoined, err := tr.ObserveClient([]byte(`{"id":"codex.chatSessionProvider:1","method":"thread/list","params":{"limit":50,"cursor":null,"sortKey":"created_at","modelProviders":null,"archived":false,"sourceKinds":[]}}`))
	if err != nil {
		t.Fatalf("observe second joined thread/list: %v", err)
	}
	if !secondJoined.Suppress {
		t.Fatalf("expected second matching client thread/list to join native inflight request, got %#v", secondJoined)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"relay-threads-refresh-0","result":{"data":[{"id":"thread-1","name":"修复登录流程","cwd":"/data/dl/droid","preview":"修登录","state":"idle"}]}}`))
	if err != nil {
		t.Fatalf("observe native refresh response: %v", err)
	}
	if !refreshed.Suppress {
		t.Fatalf("expected native refresh owner response to stay suppressed, got %#v", refreshed)
	}
	if len(refreshed.Events) != 1 {
		t.Fatalf("expected snapshot event from native refresh response, got %#v", refreshed.Events)
	}
	if len(refreshed.OutboundToParent) != 2 {
		t.Fatalf("expected native refresh response to satisfy two client aliases, got %#v", refreshed.OutboundToParent)
	}

	var alias map[string]any
	if err := json.Unmarshal(refreshed.OutboundToParent[0], &alias); err != nil {
		t.Fatalf("unmarshal first alias response: %v", err)
	}
	if alias["id"] != "codex.chatSessionProvider:0" {
		t.Fatalf("unexpected first alias id: %#v", alias)
	}
	if err := json.Unmarshal(refreshed.OutboundToParent[1], &alias); err != nil {
		t.Fatalf("unmarshal second alias response: %v", err)
	}
	if alias["id"] != "codex.chatSessionProvider:1" {
		t.Fatalf("unexpected second alias id: %#v", alias)
	}
}

func TestThreadListCoalescingKeepsDifferentSortKeysSeparate(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native refresh request, got %#v", commands)
	}

	separate, err := tr.ObserveClient([]byte(`{"id":"CodexWebviewProvider.webview:1","method":"thread/list","params":{"limit":50,"cursor":null,"sortKey":"updated_at","modelProviders":null,"archived":false,"sourceKinds":[]}}`))
	if err != nil {
		t.Fatalf("observe updated_at thread/list: %v", err)
	}
	if separate.Suppress {
		t.Fatalf("expected updated_at thread/list to stay separate from created_at refresh, got %#v", separate)
	}

	joined, err := tr.ObserveClient([]byte(`{"id":"codex.chatSessionProvider:0","method":"thread/list","params":{"limit":50,"cursor":null,"sortKey":"created_at","modelProviders":null,"archived":false,"sourceKinds":[]}}`))
	if err != nil {
		t.Fatalf("observe created_at thread/list: %v", err)
	}
	if !joined.Suppress {
		t.Fatalf("expected created_at thread/list to join native refresh request, got %#v", joined)
	}

	if tr.threadListBroker.InflightCount() != 2 {
		t.Fatalf("expected separate inflight groups for created_at and updated_at, got %d", tr.threadListBroker.InflightCount())
	}
}
