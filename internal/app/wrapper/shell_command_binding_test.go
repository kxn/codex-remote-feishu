package wrapper

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestManagedShellItemsBindByExactPayloadCommandAndItemID(t *testing.T) {
	runtime := newShellCommandRuntime(t.TempDir(), "typed-shell")
	command, cleanup, err := runtime.prepare(agentproto.Command{CommandID: "typed-native-shell", Kind: agentproto.CommandThreadShellCommand, Target: agentproto.Target{ThreadID: "thread", TurnID: "turn"}, ShellCommand: agentproto.ShellCommand{Payload: `{"schema":"queued_input_bundle.v1","text":"actual user input"}`}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(false)
	unrelated := []agentproto.Event{{Kind: agentproto.EventItemStarted, ItemKind: "command_execution", ItemID: "unrelated", ThreadID: "thread", TurnID: "turn", Metadata: map[string]any{"source": "userShell", "command": "another native command"}}}
	runtime.observeEvents(unrelated)
	if unrelated[0].CommandID != "" {
		t.Fatal("unrelated user shell claimed typed action")
	}
	started := []agentproto.Event{{Kind: agentproto.EventItemStarted, ItemKind: "command_execution", ItemID: "exact-shell-item", ThreadID: "thread", TurnID: "turn", Metadata: map[string]any{"source": "userShell", "command": command.ShellCommand.Command}}}
	runtime.observeEvents(started)
	if started[0].CommandID != command.CommandID {
		t.Fatal("exact fixed command did not bind its source")
	}
	completed := []agentproto.Event{{Kind: agentproto.EventItemCompleted, ItemKind: "command_execution", ItemID: "exact-shell-item", ThreadID: "thread", TurnID: "turn", Metadata: map[string]any{"source": "userShell"}}}
	runtime.observeEvents(completed)
	if completed[0].CommandID != command.CommandID {
		t.Fatal("exact started item lost source on completion")
	}
}

func TestManagedShellRejectsForeignSessionAndCannotRebindAnItem(t *testing.T) {
	runtime := newShellCommandRuntime(t.TempDir(), "strict-shell")
	command, cleanup, err := runtime.prepare(agentproto.Command{CommandID: "typed-shell", Kind: agentproto.CommandThreadShellCommand, Target: agentproto.Target{ThreadID: "session", TurnID: "expected"}, ShellCommand: agentproto.ShellCommand{OperationID: "operation", Payload: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(false)
	item := func(thread, turn, id string, kind agentproto.EventKind) []agentproto.Event {
		return []agentproto.Event{{Kind: kind, ItemKind: "command_execution", ItemID: id, ThreadID: thread, TurnID: turn, Metadata: map[string]any{"source": "userShell", "command": "/bin/bash -lc " + fmt.Sprintf("%q", command.ShellCommand.Command)}}}
	}
	foreign := runtime.observeEvents(item("foreign-session", "turn", "wrong", agentproto.EventItemCompleted))
	if foreign[0].CommandID != "" {
		t.Fatal("foreign session claimed private payload")
	}
	first := runtime.observeEvents(item("session", "actual-shell-turn", "real-item", agentproto.EventItemStarted))
	if first[0].CommandID != command.CommandID {
		t.Fatal("exact native shell argv did not bind source")
	}
	other := runtime.observeEvents(item("session", "another-turn", "different-item", agentproto.EventItemCompleted))
	if other[0].CommandID != "" {
		t.Fatal("a bound item was replaced by a different native item")
	}
	completed := runtime.observeEvents(item("session", "actual-shell-turn", "real-item", agentproto.EventItemCompleted))
	if completed[0].Kind != agentproto.EventNativeShellItemObserved || completed[0].CommandID != command.CommandID {
		t.Fatal("foreign item removed real payload mapping")
	}
}

func TestPrivateShellDisplayArgvMatchesOnlyExactFixedReader(t *testing.T) {
	expected := "cat '/private/unique-payload.txt'"
	for _, test := range []struct {
		display string
		want    bool
	}{{expected, true}, {"/bin/bash -lc " + fmt.Sprintf("%q", expected), true}, {"/bin/bash -lc " + fmt.Sprintf("%q", expected+"; echo extra"), false}, {"/bin/bash -lc " + fmt.Sprintf("%q", "echo "+expected), false}, {"/bin/bash -lc " + fmt.Sprintf("%q", expected) + " extra", false}, {"/bin/bash -lc \"$(echo private-payload)\"", false}} {
		if actual := matchesPrivateShellCommand(test.display, expected); actual != test.want {
			t.Fatalf("display %q matched=%v", test.display, actual)
		}
	}
}

func TestPrivateShellDisplayArgvPreservesQuotedPaths(t *testing.T) {
	for _, expected := range []string{"cat '/private/path with spaces/payload'", "cat '/private/owner'\"'\"'s payload'"} {
		display := fmt.Sprintf("%q -lc %q", "/private/shell with spaces/bash", expected)
		if !matchesPrivateShellCommand(display, expected) {
			t.Fatalf("literal argv lost exact script: %s", display)
		}
		if matchesPrivateShellCommand(display, expected+" extra") {
			t.Fatal("different script matched")
		}
	}
}
