package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorPrepareForChildLaunchFreshStartDefersThreadBindingUntilInit(t *testing.T) {
	tr := NewTranslator("inst-1")
	tr.sessionID = "resume-session-1"
	tr.permissionMode = "plan"
	tr.PrepareForChildLaunch("")

	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-prompt-start-new",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode:         agentproto.PromptExecutionModeStartNew,
			CreateThreadIfMissing: true,
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "start a fresh session"}},
		},
	})
	if err != nil {
		t.Fatalf("translate prompt send: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("expected permission frame + prompt payload, got %#v", payloads)
	}
	if len(tr.pendingTurns) != 1 {
		t.Fatalf("expected one pending turn, got %#v", tr.pendingTurns)
	}
	if tr.pendingTurns[0].ThreadID != "" {
		t.Fatalf("pending turn thread = %q, want empty until init arrives", tr.pendingTurns[0].ThreadID)
	}

	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "fresh-session-1",
		"cwd":            "/tmp/workspace",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})
	if tr.pendingTurns[0].ThreadID != "fresh-session-1" {
		t.Fatalf("pending turn thread after init = %q, want fresh-session-1", tr.pendingTurns[0].ThreadID)
	}

	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":   "msg-start-new-1",
				"type": "message",
				"role": "assistant",
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected turn.started from message_start, got %#v", started.Events)
	}
	if started.Events[0].ThreadID != "fresh-session-1" {
		t.Fatalf("turn started thread = %q, want fresh-session-1", started.Events[0].ThreadID)
	}
}
