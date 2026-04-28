package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorToolApprovalMainChain(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-tool-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-bash-1",
					"name":  "Bash",
					"input": map[string]any{"command": "printf BLACKBOX_TOOL_OK", "description": "Print BLACKBOX_TOOL_OK"},
				},
			},
		},
	})
	if len(assistant.Events) != 1 {
		t.Fatalf("expected one item.started event, got %#v", assistant.Events)
	}
	if assistant.Events[0].Kind != agentproto.EventItemStarted || assistant.Events[0].ItemKind != "dynamic_tool_call" {
		t.Fatalf("unexpected assistant tool_use event: %#v", assistant.Events[0])
	}

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-tool-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"tool_use_id": "call-bash-1",
			"input": map[string]any{
				"command":     "printf BLACKBOX_TOOL_OK",
				"description": "Print BLACKBOX_TOOL_OK",
			},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	event := requestStarted.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.ThreadID != threadID || event.TurnID != turnID {
		t.Fatalf("unexpected request.started event: %#v", event)
	}
	if event.RequestPrompt == nil || event.RequestPrompt.Type != agentproto.RequestTypeApproval || event.RequestPrompt.RawType != "can_use_tool" {
		t.Fatalf("unexpected approval prompt: %#v", event.RequestPrompt)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-tool-1",
			Response: map[string]any{
				"type":     "approval",
				"decision": "accept",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate request respond: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one control_response payload, got %d", len(payloads))
	}
	payload := decodeFrame(t, payloads[0])
	if _, ok := payload["request_id"]; ok {
		t.Fatalf("unexpected top-level request_id in control_response: %#v", payload)
	}
	response := testMapValue(payload["response"])
	if lookupStringFromAny(response["request_id"]) != "req-tool-1" {
		t.Fatalf("unexpected response payload: %#v", payload)
	}
	body := testMapValue(response["response"])
	if lookupStringFromAny(body["behavior"]) != "allow" {
		t.Fatalf("unexpected allow body: %#v", body)
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-bash-1",
					"content":     "BLACKBOX_TOOL_OK",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{
			"stdout":      "BLACKBOX_TOOL_OK",
			"stderr":      "",
			"interrupted": false,
			"isImage":     false,
		},
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one tool completed event, got %#v", completed.Events)
	}
	if completed.Events[0].Kind != agentproto.EventItemCompleted || completed.Events[0].ItemKind != "dynamic_tool_call" || completed.Events[0].Status != "completed" {
		t.Fatalf("unexpected tool completion event: %#v", completed.Events[0])
	}
}

func TestClaudeTranslatorAskUserQuestionRoundTrip(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-ask-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-ask-1",
					"name":  "AskUserQuestion",
					"input": map[string]any{"questions": []any{map[string]any{"id": "approach", "header": "Approach", "question": "Which approach should I take?", "options": []any{map[string]any{"label": "Fast"}, map[string]any{"label": "Safe"}}}}},
				},
			},
		},
	})
	if len(assistant.Events) != 0 {
		t.Fatalf("internal AskUserQuestion should not emit dynamic tool noise, got %#v", assistant.Events)
	}

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-ask-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "AskUserQuestion",
			"tool_use_id": "call-ask-1",
			"input": map[string]any{
				"questions": []any{
					map[string]any{
						"id":       "approach",
						"header":   "Approach",
						"question": "Which approach should I take?",
						"options":  []any{map[string]any{"label": "Fast"}, map[string]any{"label": "Safe"}},
					},
				},
			},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	event := requestStarted.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.ThreadID != threadID || event.TurnID != turnID {
		t.Fatalf("unexpected ask request event: %#v", event)
	}
	if event.RequestPrompt == nil || event.RequestPrompt.Type != agentproto.RequestTypeRequestUserInput || len(event.RequestPrompt.Questions) != 1 {
		t.Fatalf("unexpected request_user_input prompt: %#v", event.RequestPrompt)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-ask-1",
			Response: map[string]any{
				"decision": "accept",
				"answers": map[string]any{
					"approach": map[string]any{
						"answers": []any{"Fast"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate ask response: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	updatedInput := testMapValue(body["updatedInput"])
	answers := testMapValue(updatedInput["answers"])
	if lookupStringFromAny(answers["Which approach should I take?"]) != "Fast" {
		t.Fatalf("unexpected AskUserQuestion updated answers: %#v", answers)
	}

	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-ask-1",
					"content":     "User has answered the question.",
				},
			},
		},
		"tool_use_result": map[string]any{
			"questions": []any{map[string]any{"question": "Which approach should I take?", "header": "Approach"}},
			"answers":   map[string]any{"Which approach should I take?": "Fast"},
		},
	})
	if len(resolved.Events) != 1 {
		t.Fatalf("expected one request.resolved event, got %#v", resolved.Events)
	}
	if resolved.Events[0].Kind != agentproto.EventRequestResolved || resolved.Events[0].RequestID != "req-ask-1" {
		t.Fatalf("unexpected request resolved event: %#v", resolved.Events[0])
	}
}

func TestClaudeTranslatorPlanDeclineInterruptsTurn(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "plan")

	planText := "1. Update README.txt line 1.\n2. Save the file."
	planItem := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{"type": "text", "text": planText},
			},
		},
	})
	if len(planItem.Events) != 2 || planItem.Events[1].Kind != agentproto.EventItemCompleted {
		t.Fatalf("expected assistant text item lifecycle, got %#v", planItem.Events)
	}

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	if len(assistant.Events) != 0 {
		t.Fatalf("internal ExitPlanMode should not emit dynamic tool noise, got %#v", assistant.Events)
	}

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-1",
			"input":       map[string]any{},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	event := requestStarted.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.ThreadID != threadID || event.TurnID != turnID {
		t.Fatalf("unexpected plan request event: %#v", event)
	}
	if event.RequestPrompt == nil || event.RequestPrompt.Type != agentproto.RequestTypeApproval || event.RequestPrompt.Body != planText {
		t.Fatalf("unexpected plan confirmation prompt: %#v", event.RequestPrompt)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID:          "req-plan-1",
			InterruptOnDecline: true,
			Response: map[string]any{
				"decision": "decline",
				"message":  "blackbox reject plan",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan decline: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	if lookupStringFromAny(body["behavior"]) != "deny" || body["interrupt"] != true {
		t.Fatalf("unexpected deny body: %#v", body)
	}

	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-plan-1",
					"content":     "blackbox reject plan",
					"is_error":    true,
				},
			},
		},
		"tool_use_result": "Error: blackbox reject plan",
	})
	if len(resolved.Events) != 1 || resolved.Events[0].Kind != agentproto.EventRequestResolved {
		t.Fatalf("expected request.resolved on plan denial, got %#v", resolved.Events)
	}
	if lookupStringFromAny(resolved.Events[0].Metadata["decision"]) != "decline" {
		t.Fatalf("unexpected resolved plan metadata: %#v", resolved.Events[0].Metadata)
	}

	observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []any{map[string]any{"type": "text", "text": "[Request interrupted by user for tool use]"}},
		},
	})

	completed := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "error_during_execution",
		"is_error":   false,
		"session_id": threadID,
		"usage": map[string]any{
			"input_tokens":                1,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
			"output_tokens":               0,
		},
		"modelUsage": map[string]any{
			"mimo-v2.5-pro": map[string]any{"contextWindow": 200000},
		},
	})
	last := completed.Events[len(completed.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted || last.Status != "interrupted" {
		t.Fatalf("expected interrupted turn completion, got %#v", completed.Events)
	}
}

func TestClaudeTranslatorPlanRequestFallsBackToLatestPlanFile(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "plan")

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	planDir := filepath.Join(homeDir, ".claude-all", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", planDir, err)
	}
	planText := "1. Inspect README.txt line 1.\n2. Replace it with the approved text."
	planFile := filepath.Join(planDir, "fresh-plan.md")
	if err := os.WriteFile(planFile, []byte(planText+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", planFile, err)
	}

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-fallback-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-fallback-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-fallback-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-fallback-1",
			"input":       map[string]any{},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	event := requestStarted.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.ThreadID != threadID || event.TurnID != turnID {
		t.Fatalf("unexpected plan request event: %#v", event)
	}
	if event.RequestPrompt == nil || event.RequestPrompt.Body != planText {
		t.Fatalf("expected latest plan file fallback body, got %#v", event.RequestPrompt)
	}
	if lookupStringFromAny(event.Metadata["planBodySource"]) != "latest_plan_file" {
		t.Fatalf("expected latest_plan_file source, got %#v", event.Metadata)
	}
	if lookupStringFromAny(event.Metadata["planFilePath"]) != planFile {
		t.Fatalf("expected plan file path metadata, got %#v", event.Metadata)
	}
}

func TestClaudeTranslatorPlanResolvedUsesToolResultFilePathFallback(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "plan")
	t.Setenv("HOME", t.TempDir())

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-filehint-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-filehint-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-filehint-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-filehint-1",
			"input":       map[string]any{},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	if requestStarted.Events[0].RequestPrompt == nil || requestStarted.Events[0].RequestPrompt.Body != "Claude 计划如下，请确认后继续。" {
		t.Fatalf("expected generic body before file fallback materializes, got %#v", requestStarted.Events[0].RequestPrompt)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-filehint-1",
			Response: map[string]any{
				"decision": "accept",
				"feedback": "Approved. Execute the plan.",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan approval: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one control_response payload, got %d", len(payloads))
	}

	planFile := filepath.Join(t.TempDir(), "tool-result-plan.md")
	planText := "1. Update README.txt line 1.\n2. Save the file without further edits."
	if err := os.WriteFile(planFile, []byte(planText+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", planFile, err)
	}
	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-plan-filehint-1",
					"content":     "User has approved exiting plan mode.",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{
			"feedback": "Approved. Execute the plan.",
			"filePath": planFile,
		},
	})
	if len(resolved.Events) != 1 || resolved.Events[0].Kind != agentproto.EventRequestResolved {
		t.Fatalf("expected request.resolved for plan approval, got %#v", resolved.Events)
	}
	event := resolved.Events[0]
	if event.ThreadID != threadID || event.TurnID != turnID {
		t.Fatalf("unexpected resolved plan event ids: %#v", event)
	}
	if lookupStringFromAny(event.Metadata["body"]) != planText {
		t.Fatalf("expected plan body from tool_result file path, got %#v", event.Metadata)
	}
	if lookupStringFromAny(event.Metadata["planBodySource"]) != "tool_result.filePath" {
		t.Fatalf("expected tool_result.filePath source, got %#v", event.Metadata)
	}
	if lookupStringFromAny(event.Metadata["planFilePath"]) != planFile {
		t.Fatalf("expected resolved plan file path, got %#v", event.Metadata)
	}
}

func TestClaudeTranslatorMaterializesFinalTextOnErrorResult(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	observeClaude(t, tr, map[string]any{
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
	observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Partial answer: ",
			},
		},
	})

	result := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "error_during_execution",
		"is_error":   true,
		"result":     "final fallback answer",
		"session_id": threadID,
		"usage": map[string]any{
			"input_tokens":                1,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
			"output_tokens":               21,
		},
		"modelUsage": map[string]any{
			"mimo-v2.5-pro": map[string]any{"contextWindow": 200000},
		},
		"errors": []any{map[string]any{"message": "tool failed"}},
	})
	if len(result.Events) < 3 {
		t.Fatalf("expected materialized item + usage + completion, got %#v", result.Events)
	}
	if result.Events[0].Kind != agentproto.EventItemStarted || result.Events[1].Kind != agentproto.EventItemCompleted {
		t.Fatalf("expected fallback item lifecycle from result text, got %#v", result.Events)
	}
	if result.Events[1].ItemKind != "agent_message" || lookupStringFromAny(result.Events[1].Metadata["text"]) != "final fallback answer" {
		t.Fatalf("unexpected fallback final text event: %#v", result.Events[1])
	}
	last := result.Events[len(result.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted || last.ThreadID != threadID || last.TurnID != turnID || last.Status != "failed" {
		t.Fatalf("unexpected failed completion event: %#v", last)
	}
	if last.Problem == nil || last.Problem.Code != "claude_turn_failed" {
		t.Fatalf("expected claude_turn_failed problem, got %#v", last.Problem)
	}
}

func startClaudeTurn(t *testing.T, tr *Translator, permissionMode string) (string, string) {
	t.Helper()
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-claude-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": permissionMode,
	})
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{PlanMode: permissionMode},
	}); err != nil {
		t.Fatalf("translate prompt send: %v", err)
	}
	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-start-1",
				"type":    "message",
				"role":    "assistant",
				"model":   "mimo-v2.5-pro",
				"content": []any{},
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected turn.started event, got %#v", started.Events)
	}
	return started.Events[0].ThreadID, started.Events[0].TurnID
}

func observeClaude(t *testing.T, tr *Translator, payload any) Result {
	t.Helper()
	line, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal observe payload: %v", err)
	}
	result, err := tr.ObserveServer(line)
	if err != nil {
		t.Fatalf("ObserveServer(%s): %v", string(line), err)
	}
	return result
}

func decodeFrame(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	return payload
}

func testMapValue(value any) map[string]any {
	current, _ := value.(map[string]any)
	if current == nil {
		return map[string]any{}
	}
	return current
}
