package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
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
	threadID := xutil.LookupStringFromAny(params["threadId"])
	update := parseThreadGoal(threadID, goal)
	if update == nil {
		return Result{}
	}
	update.TurnID = xutil.LookupStringFromAny(params["turnId"])
	update.ExternalMutation = !t.pendingGoalMutationForThread(threadID) && !t.ownsLastGoalMutation(threadID, update)
	return Result{Events: []agentproto.Event{{
		Kind:       agentproto.EventThreadGoalUpdated,
		ThreadID:   update.ThreadID,
		TurnID:     update.TurnID,
		ThreadGoal: update,
	}}}
}

func (t *Translator) observeThreadGoalCleared(message map[string]any) Result {
	threadID := lookupString(message, "params", "threadId")
	update := agentproto.NormalizeThreadGoalUpdate(&agentproto.ThreadGoalUpdate{
		ThreadID:         threadID,
		Cleared:          true,
		ExternalMutation: !t.pendingGoalMutationForThread(threadID) && !t.ownsLastGoalMutation(threadID, &agentproto.ThreadGoalUpdate{ThreadID: threadID, Cleared: true}),
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
		ThreadID:        xutil.LookupStringFromAny(params["threadId"]),
		ModelProviderID: xutil.FirstNonEmpty(xutil.LookupStringFromAny(settings["modelProvider"]), xutil.LookupStringFromAny(settings["modelProviderId"])),
		Model:           xutil.FirstNonEmpty(xutil.LookupStringFromAny(settings["model"]), xutil.LookupStringFromAny(settings["modelId"])),
		ReasoningEffort: xutil.FirstNonEmpty(xutil.LookupStringFromAny(settings["reasoningEffort"]), xutil.LookupStringFromAny(settings["reasoning_effort"])),
		ApprovalPolicy:  xutil.LookupStringFromAny(settings["approvalPolicy"]),
		Sandbox:         xutil.FirstNonEmpty(xutil.LookupStringFromAny(settings["sandbox"]), lookupString(settings, "sandboxPolicy", "type")),
	})
	if update == nil {
		return Result{}
	}
	t.mergeObservedThread(update.ThreadID, update.ModelProviderID, update.Model, update.ReasoningEffort)
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
