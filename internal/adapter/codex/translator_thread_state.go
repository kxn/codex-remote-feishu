package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) observeThreadLifecycle(message map[string]any, action agentproto.ThreadLifecycleAction) Result {
	threadID := lookupString(message, "params", "threadId")
	update := agentproto.NormalizeThreadLifecycleUpdate(&agentproto.ThreadLifecycleUpdate{
		ThreadID: threadID,
		Action:   action,
	})
	if update == nil {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:            agentproto.EventThreadLifecycleUpdated,
		ThreadID:        update.ThreadID,
		ThreadLifecycle: update,
	}}}
}

func (t *Translator) observeThreadGoalUpdated(message map[string]any) Result {
	params := lookupMap(message, "params")
	goal := lookupMap(params, "goal")
	if len(goal) == 0 {
		goal = params
	}
	update := agentproto.NormalizeThreadGoalUpdate(&agentproto.ThreadGoalUpdate{
		ThreadID:        lookupStringFromAny(params["threadId"]),
		TurnID:          lookupStringFromAny(params["turnId"]),
		Objective:       firstNonEmptyString(lookupStringFromAny(goal["objective"]), lookupStringFromAny(goal["goal"])),
		Status:          lookupStringFromAny(goal["status"]),
		TokenBudget:     lookupIntFromAny(goal["tokenBudget"]),
		TokensUsed:      lookupIntFromAny(goal["tokensUsed"]),
		TimeUsedSeconds: lookupIntFromAny(goal["timeUsedSeconds"]),
	})
	if update == nil {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:       agentproto.EventThreadGoalUpdated,
		ThreadID:   update.ThreadID,
		TurnID:     update.TurnID,
		ThreadGoal: update,
	}}}
}

func (t *Translator) observeThreadGoalCleared(message map[string]any) Result {
	update := agentproto.NormalizeThreadGoalUpdate(&agentproto.ThreadGoalUpdate{
		ThreadID: lookupString(message, "params", "threadId"),
		Cleared:  true,
	})
	if update == nil {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:       agentproto.EventThreadGoalUpdated,
		ThreadID:   update.ThreadID,
		ThreadGoal: update,
	}}}
}

func (t *Translator) observeThreadSettingsUpdated(message map[string]any) Result {
	params := lookupMap(message, "params")
	settings := lookupMap(params, "settings")
	if len(settings) == 0 {
		settings = params
	}
	update := agentproto.NormalizeThreadSettingsUpdate(&agentproto.ThreadSettingsUpdate{
		ThreadID:        lookupStringFromAny(params["threadId"]),
		Model:           firstNonEmptyString(lookupStringFromAny(settings["model"]), lookupStringFromAny(settings["modelId"])),
		ReasoningEffort: firstNonEmptyString(lookupStringFromAny(settings["reasoningEffort"]), lookupStringFromAny(settings["reasoning_effort"])),
		ApprovalPolicy:  lookupStringFromAny(settings["approvalPolicy"]),
		Sandbox:         firstNonEmptyString(lookupStringFromAny(settings["sandbox"]), lookupString(settings, "sandboxPolicy", "type")),
	})
	if update == nil {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:           agentproto.EventThreadSettingsUpdated,
		ThreadID:       update.ThreadID,
		ThreadSettings: update,
	}}}
}

func normalizeThreadLifecycleMethod(method string) agentproto.ThreadLifecycleAction {
	switch strings.TrimSpace(method) {
	case "thread/archived":
		return agentproto.ThreadLifecycleArchived
	case "thread/unarchived":
		return agentproto.ThreadLifecycleUnarchived
	case "thread/deleted":
		return agentproto.ThreadLifecycleDeleted
	case "thread/closed":
		return agentproto.ThreadLifecycleClosed
	default:
		return ""
	}
}
