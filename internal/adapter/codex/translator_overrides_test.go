package codex

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslatePromptSendAppliesOverridesToExistingThreadTurnStart(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project","collaborationMode":{"mode":"custom","settings":{"model":"gpt-5.3-codex","reasoning_effort":"medium"}}}}`)); err != nil {
		t.Fatalf("observe client turn start: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{ChatID: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{Model: "gpt-5.4", ReasoningEffort: "high"},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native command, got %d", len(commands))
	}
	var turnStart map[string]any
	if err := json.Unmarshal(commands[0], &turnStart); err != nil {
		t.Fatalf("unmarshal turn/start: %v", err)
	}
	params, _ := turnStart["params"].(map[string]any)
	if params["model"] != "gpt-5.4" || params["effort"] != "high" {
		t.Fatalf("expected top-level overrides in turn/start, got %#v", params)
	}
	if params["approvalPolicy"] != "never" || !reflect.DeepEqual(params["sandboxPolicy"], map[string]any{"type": "dangerFullAccess"}) {
		t.Fatalf("expected default full access in turn/start, got %#v", params)
	}
	settings, _ := params["collaborationMode"].(map[string]any)
	settingsMap, _ := settings["settings"].(map[string]any)
	if settingsMap["model"] != "gpt-5.4" || settingsMap["reasoning_effort"] != "high" {
		t.Fatalf("expected collaborationMode settings override, got %#v", params["collaborationMode"])
	}
}

func TestTranslatePromptSendAppliesOverridesToNewThreadStartAndFollowupTurn(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{ChatID: "surface-1"},
		Target:    agentproto.Target{CWD: "/tmp/project"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{Model: "gpt-5.4", ReasoningEffort: "high"},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one thread/start command, got %d", len(commands))
	}
	var threadStart map[string]any
	if err := json.Unmarshal(commands[0], &threadStart); err != nil {
		t.Fatalf("unmarshal thread/start: %v", err)
	}
	params, _ := threadStart["params"].(map[string]any)
	if params["model"] != "gpt-5.4" {
		t.Fatalf("expected thread/start override model, got %#v", params)
	}
	if params["approvalPolicy"] != "never" || params["sandbox"] != "danger-full-access" {
		t.Fatalf("expected default full access in thread/start, got %#v", params)
	}
	config, _ := params["config"].(map[string]any)
	if config["model_reasoning_effort"] != "high" {
		t.Fatalf("expected thread/start reasoning override in config, got %#v", params)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread-created"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if len(result.OutboundToAgent) != 1 {
		t.Fatalf("expected followup turn/start, got %#v", result)
	}
	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToAgent[0], &turnStart); err != nil {
		t.Fatalf("unmarshal followup turn/start: %v", err)
	}
	turnParams, _ := turnStart["params"].(map[string]any)
	if turnParams["model"] != "gpt-5.4" || turnParams["effort"] != "high" {
		t.Fatalf("expected followup turn/start overrides, got %#v", turnParams)
	}
	if turnParams["approvalPolicy"] != "never" || !reflect.DeepEqual(turnParams["sandboxPolicy"], map[string]any{"type": "dangerFullAccess"}) {
		t.Fatalf("expected default full access in followup turn/start, got %#v", turnParams)
	}
}

func TestTranslatePromptSendConfirmAccessModeOverridesPolicies(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{ChatID: "surface-1"},
		Target:    agentproto.Target{CWD: "/tmp/project"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{AccessMode: agentproto.AccessModeConfirm},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one thread/start command, got %d", len(commands))
	}

	var threadStart map[string]any
	if err := json.Unmarshal(commands[0], &threadStart); err != nil {
		t.Fatalf("unmarshal thread/start: %v", err)
	}
	params, _ := threadStart["params"].(map[string]any)
	if params["approvalPolicy"] != "on-request" || params["sandbox"] != "workspace-write" {
		t.Fatalf("expected confirm mode on thread/start, got %#v", params)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread-created"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if len(result.OutboundToAgent) != 1 {
		t.Fatalf("expected followup turn/start, got %#v", result)
	}
	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToAgent[0], &turnStart); err != nil {
		t.Fatalf("unmarshal followup turn/start: %v", err)
	}
	turnParams, _ := turnStart["params"].(map[string]any)
	if turnParams["approvalPolicy"] != "on-request" || !reflect.DeepEqual(turnParams["sandboxPolicy"], map[string]any{"type": "workspaceWrite"}) {
		t.Fatalf("expected confirm mode on followup turn/start, got %#v", turnParams)
	}
}

func TestStructuredLocalTurnStartDoesNotOverwriteReusableTurnTemplate(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project","collaborationMode":{"mode":"custom","settings":{"model":"gpt-5.3-codex","reasoning_effort":"medium"}}}}`)); err != nil {
		t.Fatalf("observe baseline turn start: %v", err)
	}
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project","outputSchema":{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}}},"collaborationMode":{"mode":"custom","settings":{"model":"gpt-5.3-codex","reasoning_effort":"low"}}}}`)); err != nil {
		t.Fatalf("observe structured helper turn start: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one turn/start command, got %d", len(commands))
	}

	var turnStart map[string]any
	if err := json.Unmarshal(commands[0], &turnStart); err != nil {
		t.Fatalf("unmarshal turn/start: %v", err)
	}
	params, _ := turnStart["params"].(map[string]any)
	if _, exists := params["outputSchema"]; exists {
		t.Fatalf("structured helper output schema leaked into remote turn/start: %#v", params)
	}
	collaborationMode, _ := params["collaborationMode"].(map[string]any)
	settings, _ := collaborationMode["settings"].(map[string]any)
	if settings["reasoning_effort"] != "medium" {
		t.Fatalf("structured helper turn overwrote reusable reasoning config, got %#v", params["collaborationMode"])
	}
}

func TestInternalHelperThreadStartDoesNotPoisonRemoteThreadStart(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"id":"helper-thread-1","method":"thread/start","params":{"cwd":"/tmp/project","approvalPolicy":"never","sandbox":"read-only","ephemeral":true,"persistExtendedHistory":false,"model":"gpt-5.4","config":{"model_reasoning_effort":"low"}}}`)); err != nil {
		t.Fatalf("observe helper thread start: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one thread/start command, got %d", len(commands))
	}

	var threadStart map[string]any
	if err := json.Unmarshal(commands[0], &threadStart); err != nil {
		t.Fatalf("unmarshal thread/start: %v", err)
	}
	params, _ := threadStart["params"].(map[string]any)
	if params["approvalPolicy"] != "never" {
		t.Fatalf("helper thread start overwrote approval policy, got %#v", params)
	}
	if params["sandbox"] != "danger-full-access" {
		t.Fatalf("expected default full access sandbox, got %#v", params)
	}
	if _, exists := params["ephemeral"]; exists {
		t.Fatalf("helper thread start leaked ephemeral flag into remote thread/start: %#v", params)
	}
	if params["model"] != nil {
		t.Fatalf("helper thread start leaked model into remote thread/start: %#v", params)
	}
}
