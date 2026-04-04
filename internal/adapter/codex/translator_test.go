package codex

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveClientTurnStartProducesLocalInteraction(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`))
	if err != nil {
		t.Fatalf("observe client: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(result.Events))
	}
	if result.Events[0].Kind != agentproto.EventLocalInteractionObserved || result.Events[0].Action != "turn_start" {
		t.Fatalf("unexpected event: %#v", result.Events[0])
	}
}

func TestObserveClientTurnStartEmitsObservedThreadConfig(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project","collaborationMode":{"mode":"custom","settings":{"model":"gpt-5.4","reasoning_effort":"high"}}}}`))
	if err != nil {
		t.Fatalf("observe client: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected config event plus local interaction, got %#v", result.Events)
	}
	if result.Events[0].Kind != agentproto.EventConfigObserved {
		t.Fatalf("expected first event to be config observation, got %#v", result.Events[0])
	}
	if result.Events[0].ConfigScope != "thread" || result.Events[0].Model != "gpt-5.4" || result.Events[0].ReasoningEffort != "high" {
		t.Fatalf("unexpected observed config event: %#v", result.Events[0])
	}
}

func TestObserveClientTurnSteerProducesLocalInteraction(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveClient([]byte(`{"method":"turn/steer","params":{"threadId":"thread-1"}}`))
	if err != nil {
		t.Fatalf("observe client: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Action != "turn_steer" {
		t.Fatalf("unexpected steer event: %#v", result.Events)
	}

	started, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(started.Events) != 1 || started.Events[0].Initiator.Kind != agentproto.InitiatorLocalUI {
		t.Fatalf("expected local initiator after steer, got %#v", started.Events)
	}
}

func TestTranslatePromptSendToNewThreadAndFollowupTurnStart(t *testing.T) {
	tr := NewTranslator("inst-1")
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandPromptSend,
		Origin: agentproto.Origin{
			Surface:   "feishu",
			ChatID:    "surface-1",
			UserID:    "user-1",
			MessageID: "msg-1",
		},
		Target: agentproto.Target{
			CWD: "/tmp/project",
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}},
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native command, got %d", len(commands))
	}
	var start map[string]any
	if err := json.Unmarshal(commands[0], &start); err != nil {
		t.Fatalf("unmarshal thread/start: %v", err)
	}
	if start["method"] != "thread/start" {
		t.Fatalf("expected thread/start, got %#v", start)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread-created"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if !result.Suppress || len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected suppressed followup turn/start, got %#v", result)
	}
	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToCodex[0], &turnStart); err != nil {
		t.Fatalf("unmarshal turn/start: %v", err)
	}
	if turnStart["method"] != "turn/start" {
		t.Fatalf("expected turn/start, got %#v", turnStart)
	}
}

func TestObserveTurnStartedMarksRemoteInitiator(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}
	_, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatalf("observe server: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("unexpected turn started result: %#v", result.Events)
	}
}

func TestObserveLocalNewThreadStartMarksLocalInitiator(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client turn start: %v", err)
	}

	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-created","cwd":"/tmp/project"}}}`)); err != nil {
		t.Fatalf("observe thread started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-created","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Initiator.Kind != agentproto.InitiatorLocalUI {
		t.Fatalf("expected local initiator for new-thread local turn, got %#v", result.Events)
	}
}

func TestObserveTurnStartedPrefersRemoteInitiatorWhenLocalMarkerIsStale(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/steer","params":{"threadId":"thread-1"}}`)); err != nil {
		t.Fatalf("observe client turn steer: %v", err)
	}
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatalf("observe server: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote initiator to override stale local marker, got %#v", result.Events)
	}
}

func TestRemoteNewThreadStartClearsStaleLocalNewThreadMarker(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"turn/start","params":{"cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client turn start: %v", err)
	}
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one thread/start command, got %d", len(commands))
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-created","cwd":"/tmp/project"}}}`)); err != nil {
		t.Fatalf("observe thread started: %v", err)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread-created"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if !result.Suppress || len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected suppressed followup turn/start, got %#v", result)
	}
	started, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-created","turn":{"id":"turn-1"}}}`))
	if err != nil {
		t.Fatalf("observe turn started: %v", err)
	}
	if len(started.Events) != 1 || started.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote initiator for remotely created thread, got %#v", started.Events)
	}
}

func TestTranslatePromptSendToExistingThreadResumesWhenTargetDiffersFromCurrent(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/one"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-2", CWD: "/tmp/two"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native command, got %d", len(commands))
	}

	var resume map[string]any
	if err := json.Unmarshal(commands[0], &resume); err != nil {
		t.Fatalf("unmarshal thread/resume: %v", err)
	}
	if resume["method"] != "thread/resume" {
		t.Fatalf("expected thread/resume, got %#v", resume)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-resume-0","result":{}}`))
	if err != nil {
		t.Fatalf("observe resume response: %v", err)
	}
	if !result.Suppress || len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected suppressed followup turn/start, got %#v", result)
	}

	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToCodex[0], &turnStart); err != nil {
		t.Fatalf("unmarshal turn/start: %v", err)
	}
	if turnStart["method"] != "turn/start" {
		t.Fatalf("expected turn/start, got %#v", turnStart)
	}
	params, _ := turnStart["params"].(map[string]any)
	if params["threadId"] != "thread-2" {
		t.Fatalf("expected followup turn/start to target thread-2, got %#v", turnStart)
	}
}

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
	config, _ := params["config"].(map[string]any)
	if config["model_reasoning_effort"] != "high" {
		t.Fatalf("expected thread/start reasoning override in config, got %#v", params)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-start-0","result":{"thread":{"id":"thread-created"}}}`))
	if err != nil {
		t.Fatalf("observe server response: %v", err)
	}
	if len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected followup turn/start, got %#v", result)
	}
	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToCodex[0], &turnStart); err != nil {
		t.Fatalf("unmarshal followup turn/start: %v", err)
	}
	turnParams, _ := turnStart["params"].(map[string]any)
	if turnParams["model"] != "gpt-5.4" || turnParams["effort"] != "high" {
		t.Fatalf("expected followup turn/start overrides, got %#v", turnParams)
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
	if params["approvalPolicy"] != "on-request" {
		t.Fatalf("helper thread start overwrote approval policy, got %#v", params)
	}
	if _, exists := params["ephemeral"]; exists {
		t.Fatalf("helper thread start leaked ephemeral flag into remote thread/start: %#v", params)
	}
	if params["model"] != nil {
		t.Fatalf("helper thread start leaked model into remote thread/start: %#v", params)
	}
}

func TestInternalHelperThreadLifecycleIsAnnotatedInsteadOfSuppressed(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"id":"helper-thread-1","method":"thread/start","params":{"cwd":"/tmp/project","approvalPolicy":"never","sandbox":"read-only","ephemeral":true,"persistExtendedHistory":false}}`)); err != nil {
		t.Fatalf("observe helper thread start: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"helper-thread-1","result":{"thread":{"id":"helper-thread"}}}`))
	if err != nil {
		t.Fatalf("observe helper thread response: %v", err)
	}
	if result.Suppress {
		t.Fatalf("helper thread response must still reach parent stdout: %#v", result)
	}
	if len(result.Events) != 0 {
		t.Fatalf("helper thread response should not emit canonical events, got %#v", result.Events)
	}

	started, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"helper-thread","cwd":"/tmp/project"}}}`))
	if err != nil {
		t.Fatalf("observe helper thread started: %v", err)
	}
	if started.Suppress {
		t.Fatalf("helper thread notification must still reach parent stdout: %#v", started)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected annotated helper thread event, got %#v", started.Events)
	}
	if started.Events[0].Kind != agentproto.EventThreadDiscovered ||
		started.Events[0].TrafficClass != agentproto.TrafficClassInternalHelper ||
		started.Events[0].Initiator.Kind != agentproto.InitiatorInternalHelper {
		t.Fatalf("unexpected helper thread event: %#v", started.Events[0])
	}
}

func TestStructuredHelperTurnLifecycleIsAnnotatedInsteadOfSuppressed(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"id":"helper-thread-1","method":"thread/start","params":{"cwd":"/tmp/project","approvalPolicy":"never","sandbox":"read-only","ephemeral":true,"persistExtendedHistory":false}}`)); err != nil {
		t.Fatalf("observe helper thread start: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"id":"helper-thread-1","result":{"thread":{"id":"helper-thread"}}}`)); err != nil {
		t.Fatalf("observe helper thread response: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"helper-thread","cwd":"/tmp/project"}}}`)); err != nil {
		t.Fatalf("observe helper thread started: %v", err)
	}
	if _, err := tr.ObserveClient([]byte(`{"id":"helper-turn-1","method":"turn/start","params":{"threadId":"helper-thread","cwd":"/tmp/project","outputSchema":{"type":"object","properties":{"title":{"type":"string"}}}}}`)); err != nil {
		t.Fatalf("observe helper turn start: %v", err)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"helper-turn-1","result":{"turn":{"id":"turn-helper"}}}`))
	if err != nil {
		t.Fatalf("observe helper turn response: %v", err)
	}
	if result.Suppress {
		t.Fatalf("helper turn response must still reach parent stdout: %#v", result)
	}
	if len(result.Events) != 0 {
		t.Fatalf("helper turn response should not emit canonical events, got %#v", result.Events)
	}

	started, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"helper-thread","turn":{"id":"turn-helper"}}}`))
	if err != nil {
		t.Fatalf("observe helper turn started: %v", err)
	}
	if started.Suppress {
		t.Fatalf("helper turn started must still reach parent stdout: %#v", started)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected annotated helper turn started event, got %#v", started.Events)
	}
	if started.Events[0].Kind != agentproto.EventTurnStarted ||
		started.Events[0].TrafficClass != agentproto.TrafficClassInternalHelper ||
		started.Events[0].Initiator.Kind != agentproto.InitiatorInternalHelper {
		t.Fatalf("unexpected helper turn started event: %#v", started.Events[0])
	}

	delta, err := tr.ObserveServer([]byte(`{"method":"item/agentMessage/delta","params":{"threadId":"helper-thread","turnId":"turn-helper","itemId":"item-1","delta":"{\"title\":\"ok\"}"}}`))
	if err != nil {
		t.Fatalf("observe helper delta: %v", err)
	}
	if delta.Suppress {
		t.Fatalf("helper item delta must still reach parent stdout: %#v", delta)
	}
	if len(delta.Events) != 1 {
		t.Fatalf("expected annotated helper delta event, got %#v", delta.Events)
	}
	if delta.Events[0].Kind != agentproto.EventItemDelta ||
		delta.Events[0].TrafficClass != agentproto.TrafficClassInternalHelper ||
		delta.Events[0].Initiator.Kind != agentproto.InitiatorInternalHelper {
		t.Fatalf("unexpected helper delta event: %#v", delta.Events[0])
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"turn/completed","params":{"threadId":"helper-thread","turn":{"id":"turn-helper","status":"completed"}}}`))
	if err != nil {
		t.Fatalf("observe helper turn completed: %v", err)
	}
	if completed.Suppress {
		t.Fatalf("helper turn completed must still reach parent stdout: %#v", completed)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected annotated helper turn completed event, got %#v", completed.Events)
	}
	if completed.Events[0].Kind != agentproto.EventTurnCompleted ||
		completed.Events[0].TrafficClass != agentproto.TrafficClassInternalHelper ||
		completed.Events[0].Initiator.Kind != agentproto.InitiatorInternalHelper {
		t.Fatalf("unexpected helper turn completed event: %#v", completed.Events[0])
	}
}

func TestHelperTurnOnSameThreadDoesNotSuppressRemoteTurn(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"id":"helper-turn-1","method":"turn/start","params":{"threadId":"thread-1","cwd":"/tmp/project","outputSchema":{"type":"object","properties":{"title":{"type":"string"}}}}}`)); err != nil {
		t.Fatalf("observe helper turn start: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1", ChatID: "chat-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate remote command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one remote turn/start command, got %d", len(commands))
	}

	if _, err := tr.ObserveServer([]byte(`{"id":"helper-turn-1","result":{"turn":{"id":"turn-helper"}}}`)); err != nil {
		t.Fatalf("observe helper turn response: %v", err)
	}

	var remoteTurnStart map[string]any
	if err := json.Unmarshal(commands[0], &remoteTurnStart); err != nil {
		t.Fatalf("unmarshal remote turn/start: %v", err)
	}
	remoteRequestID, _ := remoteTurnStart["id"].(string)
	if remoteRequestID == "" {
		t.Fatalf("expected remote turn/start request id, got %#v", remoteTurnStart)
	}

	response, err := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turn":{"id":"turn-remote"}}}`, remoteRequestID)))
	if err != nil {
		t.Fatalf("observe remote turn response: %v", err)
	}
	if !response.Suppress {
		t.Fatalf("expected relay-owned remote turn/start response to stay suppressed, got %#v", response)
	}

	started, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-remote"}}}`))
	if err != nil {
		t.Fatalf("observe remote turn started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one remote turn started event, got %#v", started.Events)
	}
	if started.Events[0].Kind != agentproto.EventTurnStarted || started.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote turn started event, got %#v", started.Events[0])
	}

	item, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-remote","item":{"id":"item-1","type":"agentMessage","text":"您好"}}}`))
	if err != nil {
		t.Fatalf("observe remote item completed: %v", err)
	}
	if len(item.Events) != 1 || item.Events[0].ItemKind != "agent_message" {
		t.Fatalf("expected remote assistant item event, got %#v", item.Events)
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-remote","status":"completed"}}}`))
	if err != nil {
		t.Fatalf("observe remote turn completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one remote turn completed event, got %#v", completed.Events)
	}
	if completed.Events[0].Kind != agentproto.EventTurnCompleted || completed.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote turn completed event, got %#v", completed.Events[0])
	}
}

func TestInternalHelperThreadMarkerDoesNotPoisonLaterRemoteTurnOnSameThread(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe active thread resume: %v", err)
	}

	if _, err := tr.ObserveClient([]byte(`{"id":"helper-thread-1","method":"thread/start","params":{"cwd":"/tmp/project","approvalPolicy":"never","sandbox":"read-only","ephemeral":true,"persistExtendedHistory":false}}`)); err != nil {
		t.Fatalf("observe helper thread start: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"id":"helper-thread-1","result":{"thread":{"id":"thread-1"}}}`)); err != nil {
		t.Fatalf("observe helper thread response: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"thread/started","params":{"thread":{"id":"thread-1","cwd":"/tmp/project"}}}`)); err != nil {
		t.Fatalf("observe helper thread started: %v", err)
	}

	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1", ChatID: "chat-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("translate remote command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one remote turn/start command, got %d", len(commands))
	}

	started, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-remote"}}}`))
	if err != nil {
		t.Fatalf("observe remote turn started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one remote turn started event, got %#v", started.Events)
	}
	if started.Events[0].TrafficClass != agentproto.TrafficClassPrimary || started.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote primary turn started event, got %#v", started.Events[0])
	}

	item, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-remote","item":{"id":"item-1","type":"agentMessage","text":"您好"}}}`))
	if err != nil {
		t.Fatalf("observe remote item completed: %v", err)
	}
	if len(item.Events) != 1 {
		t.Fatalf("expected one remote item event, got %#v", item.Events)
	}
	if item.Events[0].TrafficClass != agentproto.TrafficClassPrimary || item.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote primary item event, got %#v", item.Events[0])
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-remote","status":"completed"}}}`))
	if err != nil {
		t.Fatalf("observe remote turn completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one remote turn completed event, got %#v", completed.Events)
	}
	if completed.Events[0].TrafficClass != agentproto.TrafficClassPrimary || completed.Events[0].Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote primary turn completed event, got %#v", completed.Events[0])
	}
}

func TestObserveServerItemLifecycleAndDelta(t *testing.T) {
	tr := NewTranslator("inst-1")

	started, err := tr.ObserveServer([]byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"agentMessage"}}}`))
	if err != nil {
		t.Fatalf("observe item started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one item started event, got %#v", started.Events)
	}
	if started.Events[0].Kind != agentproto.EventItemStarted || started.Events[0].ItemKind != "agent_message" {
		t.Fatalf("unexpected item started event: %#v", started.Events[0])
	}

	delta, err := tr.ObserveServer([]byte(`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"您好"}}`))
	if err != nil {
		t.Fatalf("observe item delta: %v", err)
	}
	if len(delta.Events) != 1 {
		t.Fatalf("expected one item delta event, got %#v", delta.Events)
	}
	if delta.Events[0].Kind != agentproto.EventItemDelta || delta.Events[0].Delta != "您好" {
		t.Fatalf("unexpected item delta event: %#v", delta.Events[0])
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"agentMessage"}}}`))
	if err != nil {
		t.Fatalf("observe item completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one item completed event, got %#v", completed.Events)
	}
	if completed.Events[0].Kind != agentproto.EventItemCompleted || completed.Events[0].ItemKind != "agent_message" {
		t.Fatalf("unexpected item completed event: %#v", completed.Events[0])
	}
}

func TestObserveServerCompletedLegacyAssistantMessageMapsToAgentMessage(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"assistant_message","text":"hello"}}}`))
	if err != nil {
		t.Fatalf("observe item completed: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	if result.Events[0].ItemKind != "agent_message" {
		t.Fatalf("expected normalized agent_message kind, got %#v", result.Events[0])
	}
	text, _ := result.Events[0].Metadata["text"].(string)
	if text != "hello" {
		t.Fatalf("expected completed text to be preserved, got %#v", result.Events[0].Metadata)
	}
}

func TestTranslateThreadsRefreshUsesThreadListAndBuildsSnapshot(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native command, got %d", len(commands))
	}

	var list map[string]any
	if err := json.Unmarshal(commands[0], &list); err != nil {
		t.Fatalf("unmarshal thread/list: %v", err)
	}
	if list["method"] != "thread/list" {
		t.Fatalf("expected thread/list refresh, got %#v", list)
	}
	params, _ := list["params"].(map[string]any)
	if params["sortKey"] != "created_at" {
		t.Fatalf("expected created_at sort key, got %#v", params)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"relay-threads-refresh-0","result":{"data":[{"id":"thread-2","preview":"整理日志"},{"id":"thread-1","name":"修复登录流程","preview":"修登录","state":"idle"}]}}`))
	if err != nil {
		t.Fatalf("observe thread/list response: %v", err)
	}
	if !refreshed.Suppress || len(refreshed.OutboundToCodex) != 2 {
		t.Fatalf("expected suppressed thread/read followups, got %#v", refreshed)
	}

	firstRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-1","result":{"thread":{"id":"thread-2","cwd":"/data/dl/droid","state":"running"}}}`))
	if err != nil {
		t.Fatalf("observe first thread/read: %v", err)
	}
	if !firstRead.Suppress || len(firstRead.Events) != 0 {
		t.Fatalf("expected intermediate thread/read to stay suppressed, got %#v", firstRead)
	}

	secondRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-2","result":{"thread":{"id":"thread-1","cwd":"/data/dl/droid","name":"修复登录流程","preview":"修登录"}}}`))
	if err != nil {
		t.Fatalf("observe second thread/read: %v", err)
	}
	if !secondRead.Suppress || len(secondRead.Events) != 1 {
		t.Fatalf("expected final snapshot event, got %#v", secondRead)
	}
	if secondRead.Events[0].Kind != agentproto.EventThreadsSnapshot || len(secondRead.Events[0].Threads) != 2 {
		t.Fatalf("unexpected snapshot payload: %#v", secondRead.Events[0])
	}
	if secondRead.Events[0].Threads[0].ThreadID != "thread-2" || secondRead.Events[0].Threads[0].CWD != "/data/dl/droid" {
		t.Fatalf("expected snapshot to preserve thread/list order, got %#v", secondRead.Events[0].Threads)
	}
	if secondRead.Events[0].Threads[1].ThreadID != "thread-1" || secondRead.Events[0].Threads[1].Name != "修复登录流程" {
		t.Fatalf("expected thread/read patch to populate title and preserve ordering, got %#v", secondRead.Events[0].Threads)
	}
	if secondRead.Events[0].Threads[0].ListOrder != 1 || secondRead.Events[0].Threads[1].ListOrder != 2 {
		t.Fatalf("expected snapshot records to retain list order metadata, got %#v", secondRead.Events[0].Threads)
	}
}

func TestObserveClientThreadNameSetResponseEmitsThreadDiscovered(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.ObserveClient([]byte(`{"id":"ThreadTitleBackfill:1","method":"thread/name/set","params":{"threadId":"thread-1","name":"修复登录流程"}}`)); err != nil {
		t.Fatalf("observe client thread/name/set: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"ThreadTitleBackfill:1","result":{"ok":true}}`))
	if err != nil {
		t.Fatalf("observe thread/name/set response: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadDiscovered {
		t.Fatalf("expected thread discovered update from successful name set, got %#v", result)
	}
	if result.Events[0].ThreadID != "thread-1" || result.Events[0].Name != "修复登录流程" {
		t.Fatalf("unexpected thread name update event: %#v", result.Events[0])
	}
}
