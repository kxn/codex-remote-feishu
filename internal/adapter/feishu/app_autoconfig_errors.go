package feishu

import (
	"errors"
	"net/http"

	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

const autoConfigAppManagementScope = "application:application:self_manage"

func overridePlanFromAPIError(plan AutoConfigPlan, err error) AutoConfigPlan {
	updated := plan
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr == nil {
		updated.Status = AutoConfigStatusBlocked
		updated.Summary, updated.BlockingReason = autoConfigReadFailureSummary()
		return updated
	}
	updated.Status = AutoConfigStatusBlocked
	updated.Summary, updated.BlockingReason = autoConfigReadAPIErrorSummary(apiErr)
	return updated
}

func autoConfigReadPermissionError(apiErr *APIError) bool {
	if apiErr == nil || apiErr.StatusCode == http.StatusUnauthorized {
		return false
	}
	if apiErr.StatusCode == http.StatusForbidden || len(apiErr.PermissionViolations) > 0 {
		return true
	}
	_, ok := ExtractPermissionGap(apiErr)
	return ok
}

func autoConfigReadPermissionPlan(plan AutoConfigPlan, scopeReqs []feishuapp.ScopeRequirement) AutoConfigPlan {
	requirement := autoConfigAppManagementScopeRequirement(scopeReqs)
	scopeType := normalizeTokenType(requirement.ScopeType)
	if scopeType == "" {
		scopeType = "tenant"
	}
	updated := plan
	updated.BlockingReason = ""
	updated.Diff = AutoConfigDiff{
		ConfigPatchRequired: true,
		MissingScopes: []AutoConfigScopeRef{{
			Scope:     requirement.Scope,
			ScopeType: scopeType,
		}},
		PublishRequired: true,
	}
	updated.Publish = AutoConfigPublishState{NeedsPublish: true}
	updated.BlockingRequirements = []AutoConfigRequirementStatus{{
		Kind:           AutoConfigRequirementKindScope,
		Key:            requirement.Scope,
		ScopeType:      scopeType,
		Feature:        requirement.Feature,
		Required:       true,
		DegradeMessage: requirement.DegradeMessage,
		Present:        false,
	}}
	updated.DegradableRequirements = nil
	updated.Status, updated.Summary = derivePlanState(updated)
	return updated
}

func autoConfigAppManagementScopeRequirement(scopeReqs []feishuapp.ScopeRequirement) feishuapp.ScopeRequirement {
	for _, item := range scopeReqs {
		if item.Scope == autoConfigAppManagementScope {
			return item
		}
	}
	return feishuapp.ScopeRequirement{
		Scope:     autoConfigAppManagementScope,
		ScopeType: "tenant",
		Required:  true,
	}
}

func autoConfigReadAPIErrorSummary(apiErr *APIError) (string, string) {
	if apiErr == nil {
		return autoConfigReadFailureSummary()
	}
	if apiErr.StatusCode == http.StatusUnauthorized {
		return "当前飞书应用凭证已经失效，请重新连接飞书机器人。", autoConfigBlockingCredentialIssue
	}
	if apiErr.StatusCode == http.StatusForbidden || len(apiErr.PermissionViolations) > 0 {
		return "当前凭证没有修改飞书应用配置的权限，请使用有权限的管理员账号处理。", autoConfigBlockingPermissionIssue
	}
	if _, ok := ExtractPermissionGap(apiErr); ok {
		return "当前凭证缺少修改飞书应用配置所需的权限，请在飞书后台处理。", autoConfigBlockingPermissionIssue
	}
	return autoConfigReadFailureSummary()
}

func autoConfigReadFailureSummary() (string, string) {
	return "暂时无法读取飞书应用配置，请稍后重新检查。", autoConfigBlockingReadFailed
}
