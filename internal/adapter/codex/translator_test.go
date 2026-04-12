package codex

import (
	"encoding/json"
	"fmt"
	"strings"
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

func TestTranslateTurnSteer(t *testing.T) {
	tr := NewTranslator("inst-1")
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandTurnSteer,
		Target: agentproto.Target{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{
				{Type: agentproto.InputText, Text: "补充信息"},
				{Type: agentproto.InputLocalImage, Path: "/tmp/queued.png", MIMEType: "image/png"},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one steer command, got %#v", commands)
	}
	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal turn/steer: %v", err)
	}
	if payload["method"] != "turn/steer" {
		t.Fatalf("expected turn/steer payload, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params["threadId"] != "thread-1" || params["expectedTurnId"] != "turn-1" {
		t.Fatalf("unexpected steer params: %#v", params)
	}
	if _, exists := params["turnId"]; exists {
		t.Fatalf("unexpected legacy turnId in steer params: %#v", params)
	}
	inputs, _ := params["input"].([]any)
	if len(inputs) != 2 {
		t.Fatalf("expected text + image steer inputs, got %#v", params["input"])
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

func TestObserveThreadTokenUsageUpdated(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"thread/tokenUsage/updated","params":{"threadId":"thread-1","turnId":"turn-1","tokenUsage":{"last":{"totalTokens":200,"inputTokens":150,"cachedInputTokens":90,"outputTokens":50,"reasoningOutputTokens":20},"total":{"totalTokens":500,"inputTokens":400,"cachedInputTokens":200,"outputTokens":100,"reasoningOutputTokens":40},"modelContextWindow":1000}}}`))
	if err != nil {
		t.Fatalf("observe server: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one usage event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventThreadTokenUsageUpdated {
		t.Fatalf("unexpected event kind: %#v", event)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("unexpected usage target: %#v", event)
	}
	if event.TokenUsage == nil {
		t.Fatalf("expected token usage payload, got %#v", event)
	}
	if event.TokenUsage.Last.CachedInputTokens != 90 || event.TokenUsage.Total.TotalTokens != 500 {
		t.Fatalf("unexpected token usage payload: %#v", event.TokenUsage)
	}
	if event.TokenUsage.ModelContextWindow == nil || *event.TokenUsage.ModelContextWindow != 1000 {
		t.Fatalf("unexpected model context window: %#v", event.TokenUsage)
	}
}

func TestObserveTurnPlanUpdated(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"turn/plan/updated","params":{"threadId":"thread-1","turnId":"turn-1","explanation":"先完成协议接入。","plan":[{"step":"接入结构化 plan 协议","status":"completed"},{"step":"做 orchestrator 去重","status":"inProgress"},{"step":"接入飞书卡片投影","status":"pending"}]}}`))
	if err != nil {
		t.Fatalf("observe server: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one plan update event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventTurnPlanUpdated {
		t.Fatalf("unexpected event kind: %#v", event)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("unexpected event target: %#v", event)
	}
	if event.PlanSnapshot == nil {
		t.Fatalf("expected plan snapshot payload, got %#v", event)
	}
	if event.PlanSnapshot.Explanation != "先完成协议接入。" {
		t.Fatalf("unexpected explanation: %#v", event.PlanSnapshot)
	}
	if len(event.PlanSnapshot.Steps) != 3 {
		t.Fatalf("unexpected plan steps: %#v", event.PlanSnapshot)
	}
	if event.PlanSnapshot.Steps[0].Status != agentproto.TurnPlanStepStatusCompleted {
		t.Fatalf("unexpected first status: %#v", event.PlanSnapshot.Steps)
	}
	if event.PlanSnapshot.Steps[1].Status != agentproto.TurnPlanStepStatusInProgress {
		t.Fatalf("unexpected second status: %#v", event.PlanSnapshot.Steps)
	}
	if event.PlanSnapshot.Steps[2].Status != agentproto.TurnPlanStepStatusPending {
		t.Fatalf("unexpected third status: %#v", event.PlanSnapshot.Steps)
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

func TestTranslatePromptSendReasoningOnlyDoesNotCreateInvalidCollaborationMode(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{ChatID: "surface-1"},
		Target:    agentproto.Target{ThreadID: "thread-2", CWD: "/tmp/two"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
		Overrides: agentproto.PromptOverrides{ReasoningEffort: "xhigh"},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
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
	params, _ := turnStart["params"].(map[string]any)
	if params["effort"] != "xhigh" {
		t.Fatalf("expected top-level effort override, got %#v", params)
	}
	if value, exists := params["collaborationMode"]; exists && value != nil {
		t.Fatalf("expected reasoning-only override to avoid custom collaborationMode, got %#v", params)
	}
}

func TestObserveServerThreadResumeErrorEmitsFailedTurnCompleted(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-2", CWD: "/tmp/two"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-resume-0","error":{"message":"resume failed"}}`))
	if err != nil {
		t.Fatalf("observe resume error: %v", err)
	}
	if len(result.OutboundToCodex) != 0 || len(result.Events) != 1 {
		t.Fatalf("expected failed event without followup, got %#v", result)
	}
	if result.Events[0].Kind != agentproto.EventTurnCompleted || result.Events[0].Status != "failed" || result.Events[0].ThreadID != "thread-2" {
		t.Fatalf("unexpected failed event: %#v", result.Events[0])
	}
	if !strings.Contains(result.Events[0].ErrorMessage, "resume failed") {
		t.Fatalf("expected resume error message, got %#v", result.Events[0])
	}
}

func TestObserveServerSuppressedTurnStartErrorEmitsFailedTurnCompleted(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{ChatID: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-2", CWD: "/tmp/two"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"relay-thread-resume-0","result":{}}`))
	if err != nil {
		t.Fatalf("observe resume response: %v", err)
	}
	if len(result.OutboundToCodex) != 1 {
		t.Fatalf("expected followup turn/start, got %#v", result)
	}

	var turnStart map[string]any
	if err := json.Unmarshal(result.OutboundToCodex[0], &turnStart); err != nil {
		t.Fatalf("unmarshal turn/start: %v", err)
	}
	requestID, _ := turnStart["id"].(string)
	if requestID == "" {
		t.Fatalf("expected followup request id, got %#v", turnStart)
	}

	failed, err := tr.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"error":{"message":"Invalid request: missing field 'model'"}}`, requestID)))
	if err != nil {
		t.Fatalf("observe turn/start error: %v", err)
	}
	if len(failed.OutboundToCodex) != 0 || len(failed.Events) != 1 {
		t.Fatalf("expected failed event without suppression, got %#v", failed)
	}
	if failed.Events[0].Kind != agentproto.EventTurnCompleted || failed.Events[0].Status != "failed" || failed.Events[0].ThreadID != "thread-2" {
		t.Fatalf("unexpected failed event: %#v", failed.Events[0])
	}
	if !strings.Contains(failed.Events[0].ErrorMessage, "missing field 'model'") {
		t.Fatalf("expected turn/start error message, got %#v", failed.Events[0])
	}
}

func TestTranslatorDebugLogsRemoteResumeFollowup(t *testing.T) {
	tr := NewTranslator("inst-1")
	var debugLogs []string
	tr.SetDebugLogger(func(format string, args ...any) {
		debugLogs = append(debugLogs, fmt.Sprintf(format, args...))
	})

	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/one"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:chat:1"},
		Target:    agentproto.Target{ThreadID: "thread-2", CWD: "/tmp/two"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"id":"relay-thread-resume-0","result":{}}`)); err != nil {
		t.Fatalf("observe resume response: %v", err)
	}

	joined := strings.Join(debugLogs, "\n")
	if !strings.Contains(joined, "translate remote prompt: command=cmd-1 action=thread/resume request=relay-thread-resume-0") {
		t.Fatalf("expected thread/resume debug log, got %s", joined)
	}
	if !strings.Contains(joined, "observe server thread/resume result: request=relay-thread-resume-0 thread=thread-2 followup=relay-turn-start-1") {
		t.Fatalf("expected followup debug log, got %s", joined)
	}
}
