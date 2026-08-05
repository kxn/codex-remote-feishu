package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
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
		ThreadID: xutil.LookupStringFromAny(params["threadId"]),
	}
	switch method {
	case "skills/changed":
		update.SkillsChanged = true
	case "mcpServer/startupStatus/updated":
		update.MCPServerStartupStatus = &agentproto.MCPServerStartupStatus{
			ThreadID:      xutil.LookupStringFromAny(params["threadId"]),
			Name:          xutil.LookupStringFromAny(params["name"]),
			Status:        xutil.LookupStringFromAny(params["status"]),
			Error:         xutil.LookupStringFromAny(params["error"]),
			FailureReason: xutil.LookupStringFromAny(params["failureReason"]),
		}
	case "mcpServer/oauthLogin/completed":
		update.MCPOAuthLoginCompleted = &agentproto.MCPOAuthLoginCompletionState{
			Name:     xutil.LookupStringFromAny(params["name"]),
			ThreadID: xutil.LookupStringFromAny(params["threadId"]),
			Success:  xutil.LookupBoolFromAny(params["success"]),
			Error:    xutil.LookupStringFromAny(params["error"]),
		}
	case "app/list/updated":
		update.Apps = extractAppStateRecords(params)
	case "account/updated":
		update.Account = &agentproto.AccountState{
			AuthMode: xutil.LookupStringFromAny(params["authMode"]),
			PlanType: xutil.LookupStringFromAny(params["planType"]),
		}
	case "account/rateLimits/updated":
		update.RateLimits = extractSparseRateLimits(params["rateLimits"])
	case "account/login/completed", "accountLoginCompleted":
		update.AccountLoginCompleted = &agentproto.AccountLoginCompletionState{
			LoginID: xutil.LookupStringFromAny(params["loginId"]),
			Success: xutil.LookupBoolFromAny(params["success"]),
			Error:   xutil.LookupStringFromAny(params["error"]),
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
			ID:          xutil.FirstNonEmpty(xutil.LookupStringFromAny(record["id"]), xutil.LookupStringFromAny(record["appId"])),
			Name:        xutil.FirstNonEmpty(xutil.LookupStringFromAny(record["name"]), xutil.LookupStringFromAny(record["title"])),
			Description: xutil.LookupStringFromAny(record["description"]),
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
		result[name] = xutil.CloneMap(record)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
