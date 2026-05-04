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
	if event := response.Events[0]; event.Kind != agentproto.EventConfigObserved || event.AccessMode != agentproto.AccessModeFullAccess || event.PlanMode != "off" {
		t.Fatalf("unexpected control_response observed config event: %#v", event)
	}
}
