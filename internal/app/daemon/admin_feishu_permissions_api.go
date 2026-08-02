package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

const defaultFeishuPermissionCheckTimeout = 20 * time.Second

type feishuAppPermissionCheckItem struct {
	Scope     string `json:"scope"`
	ScopeType string `json:"scopeType,omitempty"`
}

type feishuAppPermissionCheckResponse struct {
	App           adminFeishuAppSummary          `json:"app"`
	Ready         bool                           `json:"ready"`
	MissingScopes []feishuAppPermissionCheckItem `json:"missingScopes,omitempty"`
	GrantJSON     string                         `json:"grantJSON"`
	LastCheckedAt time.Time                      `json:"lastCheckedAt"`
}

func (a *App) handleFeishuAppPermissionsCheck(w http.ResponseWriter, r *http.Request) {
	summary, runtimeCfg, err := a.loadFeishuLiveGatewayTarget(r.PathValue("id"))
	if err != nil {
		a.writeFeishuAppTargetError(w, err)
		return
	}

	checkCtx, cancel := context.WithTimeout(r.Context(), defaultFeishuPermissionCheckTimeout)
	defer cancel()
	grantedScopes, err := listFeishuAppScopes(checkCtx, runtimeCfg)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, apiError{
			Code:    "feishu_permission_check_failed",
			Message: "failed to check feishu app permissions",
			Details: err.Error(),
		})
		return
	}

	response, err := buildFeishuAppPermissionCheckResponse(
		summary,
		feishuapp.DefaultManifest(),
		grantedScopes,
		time.Now().UTC(),
	)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "feishu_permission_manifest_unavailable",
			Message: "failed to build feishu permission import JSON",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func buildFeishuAppPermissionCheckResponse(
	app adminFeishuAppSummary,
	manifest feishuapp.Manifest,
	grantedScopes []feishu.AppScopeStatus,
	checkedAt time.Time,
) (feishuAppPermissionCheckResponse, error) {
	grantJSON, err := json.MarshalIndent(manifest.Scopes, "", "  ")
	if err != nil {
		return feishuAppPermissionCheckResponse{}, err
	}
	missingScopes := missingFeishuManifestScopes(manifest, grantedScopes)
	return feishuAppPermissionCheckResponse{
		App:           app,
		Ready:         len(missingScopes) == 0,
		MissingScopes: missingScopes,
		GrantJSON:     string(grantJSON),
		LastCheckedAt: checkedAt,
	}, nil
}

func missingFeishuManifestScopes(
	manifest feishuapp.Manifest,
	grantedScopes []feishu.AppScopeStatus,
) []feishuAppPermissionCheckItem {
	granted := map[string]bool{}
	for _, item := range grantedScopes {
		if !feishuScopeStatusGranted(item) {
			continue
		}
		granted[feishuPermissionGapKey(item.ScopeName, item.ScopeType)] = true
		granted[feishuPermissionGapKey(item.ScopeName, "")] = true
	}

	requiredScopes := manifest.ScopeRequirements
	if len(requiredScopes) == 0 {
		requiredScopes = scopeRequirementsFromImport(manifest.Scopes)
	}
	missing := make([]feishuAppPermissionCheckItem, 0)
	seen := map[string]bool{}
	for _, item := range requiredScopes {
		scope := strings.TrimSpace(item.Scope)
		if scope == "" {
			continue
		}
		scopeType := strings.TrimSpace(item.ScopeType)
		key := feishuPermissionGapKey(scope, scopeType)
		if seen[key] {
			continue
		}
		seen[key] = true
		if granted[key] || granted[feishuPermissionGapKey(scope, "")] {
			continue
		}
		missing = append(missing, feishuAppPermissionCheckItem{
			Scope:     scope,
			ScopeType: scopeType,
		})
	}
	return missing
}

func scopeRequirementsFromImport(scopes feishuapp.ScopesImport) []feishuapp.ScopeRequirement {
	out := make([]feishuapp.ScopeRequirement, 0, len(scopes.Scopes.Tenant)+len(scopes.Scopes.User))
	for _, scope := range scopes.Scopes.Tenant {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			out = append(out, feishuapp.ScopeRequirement{Scope: scope, ScopeType: "tenant"})
		}
	}
	for _, scope := range scopes.Scopes.User {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			out = append(out, feishuapp.ScopeRequirement{Scope: scope, ScopeType: "user"})
		}
	}
	return out
}
