package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeInternalHelperStreamingTextAndConfigEventsAreAnnotated(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeInternalHelperTurn(t, tr, "", "cmd-helper-config-1")

	initResult := observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "thread-helper-1",
		"model":          "mimo-v2.5-pro",
		"cwd":            "/tmp/project",
		"permissionMode": "default",
	})
	if len(initResult.Events) != 1 || initResult.Events[0].Kind != agentproto.EventConfigObserved {
		t.Fatalf("expected one helper config.observed event, got %#v", initResult.Events)
	}
	assertClaudeInternalHelperEvent(t, initResult.Events[0])
	if initResult.Events[0].ThreadID != "thread-helper-1" {
		t.Fatalf("expected helper config thread id propagated, got %#v", initResult.Events[0])
	}

	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-helper-start-1",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected helper turn.started event, got %#v", started.Events)
	}
	assertClaudeInternalHelperEvent(t, started.Events[0])

	streamStart := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		},
	})
	if len(streamStart.Events) != 1 || streamStart.Events[0].Kind != agentproto.EventItemStarted {
		t.Fatalf("expected helper text item.started event, got %#v", streamStart.Events)
	}
	assertClaudeInternalHelperEvent(t, streamStart.Events[0])
	itemID := streamStart.Events[0].ItemID

	streamDelta := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "helper delta",
			},
		},
	})
	if len(streamDelta.Events) != 1 || streamDelta.Events[0].Kind != agentproto.EventItemDelta {
		t.Fatalf("expected helper text item.delta event, got %#v", streamDelta.Events)
	}
	assertClaudeInternalHelperEvent(t, streamDelta.Events[0])
	if streamDelta.Events[0].ItemID != itemID {
		t.Fatalf("expected helper text delta to reuse item %q, got %#v", itemID, streamDelta.Events[0])
	}

	streamStop := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		},
	})
	if len(streamStop.Events) != 1 || streamStop.Events[0].Kind != agentproto.EventItemCompleted {
		t.Fatalf("expected helper text item.completed event, got %#v", streamStop.Events)
	}
	assertClaudeInternalHelperEvent(t, streamStop.Events[0])
	if streamStop.Events[0].ItemID != itemID {
		t.Fatalf("expected helper text completion to reuse item %q, got %#v", itemID, streamStop.Events[0])
	}

	completed := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "success",
		"result":     "done",
		"session_id": "thread-helper-1",
	})
	if len(completed.Events) == 0 {
		t.Fatalf("expected helper result events, got %#v", completed.Events)
	}
	last := completed.Events[len(completed.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted {
		t.Fatalf("expected helper turn.completed as last event, got %#v", completed.Events)
	}
	assertClaudeInternalHelperEvent(t, last)
}

func TestClaudeInternalHelperRequestEventsAreAnnotated(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "thread-helper-2",
		"model":          "mimo-v2.5-pro",
		"cwd":            "/tmp/project",
		"permissionMode": "default",
	})
	startClaudeInternalHelperTurn(t, tr, "thread-helper-2", "cmd-helper-request-1")

	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-helper-request-1",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected helper turn.started event, got %#v", started.Events)
	}
	assertClaudeInternalHelperEvent(t, started.Events[0])

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-helper-tool-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-helper-bash-1",
					"name":  "Bash",
					"input": map[string]any{"command": "printf helper", "description": "Print helper"},
				},
			},
		},
	})
	if len(assistant.Events) != 1 || assistant.Events[0].Kind != agentproto.EventItemStarted {
		t.Fatalf("expected helper tool item.started event, got %#v", assistant.Events)
	}
	assertClaudeInternalHelperEvent(t, assistant.Events[0])

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-helper-tool-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"tool_use_id": "call-helper-bash-1",
			"input": map[string]any{
				"command":     "printf helper",
				"description": "Print helper",
			},
		},
	})
	if len(requestStarted.Events) != 1 || requestStarted.Events[0].Kind != agentproto.EventRequestStarted {
		t.Fatalf("expected helper request.started event, got %#v", requestStarted.Events)
	}
	assertClaudeInternalHelperEvent(t, requestStarted.Events[0])

	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-helper-tool-1",
			Response: map[string]any{
				"type":     "approval",
				"decision": "accept",
			},
		},
	}); err != nil {
		t.Fatalf("translate helper request respond: %v", err)
	}

	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-helper-bash-1",
					"content":     "helper",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{
			"stdout":      "helper",
			"stderr":      "",
			"interrupted": false,
			"isImage":     false,
		},
	})
	if len(resolved.Events) != 2 {
		t.Fatalf("expected helper tool completion + request.resolved, got %#v", resolved.Events)
	}
	assertClaudeInternalHelperEvent(t, resolved.Events[0])
	assertClaudeInternalHelperEvent(t, resolved.Events[1])
	if resolved.Events[1].Kind != agentproto.EventRequestResolved {
		t.Fatalf("expected helper request.resolved event, got %#v", resolved.Events[1])
	}
}

func TestClaudeInternalHelperTurnDoesNotPoisonLaterRemoteTurnOnSameThread(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "thread-shared-1",
		"model":          "mimo-v2.5-pro",
		"cwd":            "/tmp/project",
		"permissionMode": "default",
	})
	startClaudeInternalHelperTurn(t, tr, "thread-shared-1", "cmd-helper-shared-1")

	helperStarted := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-helper-shared-1",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(helperStarted.Events) != 1 || helperStarted.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected helper turn.started event, got %#v", helperStarted.Events)
	}
	assertClaudeInternalHelperEvent(t, helperStarted.Events[0])

	observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "success",
		"result":     "helper complete",
		"session_id": "thread-shared-1",
	})

	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-remote-shared-1",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-shared-1", CWD: "/tmp/project"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate remote prompt after helper: %v", err)
	}

	remoteStarted := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-remote-shared-1",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(remoteStarted.Events) != 1 || remoteStarted.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected remote turn.started event, got %#v", remoteStarted.Events)
	}
	if remoteStarted.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface || remoteStarted.Events[0].TrafficClass == agentproto.TrafficClassInternalHelper {
		t.Fatalf("expected later remote turn to stay non-helper, got %#v", remoteStarted.Events[0])
	}

	remoteCompleted := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "success",
		"result":     "remote complete",
		"session_id": "thread-shared-1",
	})
	if len(remoteCompleted.Events) == 0 {
		t.Fatalf("expected remote completion events, got %#v", remoteCompleted.Events)
	}
	last := remoteCompleted.Events[len(remoteCompleted.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted {
		t.Fatalf("expected remote turn.completed event, got %#v", remoteCompleted.Events)
	}
	if last.Initiator.Kind != agentproto.InitiatorRemoteSurface || last.TrafficClass == agentproto.TrafficClassInternalHelper {
		t.Fatalf("expected remote completion to stay non-helper, got %#v", last)
	}
}

func startClaudeInternalHelperTurn(t *testing.T, tr *Translator, threadID, commandID string) {
	t.Helper()
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: commandID,
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ThreadID:       threadID,
			InternalHelper: true,
			CWD:            "/tmp/project",
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hidden helper prompt"}},
		},
	}); err != nil {
		t.Fatalf("translate helper prompt send: %v", err)
	}
}

func assertClaudeInternalHelperEvent(t *testing.T, event agentproto.Event) {
	t.Helper()
	if event.TrafficClass != agentproto.TrafficClassInternalHelper || event.Initiator.Kind != agentproto.InitiatorInternalHelper {
		t.Fatalf("expected internal helper event annotation, got %#v", event)
	}
}
