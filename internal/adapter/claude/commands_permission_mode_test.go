package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslatePromptSendUsesClaudePermissionModeMapping(t *testing.T) {
	t.Run("full access emits bypass permissions", func(t *testing.T) {
		tr := NewTranslator("inst-1")
		observeClaude(t, tr, map[string]any{
			"type":           "system",
			"subtype":        "init",
			"session_id":     "session-claude-1",
			"cwd":            "/data/dl/droid",
			"model":          "mimo-v2.5-pro",
			"permissionMode": "default",
		})

		payloads, err := tr.TranslateCommand(agentproto.Command{
			CommandID: "cmd-full",
			Kind:      agentproto.CommandPromptSend,
			Origin:    agentproto.Origin{Surface: "surface-1"},
			Target:    agentproto.Target{ThreadID: "thread-1"},
			Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
			Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeFullAccess, PlanMode: "off"},
		})
		if err != nil {
			t.Fatalf("TranslateCommand: %v", err)
		}
		if len(payloads) != 2 {
			t.Fatalf("expected permission frame + prompt, got %d", len(payloads))
		}
		frame := decodeFrame(t, payloads[0])
		request := testMapValue(frame["request"])
		if request["subtype"] != "set_permission_mode" || request["mode"] != "bypassPermissions" {
			t.Fatalf("unexpected permission frame: %#v", frame)
		}
	})

	t.Run("confirm keeps default permission mode", func(t *testing.T) {
		tr := NewTranslator("inst-1")
		observeClaude(t, tr, map[string]any{
			"type":           "system",
			"subtype":        "init",
			"session_id":     "session-claude-1",
			"cwd":            "/data/dl/droid",
			"model":          "mimo-v2.5-pro",
			"permissionMode": "bypassPermissions",
		})

		payloads, err := tr.TranslateCommand(agentproto.Command{
			CommandID: "cmd-confirm",
			Kind:      agentproto.CommandPromptSend,
			Origin:    agentproto.Origin{Surface: "surface-1"},
			Target:    agentproto.Target{ThreadID: "thread-1"},
			Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
			Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeConfirm, PlanMode: "off"},
		})
		if err != nil {
			t.Fatalf("TranslateCommand: %v", err)
		}
		if len(payloads) != 2 {
			t.Fatalf("expected permission frame + prompt, got %d", len(payloads))
		}
		frame := decodeFrame(t, payloads[0])
		request := testMapValue(frame["request"])
		if request["subtype"] != "set_permission_mode" || request["mode"] != "default" {
			t.Fatalf("unexpected permission frame: %#v", frame)
		}
	})

	t.Run("plan mode emits native plan", func(t *testing.T) {
		tr := NewTranslator("inst-1")
		observeClaude(t, tr, map[string]any{
			"type":           "system",
			"subtype":        "init",
			"session_id":     "session-claude-1",
			"cwd":            "/data/dl/droid",
			"model":          "mimo-v2.5-pro",
			"permissionMode": "default",
		})

		payloads, err := tr.TranslateCommand(agentproto.Command{
			CommandID: "cmd-plan",
			Kind:      agentproto.CommandPromptSend,
			Origin:    agentproto.Origin{Surface: "surface-1"},
			Target:    agentproto.Target{ThreadID: "thread-1"},
			Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
			Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeFullAccess, PlanMode: "on"},
		})
		if err != nil {
			t.Fatalf("TranslateCommand: %v", err)
		}
		if len(payloads) != 2 {
			t.Fatalf("expected permission frame + prompt, got %d", len(payloads))
		}
		frame := decodeFrame(t, payloads[0])
		request := testMapValue(frame["request"])
		if request["subtype"] != "set_permission_mode" || request["mode"] != "plan" {
			t.Fatalf("unexpected permission frame: %#v", frame)
		}
	})
}

func TestObserveClaudeSystemPermissionModeEmitsObservedConfig(t *testing.T) {
	tr := NewTranslator("inst-1")

	initResult := observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-claude-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "plan",
	})
	if len(initResult.Events) != 1 {
		t.Fatalf("expected one config.observed event on init, got %#v", initResult.Events)
	}
	if event := initResult.Events[0]; event.Kind != agentproto.EventConfigObserved || event.ThreadID != "session-claude-1" || event.CWD != "/data/dl/droid" || event.Model != "mimo-v2.5-pro" || event.AccessMode != "" || event.PlanMode != "on" || event.ConfigScope != "thread" {
		t.Fatalf("unexpected init observed config event: %#v", event)
	}

	statusResult := observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "status",
		"permissionMode": "bypassPermissions",
	})
	if len(statusResult.Events) != 1 {
		t.Fatalf("expected one config.observed event on status, got %#v", statusResult.Events)
	}
	if event := statusResult.Events[0]; event.Kind != agentproto.EventConfigObserved || event.AccessMode != agentproto.AccessModeFullAccess || event.PlanMode != "off" {
		t.Fatalf("unexpected status observed config event: %#v", event)
	}
}

func TestClaudePermissionControlResponseRefreshesObservedConfig(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-claude-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-full",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeFullAccess, PlanMode: "off"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	requestID := lookupStringFromAny(decodeFrame(t, payloads[0])["request_id"])

	response := observeClaude(t, tr, map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": requestID,
		},
	})
	if len(response.Events) != 1 {
		t.Fatalf("expected one config.observed event on control_response, got %#v", response.Events)
	}
	if len(response.ResolvedCommandResponses) != 1 || response.ResolvedCommandResponses[0].RequestID != requestID || response.ResolvedCommandResponses[0].RejectMessage != "" {
		t.Fatalf("unexpected resolved command responses: %#v", response.ResolvedCommandResponses)
	}
	if event := response.Events[0]; event.Kind != agentproto.EventConfigObserved || event.AccessMode != agentproto.AccessModeFullAccess || event.PlanMode != "off" {
		t.Fatalf("unexpected control_response observed config event: %#v", event)
	}
}

func TestClaudeAbortCommandClearsPendingTurnAndControlReply(t *testing.T) {
	tr := NewTranslator("inst-1")
	observeClaude(t, tr, map[string]any{
		"type":           "system",
		"subtype":        "init",
		"session_id":     "session-claude-1",
		"cwd":            "/data/dl/droid",
		"model":          "mimo-v2.5-pro",
		"permissionMode": "default",
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-failed",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeFullAccess, PlanMode: "off"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("expected permission frame + prompt, got %#v", payloads)
	}
	if len(tr.pendingTurns) != 1 || len(tr.pendingControlReplies) != 1 {
		t.Fatalf("expected staged pending turn + control reply, turns=%#v replies=%#v", tr.pendingTurns, tr.pendingControlReplies)
	}

	failedRequestID := lookupStringFromAny(decodeFrame(t, payloads[0])["request_id"])
	tr.AbortCommand("cmd-failed")

	if len(tr.pendingTurns) != 0 || len(tr.pendingControlReplies) != 0 {
		t.Fatalf("expected failed command state to be cleared, turns=%#v replies=%#v", tr.pendingTurns, tr.pendingControlReplies)
	}
	response := observeClaude(t, tr, map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"request_id": failedRequestID,
		},
	})
	if len(response.Events) != 0 || len(response.ResolvedCommandResponses) != 0 {
		t.Fatalf("expected late control response for aborted command to be ignored, got %#v", response)
	}

	_, err = tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-ok",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello again"}}},
		Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeConfirm, PlanMode: "off"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand second command: %v", err)
	}
	if len(tr.pendingTurns) != 1 || tr.pendingTurns[0] == nil || tr.pendingTurns[0].CommandID != "cmd-ok" {
		t.Fatalf("expected only second command to remain pending, got %#v", tr.pendingTurns)
	}

	result := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "success",
		"session_id": "session-claude-1",
		"result":     "done",
	})
	if len(result.Events) < 2 {
		t.Fatalf("expected turn events for second command, got %#v", result.Events)
	}
	if result.Events[0].Kind != agentproto.EventTurnStarted || result.Events[0].CommandID != "cmd-ok" {
		t.Fatalf("expected second command to own next turn.start, got %#v", result.Events[0])
	}
}
