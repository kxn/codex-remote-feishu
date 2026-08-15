package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func int64PtrValue(value int64) *int64 {
	return &value
}

func decodeGoalRequest(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var request map[string]any
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode goal request: %v", err)
	}
	return request
}

func TestTranslateCommandThreadGoalSetGetClear(t *testing.T) {
	tr := NewTranslator("inst-1")

	setPayload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-set",
		Kind:      agentproto.CommandThreadGoalSet,
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Goal: agentproto.GoalCommand{
			Status:      "paused",
			Objective:   "ship it",
			TokenBudget: int64PtrValue(1200),
			Purpose:     "queue_interlock",
		},
	})
	if err != nil {
		t.Fatalf("translate goal set: %v", err)
	}
	setRequest := decodeGoalRequest(t, setPayload[0])
	if setRequest["method"] != "thread/goal/set" {
		t.Fatalf("unexpected set method: %#v", setRequest)
	}
	params := setRequest["params"].(map[string]any)
	if params["threadId"] != "thread-1" || params["status"] != "paused" || params["objective"] != "ship it" || params["tokenBudget"] != float64(1200) {
		t.Fatalf("unexpected set params: %#v", params)
	}
	if _, exists := params["purpose"]; exists {
		t.Fatalf("purpose must stay daemon-internal, leaked into upstream params: %#v", params)
	}

	getPayload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-get",
		Kind:      agentproto.CommandThreadGoalGet,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate goal get: %v", err)
	}
	getRequest := decodeGoalRequest(t, getPayload[0])
	if getRequest["method"] != "thread/goal/get" || getRequest["params"].(map[string]any)["threadId"] != "thread-1" {
		t.Fatalf("unexpected get request: %#v", getRequest)
	}

	clearPayload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-clear",
		Kind:      agentproto.CommandThreadGoalClear,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate goal clear: %v", err)
	}
	clearRequest := decodeGoalRequest(t, clearPayload[0])
	if clearRequest["method"] != "thread/goal/clear" || clearRequest["params"].(map[string]any)["threadId"] != "thread-1" {
		t.Fatalf("unexpected clear request: %#v", clearRequest)
	}
}

func TestObserveServerGoalSetResponseProducesCorrelatedResult(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-set",
		Kind:      agentproto.CommandThreadGoalSet,
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Goal: agentproto.GoalCommand{
			Status:  "paused",
			Purpose: "queue_interlock",
		},
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	requestID := decodeGoalRequest(t, payload[0])["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","result":{"goal":{"threadId":"thread-1","objective":"ship it","status":"paused","tokenBudget":1200,"tokensUsed":345,"timeUsedSeconds":67,"createdAt":1710000000123,"updatedAt":1710000000999}}}`))
	if err != nil {
		t.Fatalf("observe set response: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one result event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventThreadGoalCommandResult || event.CommandID != "cmd-goal-set" || event.ThreadID != "thread-1" {
		t.Fatalf("unexpected result event: %#v", event)
	}
	if event.ThreadGoal == nil || event.ThreadGoal.Status != "paused" || event.ThreadGoal.CreatedAt != 1710000000123 || event.ThreadGoal.UpdatedAt != 1710000000999 {
		t.Fatalf("goal snapshot lost timestamps: %#v", event.ThreadGoal)
	}
	if event.ThreadGoal.TokenBudget == nil || *event.ThreadGoal.TokenBudget != 1200 {
		t.Fatalf("goal snapshot lost token budget: %#v", event.ThreadGoal)
	}
}

func TestObserveServerGoalGetEmptyProducesGoalMissing(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-get",
		Kind:      agentproto.CommandThreadGoalGet,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	requestID := decodeGoalRequest(t, payload[0])["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","result":{"goal":null}}`))
	if err != nil {
		t.Fatalf("observe get response: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadGoalCommandResult ||
		result.Events[0].CommandID != "cmd-goal-get" || !result.Events[0].GoalMissing {
		t.Fatalf("expected goal missing result, got %#v", result.Events)
	}
}

func TestObserveServerGoalClearResponseProducesGoalCleared(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-clear",
		Kind:      agentproto.CommandThreadGoalClear,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	requestID := decodeGoalRequest(t, payload[0])["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","result":{"cleared":true}}`))
	if err != nil {
		t.Fatalf("observe clear response: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadGoalCommandResult ||
		result.Events[0].CommandID != "cmd-goal-clear" || !result.Events[0].GoalCleared {
		t.Fatalf("expected goal cleared result, got %#v", result.Events)
	}
}

func TestObserveServerGoalResponseErrorProducesErrorResult(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-get",
		Kind:      agentproto.CommandThreadGoalGet,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	requestID := decodeGoalRequest(t, payload[0])["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","error":{"code":-32000,"message":"goal unavailable"}}`))
	if err != nil {
		t.Fatalf("observe error response: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadGoalCommandResult ||
		result.Events[0].CommandID != "cmd-goal-get" || result.Events[0].ErrorMessage == "" {
		t.Fatalf("expected goal error result, got %#v", result.Events)
	}
}

func TestObserveServerGoalNotificationMarksExternalMutationWithoutPendingCommand(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"thread/goal/updated","params":{"threadId":"thread-1","goal":{"objective":"external","status":"paused","createdAt":1710000000123,"updatedAt":1710000000999}}}`))
	if err != nil {
		t.Fatalf("observe notification: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].ThreadGoal == nil || !result.Events[0].ThreadGoal.ExternalMutation {
		t.Fatalf("expected external mutation flag, got %#v", result.Events)
	}
	if result.Events[0].ThreadGoal.CreatedAt != 1710000000123 || result.Events[0].ThreadGoal.UpdatedAt != 1710000000999 {
		t.Fatalf("notification lost timestamps: %#v", result.Events[0].ThreadGoal)
	}
}

func TestObserveServerGoalNotificationDuringPendingSetIsNotExternal(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-goal-set",
		Kind:      agentproto.CommandThreadGoalSet,
		Target:    agentproto.Target{ThreadID: "thread-1"},
		Goal: agentproto.GoalCommand{
			Status:  "paused",
			Purpose: "queue_interlock",
		},
	})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	_ = decodeGoalRequest(t, payload[0])["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"method":"thread/goal/updated","params":{"threadId":"thread-1","goal":{"objective":"mine","status":"paused"}}}`))
	if err != nil {
		t.Fatalf("observe notification: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].ThreadGoal == nil || result.Events[0].ThreadGoal.ExternalMutation {
		t.Fatalf("notification during pending goal set must not be external, got %#v", result.Events)
	}
}

func TestTranslateThreadReadCommandAndObserveRuntimeStatus(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-thread-read",
		Kind:      agentproto.CommandThreadRead,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate thread read: %v", err)
	}
	request := decodeGoalRequest(t, payload[0])
	if request["method"] != "thread/read" {
		t.Fatalf("unexpected thread read method: %#v", request)
	}
	params := request["params"].(map[string]any)
	if params["threadId"] != "thread-1" || params["includeTurns"] != false {
		t.Fatalf("unexpected thread read params: %#v", params)
	}
	requestID := request["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","result":{"thread":{"id":"thread-1","status":"idle"}}}`))
	if err != nil {
		t.Fatalf("observe thread read response: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one runtime status event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventThreadRuntimeStatusUpdated || event.CommandID != "cmd-thread-read" ||
		event.ThreadID != "thread-1" || event.RuntimeStatus == nil ||
		event.RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeIdle {
		t.Fatalf("unexpected runtime status event: %#v", event)
	}
}

func TestObserveThreadReadResponseError(t *testing.T) {
	tr := NewTranslator("inst-1")
	payload, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-thread-read",
		Kind:      agentproto.CommandThreadRead,
		Target:    agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate thread read: %v", err)
	}
	requestID := decodeGoalRequest(t, payload[0])["id"].(string)

	result, err := tr.ObserveServer([]byte(`{"id":"` + requestID + `","error":{"code":-32000,"message":"read failed"}}`))
	if err != nil {
		t.Fatalf("observe thread read error: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadRuntimeStatusUpdated ||
		result.Events[0].CommandID != "cmd-thread-read" || result.Events[0].ErrorMessage == "" {
		t.Fatalf("expected thread read error event, got %#v", result.Events)
	}
}
