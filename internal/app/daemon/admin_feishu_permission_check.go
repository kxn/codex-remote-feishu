package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func (a *App) handleFeishuAppPermissionCheck(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	resp, err := a.buildFeishuAppPermissionCheck(r.Context(), gatewayID)
	if err != nil {
		switch {
		case strings.HasPrefix(err.Error(), "feishu_app_not_found:"):
			writeAPIError(w, http.StatusNotFound, apiError{
				Code:    "feishu_app_not_found",
				Message: "feishu app not found",
				Details: gatewayID,
			})
		case strings.HasPrefix(err.Error(), "feishu_app_runtime_unavailable:"):
			writeAPIError(w, http.StatusConflict, apiError{
				Code:    "feishu_app_runtime_unavailable",
				Message: "feishu app is not available at runtime",
				Details: gatewayID,
			})
		default:
			writeAPIError(w, http.StatusBadGateway, apiError{
				Code:    "feishu_permission_check_failed",
				Message: "failed to check feishu app permissions",
				Details: err.Error(),
			})
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) buildFeishuAppPermissionCheck(ctx context.Context, gatewayID string) (feishuAppPermissionCheckResponse, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return feishuAppPermissionCheckResponse{}, err
	}
	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil {
		return feishuAppPermissionCheckResponse{}, err
	}
	if !ok {
		return feishuAppPermissionCheckResponse{}, errFeishuAppNotFound(gatewayID)
	}
	if _, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID); !ok {
		return feishuAppPermissionCheckResponse{}, errFeishuAppRuntimeUnavailable(gatewayID)
	}

	checkCtx, cancel := context.WithTimeout(ctx, defaultFeishuAppTestSendTimeout)
	defer cancel()
	granted, err := a.loadGrantedFeishuScopes(checkCtx, gatewayID)
	a.applyFeishuPermissionVerificationResult(gatewayID, granted, err)
	if err != nil {
		return feishuAppPermissionCheckResponse{}, err
	}

	now := time.Now().UTC()
	missing := feishuAppMissingScopes(granted, feishuapp.DefaultManifest().Scopes)
	return feishuAppPermissionCheckResponse{
		App:           summary,
		Ready:         len(missing) == 0,
		MissingScopes: missing,
		GrantJSON:     buildFeishuPermissionGrantJSON(missing),
		LastCheckedAt: &now,
	}, nil
}

func feishuAppMissingScopes(granted []feishu.AppScopeStatus, scopes feishuapp.ScopesImport) []feishuAppPermissionCheckItem {
	grantedKeys := make(map[string]struct{}, len(granted)*2)
	for _, item := range granted {
		if !feishuScopeStatusGranted(item) {
			continue
		}
		scope := strings.TrimSpace(item.ScopeName)
		if scope == "" {
			continue
		}
		scopeType := strings.TrimSpace(item.ScopeType)
		grantedKeys[feishuPermissionGapKey(scope, scopeType)] = struct{}{}
		grantedKeys[feishuPermissionGapKey(scope, "")] = struct{}{}
	}

	var missing []feishuAppPermissionCheckItem
	appendMissing := func(scopeType string, values []string) {
		for _, value := range values {
			scope := strings.TrimSpace(value)
			if scope == "" {
				continue
			}
			if _, ok := grantedKeys[feishuPermissionGapKey(scope, scopeType)]; ok {
				continue
			}
			missing = append(missing, feishuAppPermissionCheckItem{
				Scope:     scope,
				ScopeType: scopeType,
			})
		}
	}

	appendMissing("tenant", scopes.Scopes.Tenant)
	appendMissing("user", scopes.Scopes.User)
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].ScopeType == missing[j].ScopeType {
			return missing[i].Scope < missing[j].Scope
		}
		return missing[i].ScopeType < missing[j].ScopeType
	})
	return missing
}

func buildFeishuPermissionGrantJSON(missing []feishuAppPermissionCheckItem) string {
	payload := feishuapp.ScopesImport{
		Scopes: feishuapp.PermissionScopes{
			Tenant: []string{},
			User:   []string{},
		},
	}
	for _, item := range missing {
		switch strings.TrimSpace(item.ScopeType) {
		case "user":
			payload.Scopes.User = append(payload.Scopes.User, item.Scope)
		default:
			payload.Scopes.Tenant = append(payload.Scopes.Tenant, item.Scope)
		}
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func buildFeishuAppConsoleLinks(appID string) feishuAppConsoleLinks {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return feishuAppConsoleLinks{}
	}
	base := "https://open.feishu.cn/app/" + appID
	return feishuAppConsoleLinks{
		Auth:     base + "/auth",
		Events:   base + "/event?tab=event",
		Callback: base + "/event?tab=callback",
		Bot:      base + "/bot",
	}
}
