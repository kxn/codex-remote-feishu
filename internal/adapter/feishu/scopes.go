package feishu

import (
	"context"
	"strings"
)

type AppScopeStatus struct {
	ScopeName   string
	ScopeType   string
	GrantStatus int
}

// ListAppConfiguredScopes reads the app's configured scopes from the config
// side (application.get -> app.scopes) and reports them as granted
// (GrantStatus=1). For self-built / QR-registered apps, config-side presence
// is the reliable availability signal, unlike scope.list whose grant_status
// reflects tenant-authorization state and can report false negatives.
func ListAppConfiguredScopes(ctx context.Context, cfg LiveGatewayConfig) ([]AppScopeStatus, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).ListAppConfiguredScopes(ctx)
}

func (c *SetupClient) ListAppConfiguredScopes(ctx context.Context) ([]AppScopeStatus, error) {
	sdkClient, broker := c.sdk()
	cfg := c.liveGatewayConfig()
	app, err := getApplicationConfig(ctx, broker, sdkClient, cfg.AppID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, nil
	}
	values := make([]AppScopeStatus, 0, len(app.Scopes))
	for _, item := range app.Scopes {
		if item == nil {
			continue
		}
		scope := strings.TrimSpace(stringValue(item.Scope))
		if scope == "" {
			continue
		}
		if len(item.TokenTypes) == 0 {
			values = append(values, AppScopeStatus{ScopeName: scope, ScopeType: "tenant", GrantStatus: 1})
			continue
		}
		for _, tokenType := range item.TokenTypes {
			values = append(values, AppScopeStatus{
				ScopeName:   scope,
				ScopeType:   normalizePermissionScopeType(tokenType),
				GrantStatus: 1,
			})
		}
	}
	return values, nil
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
