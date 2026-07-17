package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetLongConnectionStatusReturnsOnlineCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/event/v1/connection":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q, want tenant token", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"online_instance_cnt":2}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	status, err := GetLongConnectionStatus(context.Background(), LiveGatewayConfig{
		GatewayID: "main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})
	if err != nil {
		t.Fatalf("GetLongConnectionStatus: %v", err)
	}
	if status.OnlineInstanceCount != 2 {
		t.Fatalf("OnlineInstanceCount = %d, want 2", status.OnlineInstanceCount)
	}
	if status.CheckedAt.IsZero() {
		t.Fatalf("expected CheckedAt")
	}
}

func TestGetLongConnectionStatusSurfacesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1810001,"msg":"param is invalid"}`))
	}))
	defer server.Close()

	_, err := GetLongConnectionStatus(context.Background(), LiveGatewayConfig{
		GatewayID: "main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !strings.Contains(err.Error(), "1810001") {
		t.Fatalf("expected error code in %q", err.Error())
	}
}
