package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestListAppConfiguredScopesReadsConfigSideAndSkipsScopeList(t *testing.T) {
	appID := "cli_configured_scopes"
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/application/v6/applications/" + appID:
			paths = append(paths, r.URL.Path)
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"app":{"app_id":"cli_configured_scopes","scopes":[{"scope":"im:message","token_types":["tenant"]},{"scope":"calendar:calendar:read","token_types":["user"]},{"scope":"drive:drive","token_types":[]}]}}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	scopes, err := ListAppConfiguredScopes(context.Background(), LiveGatewayConfig{
		GatewayID: "main",
		AppID:     appID,
		AppSecret: "secret",
		Domain:    server.URL,
	})
	if err != nil {
		t.Fatalf("ListAppConfiguredScopes: %v", err)
	}
	want := []AppScopeStatus{
		{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
		{ScopeName: "calendar:calendar:read", ScopeType: "user", GrantStatus: 1},
		{ScopeName: "drive:drive", ScopeType: "tenant", GrantStatus: 1},
	}
	if !reflect.DeepEqual(scopes, want) {
		t.Fatalf("scopes = %#v, want %#v", scopes, want)
	}
	for _, path := range paths {
		if path == "/open-apis/application/v6/scopes" {
			t.Fatalf("scope.list must not be consulted by the configured-scope reader")
		}
	}
}
