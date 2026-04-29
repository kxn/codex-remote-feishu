package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
)

func TestLarkDrivePreviewAPIGrantPermissionUsesFullAccess(t *testing.T) {
	type permissionRequest struct {
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Perm       string `json:"perm"`
		Type       string `json:"type"`
	}

	var (
		tokenHits      int
		permissionHits int
		grantReq       permissionRequest
		authHeader     string
		requestPath    string
		requestType    string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case larkcore.TenantAccessTokenInternalUrlPath:
			tokenHits++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"msg":                 "ok",
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case "/open-apis/drive/v1/permissions/file-token/members":
			permissionHits++
			requestPath = r.URL.Path
			requestType = r.URL.Query().Get("type")
			authHeader = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&grantReq); err != nil {
				t.Fatalf("decode permission request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"msg":  "ok",
				"data": map[string]any{},
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := lark.NewClient(
		"cli_test_app",
		"test_secret",
		lark.WithOpenBaseUrl(server.URL),
		lark.WithHttpClient(server.Client()),
		lark.WithReqTimeout(5*time.Second),
	)
	api := &larkDrivePreviewAPI{
		client: client,
		broker: NewFeishuCallBroker("app-1", client),
	}

	err := api.GrantPermission(context.Background(), "file-token", "file", previewpkg.Principal{
		Key:        "openid:ou_user",
		MemberType: "openid",
		MemberID:   "ou_user",
		Type:       "user",
	})
	if err != nil {
		t.Fatalf("GrantPermission returned error: %v", err)
	}
	if tokenHits != 1 {
		t.Fatalf("expected one tenant token fetch, got %d", tokenHits)
	}
	if permissionHits != 1 {
		t.Fatalf("expected one permission create request, got %d", permissionHits)
	}
	if requestPath != "/open-apis/drive/v1/permissions/file-token/members" {
		t.Fatalf("unexpected request path: %s", requestPath)
	}
	if requestType != "file" {
		t.Fatalf("unexpected type query: %q", requestType)
	}
	if authHeader != "Bearer tenant-token" {
		t.Fatalf("unexpected authorization header: %q", authHeader)
	}
	if grantReq.MemberType != "openid" || grantReq.MemberID != "ou_user" || grantReq.Type != "user" {
		t.Fatalf("unexpected permission request principal: %#v", grantReq)
	}
	if grantReq.Perm != previewpkg.PermissionFullAccess {
		t.Fatalf("expected perm %q, got %#v", previewpkg.PermissionFullAccess, grantReq)
	}
}
