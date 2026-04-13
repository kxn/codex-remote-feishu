package feishu

import (
	"context"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type AppScopeStatus struct {
	ScopeName   string
	ScopeType   string
	GrantStatus int
}

func ListAppScopes(ctx context.Context, cfg LiveGatewayConfig) ([]AppScopeStatus, error) {
	client := lark.NewClient(strings.TrimSpace(cfg.AppID), strings.TrimSpace(cfg.AppSecret))
	resp, err := client.Application.V6.Scope.List(ctx)
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("application.v6.scope.list", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, nil
	}
	values := make([]AppScopeStatus, 0, len(resp.Data.Scopes))
	for _, item := range resp.Data.Scopes {
		if item == nil {
			continue
		}
		values = append(values, AppScopeStatus{
			ScopeName:   strings.TrimSpace(scopeStringValue(item.ScopeName)),
			ScopeType:   normalizePermissionScopeType(scopeStringValue(item.ScopeType)),
			GrantStatus: scopeIntValue(item.GrantStatus),
		})
	}
	return values, nil
}

func scopeStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func scopeIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func scopeGranted(status AppScopeStatus) bool {
	status.ScopeName = strings.TrimSpace(status.ScopeName)
	if status.ScopeName == "" {
		return false
	}
	// The SDK exposes grant_status but does not document the enum inline.
	// Keep the mapping narrow to avoid false-positive auto-clear.
	return status.GrantStatus == 1
}
