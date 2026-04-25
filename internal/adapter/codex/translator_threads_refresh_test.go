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

func TestTranslateThreadsRefreshBorrowsStartupClientThreadList(t *testing.T) {
	tr := NewTranslator("inst-1")
	tr.ArmStartupThreadListBorrow()

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

func TestTranslateThreadsRefreshSkipsNativeListWhenBorrowAlreadyCompleted(t *testing.T) {
	tr := NewTranslator("inst-1")
	tr.ArmStartupThreadListBorrow()

	if _, err := tr.ObserveClient([]byte(`{"id":"codex.chatSessionProvider:0","method":"thread/list","params":{"limit":50}}`)); err != nil {
		t.Fatalf("observe client thread/list: %v", err)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"codex.chatSessionProvider:0","result":{"data":[{"id":"thread-1","name":"修复登录流程","cwd":"/data/dl/droid","preview":"修登录","state":"idle"}]}}`))
	if err != nil {
		t.Fatalf("observe borrowed thread/list response before refresh translate: %v", err)
	}
	if refreshed.Suppress || len(refreshed.Events) != 1 {
		t.Fatalf("expected borrowed response to stay visible and emit snapshot, got %#v", refreshed)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected already-satisfied startup borrow to skip native thread/list, got %#v", commands)
	}
}
