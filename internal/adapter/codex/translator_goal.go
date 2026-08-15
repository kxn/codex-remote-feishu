package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (t *Translator) translateThreadGoalCommand(command agentproto.Command) ([][]byte, error) {
	threadID := strings.TrimSpace(command.Target.ThreadID)
	if threadID == "" {
		return nil, fmt.Errorf("%s requires thread id", command.Kind)
	}
	params := map[string]any{"threadId": threadID}
	switch command.Kind {
	case agentproto.CommandThreadGoalSet:
		if value := strings.TrimSpace(command.Goal.Status); value != "" {
			params["status"] = value
		}
		if value := strings.TrimSpace(command.Goal.Objective); value != "" {
			params["objective"] = value
		}
		if command.Goal.TokenBudget != nil {
			params["tokenBudget"] = *command.Goal.TokenBudget
		}
	}
	requestID := t.NextRequest("thread-goal")
	t.pendingGoalRequests[requestID] = pendingGoalRequest{
		CommandID: command.CommandID,
		ThreadID:  threadID,
		Operation: string(command.Kind),
		Purpose:   strings.TrimSpace(command.Goal.Purpose),
	}
	raw, err := json.Marshal(map[string]any{
		"id":     requestID,
		"method": threadGoalRPCMethod(command.Kind),
		"params": params,
	})
	if err != nil {
		return nil, err
	}
	return [][]byte{append(raw, '\n')}, nil
}

func threadGoalRPCMethod(kind agentproto.CommandKind) string {
	switch kind {
	case agentproto.CommandThreadGoalSet:
		return "thread/goal/set"
	case agentproto.CommandThreadGoalGet:
		return "thread/goal/get"
	case agentproto.CommandThreadGoalClear:
		return "thread/goal/clear"
	default:
		return ""
	}
}

func (t *Translator) observeThreadGoalCommandResponse(pending pendingGoalRequest, message map[string]any) Result {
	event := agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: pending.CommandID,
		ThreadID:  pending.ThreadID,
	}
	if errMsg := jsonrpcutil.ExtractErrorMessage(message); errMsg != "" {
		event.ErrorMessage = errMsg
		return Result{Events: []agentproto.Event{event}}
	}
	result := lookupMap(message, "result")
	switch pending.Operation {
	case string(agentproto.CommandThreadGoalClear):
		event.GoalCleared = xutil.LookupBoolFromAny(result["cleared"]) || len(result) == 0
		if event.GoalCleared {
			t.lastOwnedGoalMutations[pending.ThreadID] = ownedGoalMutation{Cleared: true}
		}
	case string(agentproto.CommandThreadGoalGet):
		if goal := lookupMap(result, "goal"); len(goal) > 0 {
			event.ThreadGoal = parseThreadGoal(pending.ThreadID, goal)
			if event.ThreadGoal == nil {
				event.GoalMissing = true
			}
		} else {
			event.GoalMissing = true
		}
	default:
		goal := lookupMap(result, "goal")
		if len(goal) == 0 {
			goal = result
		}
		event.ThreadGoal = parseThreadGoal(pending.ThreadID, goal)
		if event.ThreadGoal != nil {
			t.lastOwnedGoalMutations[pending.ThreadID] = ownedGoalMutation{UpdatedAt: event.ThreadGoal.UpdatedAt}
		}
	}
	return Result{Events: []agentproto.Event{event}}
}

func (t *Translator) ownsLastGoalMutation(threadID string, update *agentproto.ThreadGoalUpdate) bool {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false
	}
	owned, ok := t.lastOwnedGoalMutations[threadID]
	if !ok {
		return false
	}
	if update != nil && !update.Cleared && update.UpdatedAt != 0 && owned.UpdatedAt == update.UpdatedAt {
		delete(t.lastOwnedGoalMutations, threadID)
		return true
	}
	if update != nil && update.Cleared && owned.Cleared {
		delete(t.lastOwnedGoalMutations, threadID)
		return true
	}
	return false
}

func parseThreadGoal(threadID string, goal map[string]any) *agentproto.ThreadGoalUpdate {
	if threadID == "" || len(goal) == 0 {
		return nil
	}
	var budgetPtr *int64
	if raw, ok := goal["tokenBudget"]; ok && raw != nil {
		budget := lookupGoalInt64(raw)
		budgetPtr = &budget
	}
	return agentproto.NormalizeThreadGoalUpdate(&agentproto.ThreadGoalUpdate{
		ThreadID:        threadID,
		Objective:       xutil.LookupStringFromAny(goal["objective"]),
		Status:          xutil.LookupStringFromAny(goal["status"]),
		TokenBudget:     budgetPtr,
		TokensUsed:      lookupGoalInt64(goal["tokensUsed"]),
		TimeUsedSeconds: lookupGoalInt64(goal["timeUsedSeconds"]),
		CreatedAt:       lookupGoalInt64(goal["createdAt"]),
		UpdatedAt:       lookupGoalInt64(goal["updatedAt"]),
	})
}

func lookupGoalInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func (t *Translator) pendingGoalMutationForThread(threadID string) bool {
	threadID = strings.TrimSpace(threadID)
	for _, pending := range t.pendingGoalRequests {
		if pending.ThreadID != threadID {
			continue
		}
		switch pending.Operation {
		case string(agentproto.CommandThreadGoalSet), string(agentproto.CommandThreadGoalClear):
			return true
		}
	}
	return false
}

func (t *Translator) translateThreadReadCommand(command agentproto.Command) ([][]byte, error) {
	threadID := strings.TrimSpace(command.Target.ThreadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread.read requires thread id")
	}
	requestID := t.NextRequest("thread-read")
	t.pendingThreadReads[requestID] = pendingThreadRead{
		CommandID: command.CommandID,
		ThreadID:  threadID,
	}
	raw, err := json.Marshal(map[string]any{
		"id":     requestID,
		"method": "thread/read",
		"params": map[string]any{
			"threadId":     threadID,
			"includeTurns": false,
		},
	})
	if err != nil {
		return nil, err
	}
	return [][]byte{append(raw, '\n')}, nil
}

func (t *Translator) observeThreadReadResponse(pending pendingThreadRead, message map[string]any) Result {
	event := agentproto.Event{
		Kind:      agentproto.EventThreadRuntimeStatusUpdated,
		CommandID: pending.CommandID,
		ThreadID:  pending.ThreadID,
	}
	if errMsg := jsonrpcutil.ExtractErrorMessage(message); errMsg != "" {
		event.ErrorMessage = errMsg
		return Result{Events: []agentproto.Event{event}}
	}
	record := parseThreadRecord(message["result"])
	event.RuntimeStatus = agentproto.CloneThreadRuntimeStatus(record.RuntimeStatus)
	return Result{Events: []agentproto.Event{event}}
}
