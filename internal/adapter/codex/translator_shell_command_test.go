package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslateThreadShellCommandUsesNativeParamsAndActiveTurn(t *testing.T) {
	translator := NewTranslator("inst-1")
	if _, err := translator.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`)); err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	frames, err := translator.TranslateCommand(agentproto.Command{
		CommandID: "cmd-shell",
		Kind:      agentproto.CommandThreadShellCommand,
		Target:    agentproto.Target{ThreadID: "thread-1", TurnID: "turn-1"},
		ShellCommand: agentproto.ShellCommand{
			Command: `cat '/tmp/payload-1'`,
		},
	})
	if err != nil {
		t.Fatalf("translate shell command: %v", err)
	}
	var message map[string]any
	if err := json.Unmarshal(frames[0], &message); err != nil {
		t.Fatalf("decode native request: %v", err)
	}
	if message["method"] != "thread/shellCommand" {
		t.Fatalf("method = %v", message["method"])
	}
	params := message["params"].(map[string]any)
	if params["threadId"] != "thread-1" || params["command"] != `cat '/tmp/payload-1'` {
		t.Fatalf("unexpected params: %#v", params)
	}
	if _, ok := params["turnId"]; ok {
		t.Fatal("thread/shellCommand must not invent a turnId parameter")
	}
}

func TestTranslateThreadShellCommandRejectsStaleTurn(t *testing.T) {
	translator := NewTranslator("inst-1")
	_, err := translator.TranslateCommand(agentproto.Command{
		Kind:         agentproto.CommandThreadShellCommand,
		Target:       agentproto.Target{ThreadID: "thread-1", TurnID: "turn-old"},
		ShellCommand: agentproto.ShellCommand{Command: "cat /tmp/payload"},
	})
	if err == nil {
		t.Fatal("expected shell command without an active turn to fail closed")
	}
}

func TestObserveUserShellItemCarriesSourceMetadata(t *testing.T) {
	translator := NewTranslator("inst-1")
	result, err := translator.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"commandExecution","source":"userShell","status":"completed"}}}`))
	if err != nil {
		t.Fatalf("observe user shell item: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	if got := result.Events[0].Metadata["source"]; got != "userShell" {
		t.Fatalf("source metadata = %#v", got)
	}
}
