package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslatePromptSendStartEphemeralUsesThreadStart(t *testing.T) {
	tr := NewTranslator("inst-1")
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartEphemeral,
			CWD:           "/tmp/project",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one command, got %d", len(commands))
	}
	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal thread/start: %v", err)
	}
	if payload["method"] != "thread/start" {
		t.Fatalf("expected thread/start payload, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params["ephemeral"] != true {
		t.Fatalf("expected ephemeral=true, got %#v", params)
	}
	if params["persistExtendedHistory"] != false {
		t.Fatalf("expected persistExtendedHistory=false, got %#v", params)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread-ephemeral"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if !result.Suppress || len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected followup turn/start, got %#v", result)
	}
}

func TestTranslatePromptSendForkEphemeralUsesThreadFork(t *testing.T) {
	tr := NewTranslator("inst-1")
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode:  agentproto.PromptExecutionModeForkEphemeral,
			SourceThreadID: "thread-main",
			CWD:            "/tmp/project",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one command, got %d", len(commands))
	}
	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal thread/fork: %v", err)
	}
	if payload["method"] != "thread/fork" {
		t.Fatalf("expected thread/fork payload, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params["threadId"] != "thread-main" || params["ephemeral"] != true {
		t.Fatalf("unexpected thread/fork params: %#v", params)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-fork-0","result":{"thread":{"id":"thread-forked"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if !result.Suppress || len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected followup turn/start, got %#v", result)
	}
	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToCodex[0], &turnStart); err != nil {
		t.Fatalf("unmarshal turn/start: %v", err)
	}
	paramsTurn, _ := turnStart["params"].(map[string]any)
	if paramsTurn["threadId"] != "thread-forked" {
		t.Fatalf("expected turn/start on forked thread, got %#v", paramsTurn)
	}
}

func TestTranslatePromptSendForkEphemeralRequiresSourceThread(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeForkEphemeral,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err == nil {
		t.Fatal("expected missing source thread to fail")
	}
}
