package feishu

import (
	"context"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
)

type AppScopeStatus struct {
	ScopeName   string
	ScopeType   string
	GrantStatus int
}

func ListAppScopes(ctx context.Context, cfg LiveGatewayConfig) ([]AppScopeStatus, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).ListAppScopes(ctx)
}

func (c *SetupClient) ListAppScopes(ctx context.Context) ([]AppScopeStatus, error) {
	_, broker := c.sdk()
	cfg := c.liveGatewayConfig()
	resp, err := DoSDK(ctx, broker, CallSpec{
		GatewayID:  cfg.GatewayID,
		API:        "application.v6.scope.list",
		Class:      CallClassMetaHTTP,
		Priority:   CallPriorityBackground,
		Retry:      RetrySafe,
		Permission: PermissionFailFast,
	}, func(callCtx context.Context, sdkClient *lark.Client) (*larkapplication.ListScopeResp, error) {
		resp, err := sdkClient.Application.V6.Scope.List(callCtx)
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
