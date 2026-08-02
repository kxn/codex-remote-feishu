package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func TestAdminFeishuPermissionsCheckRouteReportsMissingScopes(t *testing.T) {
	oldListScopes := listFeishuAppScopes
	t.Cleanup(func() {
		listFeishuAppScopes = oldListScopes
	})
	listFeishuAppScopes = func(_ context.Context, cfg feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		if cfg.GatewayID != "main" || cfg.AppID != "cli_xxx" || cfg.AppSecret != "secret_xxx" {
			t.Fatalf("unexpected scope check config: %#v", cfg)
		}
		manifest := feishuapp.DefaultManifest()
		scopes := make([]feishu.AppScopeStatus, 0, len(manifest.Scopes.Scopes.Tenant))
		for _, scope := range manifest.Scopes.Scopes.Tenant {
			if scope == "im:message.group_at_msg:readonly" {
				continue
			}
			scopes = append(scopes, feishu.AppScopeStatus{
				ScopeName:   scope,
				ScopeType:   "tenant",
				GrantStatus: 1,
			})
		}
		return scopes, nil
	}

	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps/main/permissions/check", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("permission check status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		App struct {
			ID string `json:"id"`
		} `json:"app"`
		Ready         bool `json:"ready"`
		MissingScopes []struct {
			Scope     string `json:"scope"`
			ScopeType string `json:"scopeType,omitempty"`
		} `json:"missingScopes"`
		GrantJSON     string `json:"grantJSON"`
		LastCheckedAt string `json:"lastCheckedAt"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode permission check response: %v", err)
	}
	if resp.App.ID != "main" {
		t.Fatalf("app id = %q, want main", resp.App.ID)
	}
	if resp.Ready {
		t.Fatal("ready = true, want false while a manifest scope is missing")
	}
	if len(resp.MissingScopes) != 1 ||
		resp.MissingScopes[0].Scope != "im:message.group_at_msg:readonly" ||
		resp.MissingScopes[0].ScopeType != "tenant" {
		t.Fatalf("unexpected missing scopes: %#v", resp.MissingScopes)
	}
	if !strings.Contains(resp.GrantJSON, `"im:message.group_at_msg:readonly"`) ||
		!strings.Contains(resp.GrantJSON, `"scopes"`) {
		t.Fatalf("grant JSON does not contain manifest scopes import: %s", resp.GrantJSON)
	}
	if strings.TrimSpace(resp.LastCheckedAt) == "" {
		t.Fatal("lastCheckedAt is empty")
	}
}
