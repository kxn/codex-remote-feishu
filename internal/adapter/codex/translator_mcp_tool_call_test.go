package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerMCPToolCallStartedExtractsMetadata(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"mcp-1","type":"mcpToolCall","status":"inProgress","server":"docs","tool":"lookup"}}}`))
	if err != nil {
		t.Fatalf("observe mcp tool call started: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventItemStarted || event.ItemKind != "mcp_tool_call" || event.Status != "inProgress" {
		t.Fatalf("unexpected mcp tool call started event: %#v", event)
	}
	if event.Metadata["server"] != "docs" || event.Metadata["tool"] != "lookup" {
		t.Fatalf("unexpected mcp tool call start metadata: %#v", event.Metadata)
	}
}

func TestObserveServerMCPToolCallCompletedExtractsFailureMetadata(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"mcp-1","type":"mcpToolCall","status":"failed","server":"docs","tool":"lookup","error":{"message":"connector timeout"},"duration_ms":12}}}`))
	if err != nil {
		t.Fatalf("observe mcp tool call completed: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "mcp_tool_call" || event.Status != "failed" {
		t.Fatalf("unexpected mcp tool call completed event: %#v", event)
	}
	if event.Metadata["server"] != "docs" || event.Metadata["tool"] != "lookup" {
		t.Fatalf("unexpected mcp tool call metadata: %#v", event.Metadata)
	}
	if event.Metadata["errorMessage"] != "connector timeout" {
		t.Fatalf("expected errorMessage metadata, got %#v", event.Metadata)
	}
	if durationMs, ok := event.Metadata["durationMs"].(int); !ok || durationMs != 12 {
		t.Fatalf("expected durationMs metadata, got %#v", event.Metadata)
	}
}
