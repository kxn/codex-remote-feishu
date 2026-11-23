package wrapper

import (
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"path/filepath"
	"testing"
)

func TestShellInventoryRequiresOwnActualRPCResponse(t *testing.T) {
	inventory := newWorkInspectionState()
	inventory.outbound("shell-command", []byte(`{"id":"shell-rpc","method":"thread/shellCommand","params":{"threadId":"session","command":"fixed-private-read"}}`))
	if len(inventory.pendingInputs) != 1 {
		t.Fatal("shell write absent from native inventory")
	}
	inventory.raw([]byte(`{"id":"unrelated-rpc","result":{}}`))
	if len(inventory.pendingInputs) != 1 || len(inventory.drainReceipts()) != 0 {
		t.Fatal("unrelated response acknowledged shell input")
	}
	inventory.raw([]byte(`{"id":"shell-rpc","result":{}}`))

	if len(inventory.pendingInputs) != 1 || len(inventory.drainReceipts()) != 0 {
		t.Fatal("empty shell RPC response was mistaken for completed input")
	}
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	root, cwd := t.TempDir(), t.TempDir()
	journal, err := runtimejournal.Open(t.Context(), filepath.Join(root, "inventory.db"), root, cwd, "incarnation")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err = inventory.persist(journal, owner); err != nil {
		t.Fatal(err)
	}
	restored := newWorkInspectionState()
	if err = restored.restore(journal, owner); err != nil {
		t.Fatal(err)
	}
	if len(restored.pendingInputs) != 1 {
		t.Fatal("restart lost ACK-to-item uncertainty")
	}
	restored.events([]agentproto.Event{{Kind: agentproto.EventTurnCompleted, CommandID: "shell-command", ThreadID: "session", TurnID: "model-turn"}})
	if len(restored.pendingInputs) != 1 {
		t.Fatal("ordinary model terminal cleared independent shell input")
	}
	restored.events([]agentproto.Event{{Kind: agentproto.EventNativeShellItemObserved, Action: "completed", CommandID: "another-command", ThreadID: "session", TurnID: "shell-turn", ItemID: "item"}})
	if len(restored.pendingInputs) != 1 {
		t.Fatal("another command cleared shell input")
	}
	restored.events([]agentproto.Event{{Kind: agentproto.EventNativeShellItemObserved, Action: "completed", CommandID: "shell-command", ThreadID: "session", TurnID: "shell-turn", ItemID: "item", Status: "failed"}})
	if len(restored.pendingInputs) != 0 {
		t.Fatal("actual shell end did not close native inventory")
	}
}
