package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestBuildInitializeFrameMatchesOpenCodeACPHandshake(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	frame, err := tr.BuildInitializeFrame()
	if err != nil {
		t.Fatalf("BuildInitializeFrame: %v", err)
	}
	payload := decodeFrame(t, frame)
	if payload["jsonrpc"] != "2.0" || payload["method"] != "initialize" {
		t.Fatalf("unexpected initialize frame: %#v", payload)
	}
	params := asMap(t, payload["params"])
	if params["protocolVersion"] != float64(1) {
		t.Fatalf("protocolVersion = %#v, want 1", params["protocolVersion"])
	}
	caps := asMap(t, params["clientCapabilities"])
	meta := asMap(t, caps["_meta"])
	if meta["terminal-auth"] != true {
		t.Fatalf("terminal-auth metadata = %#v, want true", meta["terminal-auth"])
	}
	info := asMap(t, params["clientInfo"])
	if info["name"] == "" || info["version"] == "" {
		t.Fatalf("clientInfo missing name/version: %#v", info)
	}
}

func TestPromptSendStartNewCreatesSessionThenPromptsAfterResponse(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           "/tmp/work",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	if len(result.OutboundToChild) != 1 {
		t.Fatalf("outbound = %d frames, want 1", len(result.OutboundToChild))
	}
	newFrame := decodeFrame(t, result.OutboundToChild[0])
	if newFrame["method"] != "session/new" {
		t.Fatalf("method = %q, want session/new", newFrame["method"])
	}
	newParams := asMap(t, newFrame["params"])
	if newParams["cwd"] != "/tmp/work" {
		t.Fatalf("cwd = %#v", newParams["cwd"])
	}
	if _, ok := newParams["mcpServers"].([]any); !ok {
		t.Fatalf("mcpServers missing from session/new params: %#v", newParams)
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      newFrame["id"],
		"result": map[string]any{
			"sessionId": "ses_1",
			"configOptions": []any{
				map[string]any{
					"id":           "model",
					"type":         "select",
					"category":     "model",
					"currentValue": "test/test-model",
					"options": []any{
						map[string]any{"value": "test/test-model", "name": "Test Model"},
					},
				},
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(new response): %v", err)
	}
	assertEventKinds(t, observed.Events,
		agentproto.EventThreadDiscovered,
		agentproto.EventThreadFocused,
		agentproto.EventTurnStarted,
	)
	if observed.Events[2].CommandID != "cmd-1" || observed.Events[2].ThreadID != "ses_1" {
		t.Fatalf("turn started event lost command/thread context: %#v", observed.Events[2])
	}
	if observed.Events[2].Initiator.Kind != agentproto.InitiatorRemoteSurface || observed.Events[2].Initiator.SurfaceSessionID != "surface-1" {
		t.Fatalf("turn initiator = %#v", observed.Events[2].Initiator)
	}
	if len(observed.OutboundToChild) != 1 {
		t.Fatalf("followup outbound = %d frames, want session/prompt", len(observed.OutboundToChild))
	}
	promptFrame := decodeFrame(t, observed.OutboundToChild[0])
	if promptFrame["method"] != "session/prompt" {
		t.Fatalf("followup method = %q, want session/prompt", promptFrame["method"])
	}
	promptParams := asMap(t, promptFrame["params"])
	if promptParams["sessionId"] != "ses_1" {
		t.Fatalf("prompt sessionId = %#v", promptParams["sessionId"])
	}
	prompt := promptParams["prompt"].([]any)
	first := asMap(t, prompt[0])
	if first["type"] != "text" || first["text"] != "hello" {
		t.Fatalf("prompt content = %#v", prompt)
	}
}

func TestPromptSendStartNewIncludesConfiguredRemoteMCPServers(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	tr.SetMCPServers([]MCPServer{
		{
			Name: "codex_remote_feishu",
			Type: "http",
			URL:  "http://127.0.0.1:9702/mcp?codex_remote_instance_id=inst-1",
			Headers: []MCPNameValue{
				{Name: "Authorization", Value: "Bearer secret-token"},
			},
		},
	})
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           "/tmp/work",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	params := asMap(t, frame["params"])
	servers := asSlice(t, params["mcpServers"])
	if len(servers) != 1 {
		t.Fatalf("mcpServers = %#v, want one server", servers)
	}
	server := asMap(t, servers[0])
	if server["type"] != "http" || server["name"] != "codex_remote_feishu" || server["url"] != "http://127.0.0.1:9702/mcp?codex_remote_instance_id=inst-1" {
		t.Fatalf("unexpected remote MCP server: %#v", server)
	}
	headers := asSlice(t, server["headers"])
	if len(headers) != 1 {
		t.Fatalf("headers = %#v, want one Authorization header", headers)
	}
	header := asMap(t, headers[0])
	if header["name"] != "Authorization" || header["value"] != "Bearer secret-token" {
		t.Fatalf("unexpected Authorization header: %#v", header)
	}
}

func TestPromptSendExistingSessionResumesThenPromptsAfterResponse(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-resume",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ThreadID: "ses_existing",
			CWD:      "/tmp/work",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "continue"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(resume prompt): %v", err)
	}
	resumeFrame := decodeFrame(t, result.OutboundToChild[0])
	if resumeFrame["method"] != "session/resume" {
		t.Fatalf("method = %#v, want session/resume", resumeFrame["method"])
	}
	resumeParams := asMap(t, resumeFrame["params"])
	if resumeParams["sessionId"] != "ses_existing" {
		t.Fatalf("resume params = %#v", resumeParams)
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      resumeFrame["id"],
		"result":  map[string]any{"sessionId": "ses_existing", "title": "Existing"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(resume response): %v", err)
	}
	assertEventKinds(t, observed.Events,
		agentproto.EventThreadDiscovered,
		agentproto.EventThreadFocused,
		agentproto.EventTurnStarted,
	)
	promptFrame := decodeFrame(t, observed.OutboundToChild[0])
	if promptFrame["method"] != "session/prompt" || asMap(t, promptFrame["params"])["sessionId"] != "ses_existing" {
		t.Fatalf("resume followup frame = %#v", promptFrame)
	}
}

func TestPromptSendForkEphemeralForksThenPromptsNewSession(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-fork",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ExecutionMode:  agentproto.PromptExecutionModeForkEphemeral,
			SourceThreadID: "ses_source",
			CWD:            "/tmp/work",
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "try this"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(fork prompt): %v", err)
	}
	forkFrame := decodeFrame(t, result.OutboundToChild[0])
	if forkFrame["method"] != "session/fork" {
		t.Fatalf("method = %#v, want session/fork", forkFrame["method"])
	}
	forkParams := asMap(t, forkFrame["params"])
	if forkParams["sessionId"] != "ses_source" {
		t.Fatalf("fork params = %#v", forkParams)
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      forkFrame["id"],
		"result":  map[string]any{"sessionId": "ses_forked", "title": "Forked"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(fork response): %v", err)
	}
	assertEventKinds(t, observed.Events,
		agentproto.EventThreadDiscovered,
		agentproto.EventThreadFocused,
		agentproto.EventTurnStarted,
	)
	promptFrame := decodeFrame(t, observed.OutboundToChild[0])
	if promptFrame["method"] != "session/prompt" || asMap(t, promptFrame["params"])["sessionId"] != "ses_forked" {
		t.Fatalf("fork followup frame = %#v", promptFrame)
	}
}

func TestResponsesCorrelateByJSONRPCIDOutOfOrder(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	listResult, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-list",
		Kind:      agentproto.CommandThreadsRefresh,
		Target:    agentproto.Target{CWD: "/tmp/work"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(list): %v", err)
	}
	historyResult, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-history",
		Kind:      agentproto.CommandThreadHistoryRead,
		Target:    agentproto.Target{ThreadID: "ses_1", CWD: "/tmp/work"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(history): %v", err)
	}
	listFrame := decodeFrame(t, listResult.OutboundToChild[0])
	historyFrame := decodeFrame(t, historyResult.OutboundToChild[0])

	historyObserved, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      historyFrame["id"],
		"result":  map[string]any{"configOptions": []any{}},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(history response): %v", err)
	}
	assertEventKinds(t, historyObserved.Events, agentproto.EventThreadHistoryRead)
	if historyObserved.Events[0].CommandID != "cmd-history" {
		t.Fatalf("history response correlated to wrong command: %#v", historyObserved.Events[0])
	}

	listObserved, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      listFrame["id"],
		"result": map[string]any{
			"sessions": []any{map[string]any{"sessionId": "ses_1", "cwd": "/tmp/work", "title": "One"}},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(list response): %v", err)
	}
	assertEventKinds(t, listObserved.Events, agentproto.EventThreadsSnapshot)
	if listObserved.Events[0].CommandID != "cmd-list" {
		t.Fatalf("list response correlated to wrong command: %#v", listObserved.Events[0])
	}
}

func TestAgentAndThoughtChunksMapToExistingTurn(t *testing.T) {
	tr, _ := startPromptedSession(t)

	thought, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_thought_chunk",
		"messageId":     "msg_1",
		"content":       map[string]any{"type": "text", "text": "think"},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(thought): %v", err)
	}
	assertEventKinds(t, thought.Events, agentproto.EventItemStarted, agentproto.EventItemReasoningSummaryPartAdded)
	if thought.Events[0].ItemKind != "reasoning_summary" || thought.Events[1].Delta != "think" {
		t.Fatalf("unexpected thought events: %#v", thought.Events)
	}

	message, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "msg_1",
		"content":       map[string]any{"type": "text", "text": "hello"},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(message): %v", err)
	}
	assertEventKinds(t, message.Events, agentproto.EventItemStarted, agentproto.EventItemDelta)
	if message.Events[0].ItemKind != "agent_message" || message.Events[1].Delta != "hello" {
		t.Fatalf("unexpected message events: %#v", message.Events)
	}

	next, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "msg_1",
		"content":       map[string]any{"type": "text", "text": " world"},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(second message): %v", err)
	}
	assertEventKinds(t, next.Events, agentproto.EventItemDelta)
	if next.Events[0].ItemID != message.Events[0].ItemID {
		t.Fatalf("second chunk item id = %q, want %q", next.Events[0].ItemID, message.Events[0].ItemID)
	}
}

func TestPromptResponseCompletesTurnWithoutAssistantText(t *testing.T) {
	tr, promptID := startPromptedSession(t)
	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      promptID,
		"result": map[string]any{
			"stopReason": "end_turn",
			"usage": map[string]any{
				"inputTokens":  11,
				"outputTokens": 7,
				"totalTokens":  18,
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(prompt response): %v", err)
	}
	assertEventKinds(t, result.Events,
		agentproto.EventThreadTokenUsageUpdated,
		agentproto.EventTurnCompleted,
	)
	for _, event := range result.Events {
		if event.Kind == agentproto.EventItemDelta || event.Kind == agentproto.EventItemStarted {
			t.Fatalf("prompt response must not be treated as assistant text: %#v", result.Events)
		}
	}
	completed := result.Events[len(result.Events)-1]
	if completed.Status != "completed" || completed.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime {
		t.Fatalf("unexpected completion event: %#v", completed)
	}
	if result.Events[0].TokenUsage == nil || result.Events[0].TokenUsage.Last.TotalTokens != 18 {
		t.Fatalf("usage not projected from prompt response: %#v", result.Events[0])
	}
}

func TestPermissionRequestAndResponseRoundTripACPOptionID(t *testing.T) {
	tr, _ := startPromptedSession(t)
	requested, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-1",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall": map[string]any{
				"toolCallId": "tool_1",
				"title":      "Edit file",
				"kind":       "edit",
				"status":     "pending",
				"rawInput":   map[string]any{"path": "main.go"},
			},
			"options": []any{
				map[string]any{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
				map[string]any{"optionId": "reject", "kind": "reject_once", "name": "Reject"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(permission request): %v", err)
	}
	assertEventKinds(t, requested.Events, agentproto.EventRequestStarted)
	event := requested.Events[0]
	if event.RequestID != "perm-1" || event.RequestPrompt == nil {
		t.Fatalf("unexpected permission event: %#v", event)
	}
	if event.RequestPrompt.Type != agentproto.RequestTypeApproval || event.RequestPrompt.ItemID != "tool_1" {
		t.Fatalf("unexpected request prompt: %#v", event.RequestPrompt)
	}
	if len(event.RequestPrompt.Options) != 2 || event.RequestPrompt.Options[0].OptionID != "once" {
		t.Fatalf("permission options not preserved: %#v", event.RequestPrompt.Options)
	}

	responded, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-approve",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "perm-1",
			Response:  map[string]any{"optionId": "once"},
		},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(request.respond): %v", err)
	}
	assertEventKinds(t, responded.Events, agentproto.EventRequestResolved)
	if len(responded.OutboundToChild) != 1 {
		t.Fatalf("permission response outbound = %d frames, want 1", len(responded.OutboundToChild))
	}
	frame := decodeFrame(t, responded.OutboundToChild[0])
	if frame["id"] != "perm-1" {
		t.Fatalf("permission response id = %#v", frame["id"])
	}
	outcome := asMap(t, asMap(t, frame["result"])["outcome"])
	if outcome["outcome"] != "selected" || outcome["optionId"] != "once" {
		t.Fatalf("unexpected permission response outcome: %#v", outcome)
	}
}

func TestThreadsRefreshMapsSessionListToSnapshot(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-list",
		Kind:      agentproto.CommandThreadsRefresh,
		Target:    agentproto.Target{CWD: "/tmp/work"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(threads.refresh): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["method"] != "session/list" {
		t.Fatalf("method = %#v, want session/list", frame["method"])
	}
	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"result": map[string]any{
			"sessions": []any{
				map[string]any{"sessionId": "ses_1", "cwd": "/tmp/work", "title": "One"},
				map[string]any{"sessionId": "ses_2", "cwd": "/tmp/other", "title": "Two"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(list response): %v", err)
	}
	assertEventKinds(t, observed.Events, agentproto.EventThreadsSnapshot)
	if len(observed.Events[0].Threads) != 2 || observed.Events[0].Threads[0].ThreadID != "ses_1" {
		t.Fatalf("threads snapshot = %#v", observed.Events[0].Threads)
	}
}

func TestThreadHistoryReadUsesSessionLoadAndReturnsHistoryEnvelope(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-history",
		Kind:      agentproto.CommandThreadHistoryRead,
		Target:    agentproto.Target{ThreadID: "ses_1", CWD: "/tmp/work"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(history): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["method"] != "session/load" {
		t.Fatalf("method = %#v, want session/load", frame["method"])
	}
	params := asMap(t, frame["params"])
	if params["sessionId"] != "ses_1" {
		t.Fatalf("session/load params = %#v", params)
	}

	for _, replay := range []map[string]any{
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "user_message_chunk",
			"messageId":     "msg_user",
			"content":       map[string]any{"type": "text", "text": "hello"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "agent_thought_chunk",
			"messageId":     "msg_assistant",
			"content":       map[string]any{"type": "text", "text": "thinking"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "call_shell",
			"title":         "printf hello",
			"kind":          "execute",
			"status":        "pending",
			"rawInput":      map[string]any{"cmd": "printf hello", "cwd": "/tmp/work"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "call_shell",
			"status":        "completed",
			"content": []any{
				map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "hello\n"}},
			},
			"rawOutput": map[string]any{"output": "hello\n"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"messageId":     "msg_assistant",
			"content":       map[string]any{"type": "text", "text": "hi there"},
		}),
	} {
		replayed, err := tr.ObserveServer(mustLine(t, replay))
		if err != nil {
			t.Fatalf("ObserveServer(replay): %v", err)
		}
		if len(replayed.Events) != 0 {
			t.Fatalf("load replay update leaked live events: %#v", replayed.Events)
		}
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"result":  map[string]any{"configOptions": []any{}},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(load response): %v", err)
	}
	assertEventKinds(t, observed.Events, agentproto.EventThreadHistoryRead)
	if observed.Events[0].ThreadHistory == nil || observed.Events[0].ThreadHistory.Thread.ThreadID != "ses_1" {
		t.Fatalf("history event = %#v", observed.Events[0])
	}
	turns := observed.Events[0].ThreadHistory.Turns
	if len(turns) != 1 {
		t.Fatalf("history turns = %#v, want one turn", turns)
	}
	items := turns[0].Items
	if len(items) != 4 {
		t.Fatalf("history items = %#v, want user/thought/tool/assistant", items)
	}
	if items[0].Kind != "user_message" || items[0].Text != "hello" {
		t.Fatalf("user history item = %#v", items[0])
	}
	if items[1].Kind != "reasoning_summary" || items[1].Text != "thinking" {
		t.Fatalf("thought history item = %#v", items[1])
	}
	if items[2].Kind != "command_execution" || items[2].Command != "printf hello" || items[2].Text != "hello\n" || items[2].Status != "completed" {
		t.Fatalf("tool history item = %#v", items[2])
	}
	if items[3].Kind != "agent_message" || items[3].Text != "hi there" {
		t.Fatalf("assistant history item = %#v", items[3])
	}
}

func TestThreadHistoryReadSuppressesDynamicToolResultText(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-history",
		Kind:      agentproto.CommandThreadHistoryRead,
		Target:    agentproto.Target{ThreadID: "ses_1", CWD: "/tmp/work"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(history): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])

	for _, replay := range []map[string]any{
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "user_message_chunk",
			"messageId":     "msg_user",
			"content":       map[string]any{"type": "text", "text": "inspect repo"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "read_1",
			"title":         "read",
			"kind":          "read",
			"status":        "pending",
			"rawInput":      map[string]any{"path": "README.md"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "read_1",
			"status":        "completed",
			"content": []any{
				map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "# README\n\nfull file contents"}},
			},
			"rawOutput": map[string]any{"output": "# README\n\nfull file contents"},
		}),
	} {
		replayed, err := tr.ObserveServer(mustLine(t, replay))
		if err != nil {
			t.Fatalf("ObserveServer(replay): %v", err)
		}
		if len(replayed.Events) != 0 {
			t.Fatalf("load replay update leaked live events: %#v", replayed.Events)
		}
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"result":  map[string]any{"configOptions": []any{}},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(load response): %v", err)
	}
	assertEventKinds(t, observed.Events, agentproto.EventThreadHistoryRead)
	items := observed.Events[0].ThreadHistory.Turns[0].Items
	if len(items) != 2 {
		t.Fatalf("history items = %#v, want user/tool", items)
	}
	if items[1].Kind != "dynamic_tool_call" || items[1].Text != "" {
		t.Fatalf("dynamic tool raw result should not become history text: %#v", items[1])
	}
	if raw, ok := items[1].Metadata["rawOutput"].(map[string]any); !ok || raw["output"] != "# README\n\nfull file contents" {
		t.Fatalf("dynamic raw output should stay in metadata: %#v", items[1])
	}
}

func TestTurnInterruptSendsSessionCancelNotification(t *testing.T) {
	tr, _ := startPromptedSession(t)
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-cancel",
		Kind:      agentproto.CommandTurnInterrupt,
		Target:    agentproto.Target{ThreadID: "ses_1"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(cancel): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["method"] != "session/cancel" {
		t.Fatalf("method = %#v, want session/cancel", frame["method"])
	}
	if _, hasID := frame["id"]; hasID {
		t.Fatalf("session/cancel must be a notification, got id in %#v", frame)
	}
}

func TestWriteTextFileClientRequestFailsClosed(t *testing.T) {
	tr, _ := startPromptedSession(t)
	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "write-1",
		"method":  "fs/write_text_file",
		"params": map[string]any{
			"sessionId": "ses_1",
			"path":      "/tmp/work/main.go",
			"content":   "package main\n",
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(write request): %v", err)
	}
	if len(result.OutboundToChild) != 1 {
		t.Fatalf("write response frames = %d, want 1", len(result.OutboundToChild))
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["id"] != "write-1" || frame["error"] == nil {
		t.Fatalf("write request must fail closed with matching id: %#v", frame)
	}
}

func TestWriteTextFileAfterPermissionApprovalWritesWorkspaceFileAndEmitsPatch(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	tr, _ := startPromptedSessionWithWorkspace(t, workspace)
	if _, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-1",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall":  map[string]any{"toolCallId": "tool_1", "title": "Edit file", "kind": "edit", "status": "pending"},
			"options": []any{
				map[string]any{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
				map[string]any{"optionId": "reject", "kind": "reject_once", "name": "Reject"},
			},
		},
	})); err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-approve",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "perm-1",
			Response:  map[string]any{"optionId": "once"},
		},
	}); err != nil {
		t.Fatalf("TranslateCommand(approve): %v", err)
	}

	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "write-1",
		"method":  "fs/write_text_file",
		"params": map[string]any{
			"sessionId": "ses_1",
			"path":      target,
			"content":   "package main\n\nfunc main() {}\n",
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(write request): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["id"] != "write-1" || frame["error"] != nil {
		t.Fatalf("write response = %#v, want success", frame)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("written file = %q, %v", string(got), err)
	}
	assertEventKinds(t, result.Events, agentproto.EventItemFileChangePatchUpdated)
	changeEvent := result.Events[0]
	if changeEvent.ItemKind != "file_change" || len(changeEvent.FileChanges) != 1 {
		t.Fatalf("file change event = %#v", changeEvent)
	}
	if changeEvent.FileChanges[0].Path != "main.go" || changeEvent.FileChanges[0].Kind != agentproto.FileChangeUpdate || changeEvent.FileChanges[0].Diff == "" {
		t.Fatalf("file change record = %#v", changeEvent.FileChanges[0])
	}

	second, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "write-2",
		"method":  "fs/write_text_file",
		"params": map[string]any{
			"sessionId": "ses_1",
			"path":      target,
			"content":   "package main\n\nfunc main() { println(\"again\") }\n",
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(second write request): %v", err)
	}
	secondFrame := decodeFrame(t, second.OutboundToChild[0])
	if secondFrame["id"] != "write-2" || secondFrame["error"] == nil {
		t.Fatalf("second write must fail after once approval is consumed: %#v", secondFrame)
	}
}

func TestWriteTextFileRejectsSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	linkPath := filepath.Join(workspace, "escape.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	tr, _ := startPromptedSessionWithWorkspace(t, workspace)
	if _, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-escape",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall":  map[string]any{"toolCallId": "tool_escape", "title": "Edit escape", "kind": "edit", "status": "pending"},
			"options": []any{
				map[string]any{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
				map[string]any{"optionId": "reject", "kind": "reject_once", "name": "Reject"},
			},
		},
	})); err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-approve-escape",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "perm-escape",
			Response:  map[string]any{"optionId": "once"},
		},
	}); err != nil {
		t.Fatalf("TranslateCommand(approve escape): %v", err)
	}

	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "write-escape",
		"method":  "fs/write_text_file",
		"params": map[string]any{
			"sessionId": "ses_1",
			"path":      linkPath,
			"content":   "escaped\n",
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(write escape): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["id"] != "write-escape" || frame["error"] == nil {
		t.Fatalf("symlink escape write must fail closed: %#v", frame)
	}
	if got, err := os.ReadFile(outsideFile); err != nil || string(got) != "outside\n" {
		t.Fatalf("outside file changed through symlink: %q, %v", string(got), err)
	}
}

func TestWriteTextFileRejectsDanglingSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "created-through-link.txt")
	linkPath := filepath.Join(workspace, "dangling-escape.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not available: %v", err)
	}
	tr, _ := startPromptedSessionWithWorkspace(t, workspace)
	if _, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-dangling",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall":  map[string]any{"toolCallId": "tool_dangling", "title": "Edit dangling", "kind": "edit", "status": "pending"},
			"options": []any{
				map[string]any{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
				map[string]any{"optionId": "reject", "kind": "reject_once", "name": "Reject"},
			},
		},
	})); err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-approve-dangling",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "perm-dangling",
			Response:  map[string]any{"optionId": "once"},
		},
	}); err != nil {
		t.Fatalf("TranslateCommand(approve dangling): %v", err)
	}

	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "write-dangling",
		"method":  "fs/write_text_file",
		"params": map[string]any{
			"sessionId": "ses_1",
			"path":      linkPath,
			"content":   "escaped\n",
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(write dangling): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if frame["id"] != "write-dangling" || frame["error"] == nil {
		t.Fatalf("dangling symlink escape write must fail closed: %#v", frame)
	}
	if _, err := os.Stat(outsideFile); !os.IsNotExist(err) {
		t.Fatalf("outside file was created through dangling symlink: %v", err)
	}
}

func startPromptedSession(t *testing.T) (*Translator, any) {
	t.Helper()
	return startPromptedSessionWithWorkspace(t, "/tmp/work")
}

func startPromptedSessionWithWorkspace(t *testing.T, workspace string) (*Translator, any) {
	t.Helper()
	tr := NewTranslator("inst-1", workspace)
	start, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           workspace,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	newFrame := decodeFrame(t, start.OutboundToChild[0])
	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      newFrame["id"],
		"result":  map[string]any{"sessionId": "ses_1"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(new response): %v", err)
	}
	if len(observed.OutboundToChild) != 1 {
		t.Fatalf("new response followup = %d frames, want prompt", len(observed.OutboundToChild))
	}
	promptFrame := decodeFrame(t, observed.OutboundToChild[0])
	if promptFrame["method"] != "session/prompt" {
		t.Fatalf("followup method = %#v", promptFrame["method"])
	}
	return tr, promptFrame["id"]
}

func sessionUpdate(sessionID string, update map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update":    update,
		},
	}
}

func mustLine(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return append(data, '\n')
}

func decodeFrame(t *testing.T, frame []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatalf("unmarshal frame %q: %v", string(frame), err)
	}
	return payload
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is %T, want map: %#v", value, value)
	}
	return out
}

func asSlice(t *testing.T, value any) []any {
	t.Helper()
	out, ok := value.([]any)
	if !ok {
		t.Fatalf("value is %T, want slice: %#v", value, value)
	}
	return out
}

func assertEventKinds(t *testing.T, events []agentproto.Event, want ...agentproto.EventKind) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %s, want %s; full=%#v", eventKinds(events), want, events)
	}
	for i := range want {
		if events[i].Kind != want[i] {
			t.Fatalf("event[%d] kind = %q, want %q; full=%#v", i, events[i].Kind, want[i], events)
		}
	}
}

func eventKinds(events []agentproto.Event) string {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		parts = append(parts, string(event.Kind))
	}
	return strings.Join(parts, ",")
}
