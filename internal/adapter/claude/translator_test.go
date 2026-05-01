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
	if assistant.Events[0].Kind != agentproto.EventItemStarted || assistant.Events[0].ItemKind != "command_execution" {
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
	if completed.Events[0].Kind != agentproto.EventItemCompleted || completed.Events[0].ItemKind != "command_execution" || completed.Events[0].Status != "completed" {
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

func TestClaudeTranslatorDirectFailureWithoutMessageStartStillReconcilesTurn(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-claude-auth",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-auth-fail",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-auth"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "你好"}}},
	}); err != nil {
		t.Fatalf("translate prompt send: %v", err)
	}

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-auth-fail",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "Not logged in · Please run /login",
				},
			},
		},
	})
	if len(assistant.Events) != 3 {
		t.Fatalf("expected turn start + assistant item lifecycle, got %#v", assistant.Events)
	}
	if assistant.Events[0].Kind != agentproto.EventTurnStarted {
		t.Fatalf("expected first event to start turn, got %#v", assistant.Events[0])
	}
	if assistant.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface || assistant.Events[0].Initiator.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected assistant path to preserve remote initiator, got %#v", assistant.Events[0].Initiator)
	}
	if assistant.Events[1].Kind != agentproto.EventItemStarted || assistant.Events[2].Kind != agentproto.EventItemCompleted {
		t.Fatalf("expected assistant message lifecycle, got %#v", assistant.Events)
	}
	threadID := assistant.Events[0].ThreadID
	turnID := assistant.Events[0].TurnID
	if threadID != "session-claude-auth" {
		t.Fatalf("expected session thread ID to be used, got %q", threadID)
	}
	if got := lookupStringFromAny(assistant.Events[2].Metadata["text"]); got != "Not logged in · Please run /login" {
		t.Fatalf("unexpected assistant text %q", got)
	}

	result := observeClaude(t, tr, map[string]any{
		"type":     "result",
		"subtype":  "error_during_execution",
		"is_error": true,
		"result":   "Not logged in · Please run /login",
		"errors": []any{
			map[string]any{"message": "authentication_failed"},
		},
	})
	if len(result.Events) != 1 {
		t.Fatalf("expected only turn completion after direct assistant failure, got %#v", result.Events)
	}
	completed := result.Events[0]
	if completed.Kind != agentproto.EventTurnCompleted || completed.ThreadID != threadID || completed.TurnID != turnID || completed.Status != "failed" {
		t.Fatalf("unexpected completion event: %#v", completed)
	}
	if completed.Problem == nil || completed.Problem.Code != "claude_turn_failed" {
		t.Fatalf("expected claude_turn_failed problem, got %#v", completed.Problem)
	}
	if tr.activeTurn != nil || len(tr.pendingTurns) != 0 {
		t.Fatalf("expected translator turn state to be fully reconciled, active=%#v pending=%#v", tr.activeTurn, tr.pendingTurns)
	}
}

func TestClaudeTranslatorDoesNotInventSyntheticThreadBeforeSessionInit(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-pre-init",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "你好"}}},
	}); err != nil {
		t.Fatalf("translate prompt send: %v", err)
	}

	started := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-start-pre-init",
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
	if started.Events[0].ThreadID != "" {
		t.Fatalf("expected no synthetic thread id before session init, got %#v", started.Events[0])
	}

	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-after-init",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})
	result := observeClaude(t, tr, map[string]any{
		"type":     "result",
		"subtype":  "success",
		"is_error": false,
		"result":   "done",
	})
	last := result.Events[len(result.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted || last.ThreadID != "session-after-init" {
		t.Fatalf("expected completion to use authoritative session id, got %#v", last)
	}
}

func TestClaudeTranslatorInitRefreshesPendingTurnToResumedSession(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "bootstrap-session",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-resume",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "resume-session-1"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "resume this session"}}},
	}); err != nil {
		t.Fatalf("translate prompt send: %v", err)
	}

	started := observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "resume-session-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})
	if len(started.Events) != 0 {
		t.Fatalf("expected init refresh to stay silent, got %#v", started.Events)
	}

	started = observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      "msg-start-resume",
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
	if started.Events[0].ThreadID != "resume-session-1" {
		t.Fatalf("expected resumed session init to refresh pending turn thread id, got %#v", started.Events[0])
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

func TestClaudeTranslatorReconcilesFinalAssistantTextOntoStreamingBlock(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	thinkingStart := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type":     "thinking",
				"thinking": "",
			},
		},
	})
	if len(thinkingStart.Events) != 1 || thinkingStart.Events[0].Kind != agentproto.EventItemStarted || thinkingStart.Events[0].ItemKind != "reasoning_summary" {
		t.Fatalf("expected thinking block start to open a reasoning summary item, got %#v", thinkingStart.Events)
	}

	streamStart := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_start",
			"index": 1,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		},
	})
	if len(streamStart.Events) != 1 || streamStart.Events[0].Kind != agentproto.EventItemStarted {
		t.Fatalf("expected one text item.started event, got %#v", streamStart.Events)
	}
	itemID := streamStart.Events[0].ItemID

	streamDelta := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_delta",
			"index": 1,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "你好！有什么我可以帮助你的吗？",
			},
		},
	})
	if len(streamDelta.Events) != 1 || streamDelta.Events[0].Kind != agentproto.EventItemDelta {
		t.Fatalf("expected one text item.delta event, got %#v", streamDelta.Events)
	}

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-final-text-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "你好！有什么我可以帮助你的吗？",
				},
			},
		},
	})
	if len(assistant.Events) != 1 || assistant.Events[0].Kind != agentproto.EventItemCompleted {
		t.Fatalf("expected assistant final text to complete existing stream item, got %#v", assistant.Events)
	}
	if assistant.Events[0].ItemID != itemID {
		t.Fatalf("expected assistant final text to reuse stream item id %q, got %#v", itemID, assistant.Events[0])
	}
	if assistant.Events[0].ThreadID != threadID || assistant.Events[0].TurnID != turnID {
		t.Fatalf("unexpected assistant completion ids: %#v", assistant.Events[0])
	}

	streamStop := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_stop",
			"index": 1,
		},
	})
	if len(streamStop.Events) != 0 {
		t.Fatalf("expected redundant stream stop to stay silent after assistant reconciliation, got %#v", streamStop.Events)
	}
}

func TestClaudeTranslatorIgnoresRedundantAssistantTextAfterStreamCompletion(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

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
		t.Fatalf("expected one text item.started event, got %#v", streamStart.Events)
	}
	itemID := streamStart.Events[0].ItemID

	observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "already completed",
			},
		},
	})
	streamStop := observeClaude(t, tr, map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		},
	})
	if len(streamStop.Events) != 1 || streamStop.Events[0].Kind != agentproto.EventItemCompleted {
		t.Fatalf("expected stream stop to complete text item, got %#v", streamStop.Events)
	}
	if streamStop.Events[0].ItemID != itemID || streamStop.Events[0].ThreadID != threadID || streamStop.Events[0].TurnID != turnID {
		t.Fatalf("unexpected stream completion ids: %#v", streamStop.Events[0])
	}

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-final-text-2",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "already completed",
				},
			},
		},
	})
	if len(assistant.Events) != 0 {
		t.Fatalf("expected assistant final snapshot to avoid duplicating completed text item, got %#v", assistant.Events)
	}
}

func TestClaudeTranslatorTodoWriteProjectsToProcessPlan(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "tool_use",
					"id":   "call-plan-1",
					"name": "TodoWrite",
					"input": map[string]any{
						"todos": []any{
							map[string]any{"content": "Gather evidence", "status": "in_progress", "activeForm": "Gathering evidence"},
							map[string]any{"content": "Write summary", "status": "pending", "activeForm": "Writing summary"},
						},
					},
				},
			},
		},
	})
	if len(assistant.Events) != 0 {
		t.Fatalf("expected TodoWrite assistant frame not to emit immediate visible item, got %#v", assistant.Events)
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-plan-1",
					"content":     "Todos have been modified successfully.",
				},
			},
		},
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one process plan completion event, got %#v", completed.Events)
	}
	event := completed.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "process_plan" || event.Status != "completed" {
		t.Fatalf("unexpected TodoWrite projection: %#v", event)
	}
	if snapshot, ok := event.Metadata["planSnapshot"].(map[string]any); !ok || snapshot == nil {
		t.Fatalf("expected plan snapshot metadata, got %#v", event.Metadata)
	}
}

func TestClaudeTranslatorTaskProjectsToDelegatedTask(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-1",
					"name":  "Task",
					"input": map[string]any{"subagent_type": "Explore", "description": "Audit the repository"},
				},
			},
		},
	})
	if len(assistant.Events) != 1 {
		t.Fatalf("expected delegated task item start, got %#v", assistant.Events)
	}
	if assistant.Events[0].Kind != agentproto.EventItemStarted || assistant.Events[0].ItemKind != "delegated_task" {
		t.Fatalf("unexpected Task start projection: %#v", assistant.Events[0])
	}
	if got := lookupStringFromAny(assistant.Events[0].Metadata["description"]); got != "Audit the repository" {
		t.Fatalf("expected delegated task description metadata, got %#v", assistant.Events[0].Metadata)
	}
}

func TestClaudeTranslatorTaskStopCompletesDelegatedTaskViaParentToolUseID(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":                 "msg-task-parent-1",
			"type":               "message",
			"role":               "assistant",
			"model":              "mimo-v2.5-pro",
			"parent_tool_use_id": nil,
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-parent-1",
					"name":  "Task",
					"input": map[string]any{"subagent_type": "Explore", "description": "Audit the repository"},
				},
			},
		},
		"parent_tool_use_id": nil,
	})
	if len(started.Events) != 1 || started.Events[0].ItemKind != "delegated_task" {
		t.Fatalf("expected delegated task start, got %#v", started.Events)
	}
	itemID := started.Events[0].ItemID

	hiddenStop := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-stop-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-stop-1",
					"name":  "TaskStop",
					"input": map[string]any{},
				},
			},
		},
		"parent_tool_use_id": "call-task-parent-1",
	})
	if len(hiddenStop.Events) != 0 {
		t.Fatalf("expected TaskStop assistant frame to stay hidden, got %#v", hiddenStop.Events)
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-task-stop-1",
					"content":     "Subtask completed",
					"is_error":    false,
				},
			},
		},
		"parent_tool_use_id": "call-task-parent-1",
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one delegated task completion event, got %#v", completed.Events)
	}
	event := completed.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "delegated_task" || event.Status != "completed" {
		t.Fatalf("unexpected delegated task completion projection: %#v", event)
	}
	if event.ItemID != itemID {
		t.Fatalf("expected delegated task to reuse parent item id %q, got %#v", itemID, event)
	}
	if got := lookupStringFromAny(event.Metadata["description"]); got != "Audit the repository" {
		t.Fatalf("expected parent task metadata to survive completion, got %#v", event.Metadata)
	}
}

func TestClaudeTranslatorTaskStopFailureProjectsToDelegatedTaskFailure(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-parent-2",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-parent-2",
					"name":  "Task",
					"input": map[string]any{"subagent_type": "Explore", "description": "Gather evidence"},
				},
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].ItemKind != "delegated_task" {
		t.Fatalf("expected delegated task start, got %#v", started.Events)
	}

	hiddenStop := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-stop-2",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-stop-2",
					"name":  "TaskStop",
					"input": map[string]any{},
				},
			},
		},
		"parent_tool_use_id": "call-task-parent-2",
	})
	if len(hiddenStop.Events) != 0 {
		t.Fatalf("expected TaskStop assistant frame to stay hidden, got %#v", hiddenStop.Events)
	}

	failed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-task-stop-2",
					"content":     "Subtask failed",
					"is_error":    true,
				},
			},
		},
		"parent_tool_use_id": "call-task-parent-2",
		"tool_use_result": map[string]any{
			"errorMessage": "subtask exploded",
		},
	})
	if len(failed.Events) != 1 {
		t.Fatalf("expected one delegated task failed event, got %#v", failed.Events)
	}
	event := failed.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "delegated_task" || event.Status != "failed" {
		t.Fatalf("unexpected delegated task failed projection: %#v", event)
	}
	if got := lookupStringFromAny(event.Metadata["errorMessage"]); got != "subtask exploded" {
		t.Fatalf("expected failed delegated task to keep error metadata, got %#v", event.Metadata)
	}
}

func TestClaudeTranslatorTaskOutputProjectsHiddenDelegatedTaskDelta(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-parent-3",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-parent-3",
					"name":  "Task",
					"input": map[string]any{"subagent_type": "Explore", "description": "Collect evidence"},
				},
			},
		},
	})
	if len(started.Events) != 1 || started.Events[0].ItemKind != "delegated_task" {
		t.Fatalf("expected delegated task start, got %#v", started.Events)
	}

	hiddenOutput := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-output-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-output-1",
					"name":  "TaskOutput",
					"input": map[string]any{},
				},
			},
		},
		"parent_tool_use_id": "call-task-parent-3",
	})
	if len(hiddenOutput.Events) != 0 {
		t.Fatalf("expected TaskOutput assistant frame to stay hidden, got %#v", hiddenOutput.Events)
	}

	delta := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-task-output-1",
					"content":     "Scanned 12 files",
					"is_error":    false,
				},
			},
		},
		"parent_tool_use_id": "call-task-parent-3",
	})
	if len(delta.Events) != 1 {
		t.Fatalf("expected one hidden delegated task delta event, got %#v", delta.Events)
	}
	event := delta.Events[0]
	if event.Kind != agentproto.EventItemDelta || event.ItemKind != "delegated_task" || event.Delta != "Scanned 12 files" {
		t.Fatalf("unexpected delegated task delta projection: %#v", event)
	}
}

func TestClaudeTranslatorEditProjectsToFileChange(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-edit-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "tool_use",
					"id":   "call-edit-1",
					"name": "Edit",
					"input": map[string]any{
						"file_path":   "internal/app/app.go",
						"old_string":  "old line",
						"new_string":  "new line",
						"replace_all": false,
					},
				},
			},
		},
	})
	if len(started.Events) != 1 {
		t.Fatalf("expected one Edit start event, got %#v", started.Events)
	}
	startEvent := started.Events[0]
	if startEvent.Kind != agentproto.EventItemStarted || startEvent.ItemKind != "file_change" {
		t.Fatalf("unexpected Edit start projection: %#v", startEvent)
	}
	if len(startEvent.FileChanges) != 1 {
		t.Fatalf("expected file change payload on start, got %#v", startEvent)
	}
	if startEvent.FileChanges[0].Path != "internal/app/app.go" || startEvent.FileChanges[0].Kind != agentproto.FileChangeUpdate {
		t.Fatalf("unexpected started file change payload: %#v", startEvent.FileChanges)
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-edit-1",
					"content":     "The file has been updated successfully.",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{
			"filePath":        "internal/app/app.go",
			"oldString":       "old line",
			"newString":       "new line",
			"replaceAll":      false,
			"structuredPatch": "@@ -1 +1 @@\n-old line\n+new line",
		},
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one Edit completion event, got %#v", completed.Events)
	}
	event := completed.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "file_change" || event.Status != "completed" {
		t.Fatalf("unexpected Edit completion projection: %#v", event)
	}
	if len(event.FileChanges) != 1 {
		t.Fatalf("expected completed file change payload, got %#v", event)
	}
	if event.FileChanges[0].Path != "internal/app/app.go" || event.FileChanges[0].Diff != "@@ -1 +1 @@\n-old line\n+new line" {
		t.Fatalf("unexpected completed file change payload: %#v", event.FileChanges)
	}
}

func TestClaudeTranslatorWriteProjectsToFileChange(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-write-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "tool_use",
					"id":   "call-write-1",
					"name": "Write",
					"input": map[string]any{
						"file_path": "/tmp/readme.md",
						"content":   "# hello\nworld\n",
					},
				},
			},
		},
	})
	if len(started.Events) != 1 {
		t.Fatalf("expected one Write start event, got %#v", started.Events)
	}
	startEvent := started.Events[0]
	if startEvent.Kind != agentproto.EventItemStarted || startEvent.ItemKind != "file_change" {
		t.Fatalf("unexpected Write start projection: %#v", startEvent)
	}
	if len(startEvent.FileChanges) != 1 || startEvent.FileChanges[0].Path != "/tmp/readme.md" || startEvent.FileChanges[0].Kind != agentproto.FileChangeAdd {
		t.Fatalf("unexpected Write start file change payload: %#v", startEvent.FileChanges)
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-write-1",
					"content":     "File created successfully at: /tmp/readme.md",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{
			"type":         "create",
			"filePath":     "/tmp/readme.md",
			"content":      "# hello\nworld\n",
			"originalFile": "",
		},
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one Write completion event, got %#v", completed.Events)
	}
	event := completed.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "file_change" || event.Status != "completed" {
		t.Fatalf("unexpected Write completion projection: %#v", event)
	}
	if len(event.FileChanges) != 1 {
		t.Fatalf("expected completed Write file change payload, got %#v", event)
	}
	if event.FileChanges[0].Path != "/tmp/readme.md" || event.FileChanges[0].Kind != agentproto.FileChangeAdd {
		t.Fatalf("unexpected completed Write file change payload: %#v", event.FileChanges)
	}
	if event.FileChanges[0].Diff == "" {
		t.Fatalf("expected completed Write diff to be populated, got %#v", event.FileChanges[0])
	}
}

func TestClaudeTranslatorNotebookEditProjectsToFileChange(t *testing.T) {
	tr := NewTranslator("inst-1")
	_, _ = startClaudeTurn(t, tr, "default")

	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-notebook-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type": "tool_use",
					"id":   "call-notebook-1",
					"name": "NotebookEdit",
					"input": map[string]any{
						"notebook_path": "/tmp/demo.ipynb",
						"cell_id":       "cell-1",
						"new_source":    "print('hello')",
						"cell_type":     "code",
						"edit_mode":     "replace",
					},
				},
			},
		},
	})
	if len(started.Events) != 1 {
		t.Fatalf("expected one NotebookEdit start event, got %#v", started.Events)
	}
	startEvent := started.Events[0]
	if startEvent.Kind != agentproto.EventItemStarted || startEvent.ItemKind != "file_change" {
		t.Fatalf("unexpected NotebookEdit start projection: %#v", startEvent)
	}
	if len(startEvent.FileChanges) != 1 || startEvent.FileChanges[0].Path != "/tmp/demo.ipynb" {
		t.Fatalf("unexpected NotebookEdit start file change payload: %#v", startEvent.FileChanges)
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-notebook-1",
					"content":     "Notebook cell updated successfully.",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{
			"notebook_path": "/tmp/demo.ipynb",
			"cell_id":       "cell-1",
			"new_source":    "print('hello')",
			"cell_type":     "code",
			"edit_mode":     "replace",
		},
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one NotebookEdit completion event, got %#v", completed.Events)
	}
	event := completed.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "file_change" || event.Status != "completed" {
		t.Fatalf("unexpected NotebookEdit completion projection: %#v", event)
	}
	if len(event.FileChanges) != 1 || event.FileChanges[0].Path != "/tmp/demo.ipynb" || event.FileChanges[0].Diff == "" {
		t.Fatalf("unexpected NotebookEdit completed file change payload: %#v", event.FileChanges)
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
	if started.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface || started.Events[0].Initiator.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected turn.started to preserve remote initiator, got %#v", started.Events[0].Initiator)
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
