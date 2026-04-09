package codex

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func configObservedEvents(threadID, cwd string, params map[string]any, treatAsDefault bool) []agentproto.Event {
	model, effort, access := extractObservedConfig(params)
	if model == "" && effort == "" && access == "" {
		return nil
	}
	scope := "thread"
	if treatAsDefault || threadID == "" {
		scope = "cwd_default"
	}
	return []agentproto.Event{{
		Kind:            agentproto.EventConfigObserved,
		ThreadID:        threadID,
		CWD:             cwd,
		Model:           model,
		ReasoningEffort: effort,
		AccessMode:      access,
		ConfigScope:     scope,
	}}
}

func extractObservedConfig(params map[string]any) (model, effort, access string) {
	model = choose(
		lookupString(params, "collaborationMode", "settings", "model"),
		lookupStringFromAny(params["model"]),
		lookupString(params, "config", "model"),
	)
	effort = choose(
		lookupString(params, "collaborationMode", "settings", "reasoning_effort"),
		lookupString(params, "config", "model_reasoning_effort"),
		lookupString(params, "config", "reasoning_effort"),
		lookupStringFromAny(params["effort"]),
	)
	access = chooseObservedAccessMode(
		lookupStringFromAny(params["approvalPolicy"]),
		lookupStringFromAny(params["sandbox"]),
		lookupString(params, "sandboxPolicy", "type"),
		lookupString(params, "config", "approval_policy"),
		lookupString(params, "config", "sandbox"),
	)
	return model, effort, access
}

func chooseObservedAccessMode(values ...string) string {
	for _, value := range values {
		if normalized := agentproto.NormalizeAccessMode(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func applyPromptOverridesToThreadStart(params map[string]any, overrides agentproto.PromptOverrides) {
	if overrides.Model != "" {
		params["model"] = overrides.Model
	}
	if overrides.ReasoningEffort != "" {
		configMap := lookupMapFromAny(params["config"])
		configMap["model_reasoning_effort"] = overrides.ReasoningEffort
		configMap["reasoning_effort"] = overrides.ReasoningEffort
		params["config"] = configMap
	}
	params["approvalPolicy"] = agentproto.ApprovalPolicyForAccessMode(overrides.AccessMode)
	params["sandbox"] = agentproto.ThreadSandboxForAccessMode(overrides.AccessMode)
}

func applyPromptOverridesToTurnStart(template map[string]any, overrides agentproto.PromptOverrides) {
	if overrides.Model != "" {
		template["model"] = overrides.Model
	}
	if overrides.ReasoningEffort != "" {
		template["effort"] = overrides.ReasoningEffort
	}
	collaborationMode := lookupMapFromAny(template["collaborationMode"])
	settings := lookupMapFromAny(collaborationMode["settings"])
	if overrides.Model != "" || lookupStringFromAny(settings["model"]) != "" {
		if len(collaborationMode) == 0 {
			collaborationMode = map[string]any{"mode": "custom"}
		}
		if overrides.Model != "" {
			settings["model"] = overrides.Model
		}
		if overrides.ReasoningEffort != "" {
			settings["reasoning_effort"] = overrides.ReasoningEffort
		}
		collaborationMode["settings"] = settings
		template["collaborationMode"] = collaborationMode
	}
	template["approvalPolicy"] = agentproto.ApprovalPolicyForAccessMode(overrides.AccessMode)
	template["sandboxPolicy"] = agentproto.TurnSandboxPolicyForAccessMode(overrides.AccessMode)
}

func normalizeThreadStartParams(params map[string]any) map[string]any {
	normalized := cloneMap(params)
	delete(normalized, "ephemeral")
	delete(normalized, "persistExtendedHistory")
	setDefault(normalized, "cwd", nil)
	setDefault(normalized, "model", nil)
	setDefault(normalized, "modelProvider", nil)
	setDefault(normalized, "config", map[string]any{})
	setDefault(normalized, "approvalPolicy", "on-request")
	setDefault(normalized, "baseInstructions", nil)
	setDefault(normalized, "developerInstructions", nil)
	setDefault(normalized, "sandbox", "read-only")
	setDefault(normalized, "personality", nil)
	setDefault(normalized, "experimentalRawEvents", false)
	setDefault(normalized, "dynamicTools", nil)
	return normalized
}

func normalizeTurnStartTemplate(params map[string]any) map[string]any {
	normalized := map[string]any{}
	for _, key := range []string{
		"cwd",
		"approvalPolicy",
		"sandboxPolicy",
		"model",
		"effort",
		"summary",
		"personality",
		"collaborationMode",
		"attachments",
	} {
		if value, ok := params[key]; ok {
			normalized[key] = value
		}
	}
	setDefault(normalized, "summary", "auto")
	setDefault(normalized, "attachments", []any{})
	return normalized
}

func isInternalLocalThreadStart(params map[string]any) bool {
	if lookupBoolFromAny(params["ephemeral"]) {
		return true
	}
	value, ok := params["persistExtendedHistory"].(bool)
	return ok && !value
}

func isInternalLocalTurnStart(params map[string]any) bool {
	return !isNull(params["outputSchema"])
}
