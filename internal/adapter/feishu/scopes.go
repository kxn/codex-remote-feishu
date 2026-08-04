package feishu

import (
	"context"
	"errors"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
)

type AppScopeStatus struct {
	ScopeName   string
	ScopeType   string
	GrantStatus int
}

// scopeSatisfiers maps a manifest-required scope to the scope names that
// satisfy it (including itself), per Feishu's documented "any one of"
// permission relationships. Only relationships confirmed by official API
// docs are listed; token type must still match independently.
var scopeSatisfiers = map[string][]string{
	"im:message.p2p_msg:readonly": {
		"im:message.p2p_msg:readonly",
		"im:message.p2p_msg",
	},
	"im:message.group_at_msg:readonly": {
		"im:message.group_at_msg:readonly",
		"im:message.group_at_msg",
	},
	"im:message.group_at_msg.include_bot:readonly": {
		"im:message.group_at_msg.include_bot:readonly",
		"im:message.group_at_msg.include_bot",
	},
	"im:message.group_msg:readonly": {
		"im:message.group_msg:readonly",
		"im:message.group_msg",
	},
	"im:message:readonly": {
		"im:message:readonly",
		"im:message",
	},
	"im:message.reactions:read": {
		"im:message.reactions:read",
		"im:message:readonly",
	},
	"im:resource:upload": {
		"im:resource:upload",
		"im:resource",
	},
	"application:application:self_manage": {
		"application:application:self_manage",
		"admin:app.info:readonly",
	},
}

// satisfierScopes returns the scope names that satisfy the given requirement
// scope, always including the requirement itself.
func satisfierScopes(scope string) []string {
	scope = strings.TrimSpace(scope)
	if values, ok := scopeSatisfiers[scope]; ok && len(values) > 0 {
		return values
	}
	return []string{scope}
}

func matchScopeRequirement(requirementScope, requirementType string, configuredKeys map[string]bool) (string, bool) {
	for _, scope := range satisfierScopes(requirementScope) {
		if configuredKeys[scopeKey(scope, requirementType)] {
			return scope, true
		}
	}
	return "", false
}

// MatchScopeRequirement returns the configured scope that satisfies a
// manifest requirement. It is the shared matcher for setup/admin and runtime
// permission decisions.
func MatchScopeRequirement(requirementScope, requirementType string, configured []AppScopeStatus) (string, bool) {
	configuredKeys := make(map[string]bool, len(configured))
	for _, item := range configured {
		if !scopeGranted(item) {
			continue
		}
		configuredKeys[scopeKey(item.ScopeName, item.ScopeType)] = true
	}
	return matchScopeRequirement(requirementScope, requirementType, configuredKeys)
}

// ScopeRequirementSatisfied reports whether any granted configured scope
// satisfies a manifest requirement, including documented alternatives.
func ScopeRequirementSatisfied(requirementScope, requirementType string, configured []AppScopeStatus) bool {
	_, ok := MatchScopeRequirement(requirementScope, requirementType, configured)
	return ok
}

// ListAppConfiguredScopes reads the app's configured scopes from the config
// side (application.get -> app.scopes) and normalizes config-side presence as
// granted (GrantStatus=1). For legacy apps that cannot read application.get,
// it has a narrow scope.list fallback; config-side presence remains the
// authoritative signal whenever it is available.
func ListAppConfiguredScopes(ctx context.Context, cfg LiveGatewayConfig) ([]AppScopeStatus, error) {
	return NewSetupClient(SetupClientConfigFromLiveGatewayConfig(cfg)).ListAppConfiguredScopes(ctx)
}

func (c *SetupClient) ListAppConfiguredScopes(ctx context.Context) ([]AppScopeStatus, error) {
	sdkClient, broker := c.sdk()
	cfg := c.liveGatewayConfig()
	app, err := getApplicationConfig(ctx, broker, sdkClient, cfg.AppID)
	if err != nil && canFallbackToGrantedScopes(err) {
		// Older self-built apps may not have application:self_manage, which
		// makes application.get unavailable even though scope.list can still
		// report the bot's granted scopes. Keep application.get authoritative
		// when it works, but preserve runtime permission checks for those apps.
		if scopes, fallbackErr := c.listGrantedScopes(ctx); fallbackErr == nil {
			return scopes, nil
		}
		return nil, err
	}
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

func canFallbackToGrantedScopes(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == 99991672
}

func (c *SetupClient) listGrantedScopes(ctx context.Context) ([]AppScopeStatus, error) {
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
