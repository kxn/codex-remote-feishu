package runtimejournal

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestShellInputRemainsPendingAcrossRestartUntilExactCommandRPCReceipt(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "shell-journal.db")
	store := openTestStore(t, path, root, cwd, "shell-carrier")
	command := testCommand()
	command.Kind = agentproto.CommandThreadShellCommand
	command.ShellCommand.OperationID = "independent-shell"
	command.Execution.AttemptID, command.Execution.RunRef = "", ""
	command.Target.ThreadID = "session"
	if _, _, err := store.PrepareCommand(command); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"shell-rpc","method":"thread/shellCommand","params":{"threadId":"session","command":"fixed-private-read"}}`)
	if _, err := store.PrepareFrame(command.CommandID, frame); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, path, root, cwd, "shell-carrier")
	defer store.Close()
	assertPending := func(want int) {
		t.Helper()
		actual, err := store.PendingNativeInputs(*command.Execution)
		if err != nil || actual != want {
			t.Fatalf("pending shell writes %d want %d: %v", actual, want, err)
		}
	}
	assertPending(1)
	if _, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventNativeInputReceipt, CommandID: "another-command", RequestID: "shell-rpc", Status: "accepted", Execution: command.Execution}}); err != nil {
		t.Fatal(err)
	}
	assertPending(1)
	if _, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventNativeInputReceipt, CommandID: command.CommandID, RequestID: "shell-rpc", Status: "accepted", Execution: command.Execution}}); err != nil {
		t.Fatal(err)
	}
	assertPending(1)
	if _, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventNativeShellRPCResult, CommandID: command.CommandID, RequestID: "shell-rpc", ThreadID: "session", Status: "acknowledged", Execution: command.Execution}}); err != nil {
		t.Fatal(err)
	}
	assertPending(1)
	if _, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventNativeShellItemObserved, Action: "completed", CommandID: "another-command", ThreadID: "session", TurnID: "shell-turn", ItemID: "item", Status: "completed", Execution: command.Execution}}); err != nil {
		t.Fatal(err)
	}
	assertPending(1)
	if _, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventNativeShellItemObserved, Action: "completed", CommandID: command.CommandID, ThreadID: "session", TurnID: "shell-turn", ItemID: "item", Status: "failed", Execution: command.Execution}}); err != nil {
		t.Fatal(err)
	}
	assertPending(0)
}
