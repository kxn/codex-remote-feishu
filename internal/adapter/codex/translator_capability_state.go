package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) observeCapabilityState(method string, message map[string]any) Result {
	update := extractCapabilityStateUpdate(method, message)
	if update == nil {
		return Result{}
	}
	return Result{Events: []agentproto.Event{{
		Kind:            agentproto.EventCapabilityStateUpdated,
		ThreadID:        update.ThreadID,
		CapabilityState: update,
	}}}
}

func extractCapabilityStateUpdate(method string, message map[string]any) *agentproto.CapabilityStateUpdate {
	params := lookupMap(message, "params")
	update := &agentproto.CapabilityStateUpdate{
		Method:   strings.TrimSpace(method),
		ThreadID: lookupStringFromAny(params["threadId"]),
	}
	switch method {
	case "skills/changed":
		update.SkillsChanged = true
	case "mcpServer/startupStatus/updated":
		update.MCPServerStartupStatus = &agentproto.MCPServerStartupStatus{
			ThreadID:      lookupStringFromAny(params["threadId"]),
			Name:          lookupStringFromAny(params["name"]),
			Status:        lookupStringFromAny(params["status"]),
			Error:         lookupStringFromAny(params["error"]),
			FailureReason: lookupStringFromAny(params["failureReason"]),
		}
	case "mcpServer/oauthLogin/completed":
		update.MCPOAuthLoginCompleted = &agentproto.MCPOAuthLoginCompletionState{
			Name:     lookupStringFromAny(params["name"]),
			ThreadID: lookupStringFromAny(params["threadId"]),
			Success:  lookupBoolFromAny(params["success"]),
			Error:    lookupStringFromAny(params["error"]),
		}
	case "app/list/updated":
		update.Apps = extractAppStateRecords(params)
	case "account/updated":
		update.Account = &agentproto.AccountState{
			AuthMode: lookupStringFromAny(params["authMode"]),
			PlanType: lookupStringFromAny(params["planType"]),
		}
	case "account/rateLimits/updated":
		update.RateLimits = extractSparseRateLimits(params["rateLimits"])
	case "account/login/completed", "accountLoginCompleted":
		update.AccountLoginCompleted = &agentproto.AccountLoginCompletionState{
			LoginID: lookupStringFromAny(params["loginId"]),
			Success: lookupBoolFromAny(params["success"]),
			Error:   lookupStringFromAny(params["error"]),
		}
	}
	return agentproto.NormalizeCapabilityStateUpdate(update)
}

func extractAppStateRecords(params map[string]any) []agentproto.AppStateRecord {
	source := firstNonNil(params["data"], params["apps"])
	rawApps := sliceAnyFromAny(source)
	if len(rawApps) == 0 {
		return nil
	}
	apps := make([]agentproto.AppStateRecord, 0, len(rawApps))
	for _, raw := range rawApps {
		record, _ := raw.(map[string]any)
		if record == nil {
			continue
		}
		apps = append(apps, agentproto.AppStateRecord{
			ID:          firstNonEmptyString(lookupStringFromAny(record["id"]), lookupStringFromAny(record["appId"])),
			Name:        firstNonEmptyString(lookupStringFromAny(record["name"]), lookupStringFromAny(record["title"])),
			Description: lookupStringFromAny(record["description"]),
		})
	}
	return apps
}

func extractSparseRateLimits(raw any) map[string]map[string]any {
	source, _ := raw.(map[string]any)
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]map[string]any, len(source))
	for name, value := range source {
		record, _ := value.(map[string]any)
		if strings.TrimSpace(name) == "" || len(record) == 0 {
			continue
		}
		result[name] = cloneMap(record)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
