package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatAdminCheckerAllowsOwner(t *testing.T) {
	checker := newTestChatAdminChecker(t, chatAdminTestResponse(`"owner_id":"ou_owner","user_manager_id_list":[]`), nil)

	decision, err := checker.IsUserChatOwnerOrManager(context.Background(), "oc_1", ChatUserIdentity{ID: "ou_owner", IDType: "open_id"})
	if err != nil {
		t.Fatalf("IsUserChatOwnerOrManager: %v", err)
	}
	if !decision.Allowed || decision.Reason != ChatAdminDecisionOwner {
		t.Fatalf("decision = %#v, want owner allowed", decision)
	}
}

func TestChatAdminCheckerAllowsUserManager(t *testing.T) {
	checker := newTestChatAdminChecker(t, chatAdminTestResponse(`"owner_id":"ou_owner","user_manager_id_list":["ou_manager"]`), nil)

	decision, err := checker.IsUserChatOwnerOrManager(context.Background(), "oc_1", ChatUserIdentity{ID: "ou_manager", IDType: "open_id"})
	if err != nil {
		t.Fatalf("IsUserChatOwnerOrManager: %v", err)
	}
	if !decision.Allowed || decision.Reason != ChatAdminDecisionUserManager {
		t.Fatalf("decision = %#v, want user manager allowed", decision)
	}
}

func TestChatAdminCheckerDoesNotAllowBotManagerOnly(t *testing.T) {
	checker := newTestChatAdminChecker(t, chatAdminTestResponse(`"owner_id":"ou_owner","user_manager_id_list":[],"bot_manager_id_list":["ou_actor"]`), nil)

	decision, err := checker.IsUserChatOwnerOrManager(context.Background(), "oc_1", ChatUserIdentity{ID: "ou_actor", IDType: "open_id"})
	if err != nil {
		t.Fatalf("IsUserChatOwnerOrManager: %v", err)
	}
	if decision.Allowed || decision.Reason != ChatAdminDecisionNotAdmin {
		t.Fatalf("decision = %#v, want not admin", decision)
	}
}

func TestChatAdminCheckerFailsClosedForMissingActor(t *testing.T) {
	checker := newTestChatAdminChecker(t, chatAdminTestResponse(`"owner_id":"ou_owner","user_manager_id_list":["ou_manager"]`), nil)

	decision, err := checker.IsUserChatOwnerOrManager(context.Background(), "oc_1", ChatUserIdentity{IDType: "open_id"})
	if err != nil {
		t.Fatalf("IsUserChatOwnerOrManager: %v", err)
	}
	if decision.Allowed || decision.Reason != ChatAdminDecisionMissingActor {
		t.Fatalf("decision = %#v, want missing actor", decision)
	}
}

func TestChatAdminCheckerFailsClosedForAPIError(t *testing.T) {
	checker := newTestChatAdminChecker(t, `{"code":99991663,"msg":"permission denied"}`, nil)

	decision, err := checker.IsUserChatOwnerOrManager(context.Background(), "oc_1", ChatUserIdentity{ID: "ou_owner", IDType: "open_id"})
	if err == nil {
		t.Fatal("expected API error")
	}
	if decision.Allowed || decision.Reason != ChatAdminDecisionChatInfoUnavailable {
		t.Fatalf("decision = %#v, want chat info unavailable", decision)
	}
}

func TestChatAdminCheckerCachesChatInfo(t *testing.T) {
	var chatInfoCalls atomic.Int32
	checker := newTestChatAdminChecker(t, chatAdminTestResponse(`"owner_id":"ou_owner","user_manager_id_list":[]`), &chatInfoCalls)

	for i := 0; i < 2; i++ {
		decision, err := checker.IsUserChatOwnerOrManager(context.Background(), "oc_1", ChatUserIdentity{ID: "ou_owner", IDType: "open_id"})
		if err != nil {
			t.Fatalf("IsUserChatOwnerOrManager #%d: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("decision #%d = %#v, want allowed", i+1, decision)
		}
	}
	if got := chatInfoCalls.Load(); got != 1 {
		t.Fatalf("chat info calls = %d, want 1", got)
	}
}

func newTestChatAdminChecker(t *testing.T, chatInfoResponse string, chatInfoCalls *atomic.Int32) *ChatAdminChecker {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/im/v1/chats/oc_1":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q, want tenant token", got)
			}
			if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
				t.Fatalf("user_id_type = %q, want open_id", got)
			}
			if chatInfoCalls != nil {
				chatInfoCalls.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(chatInfoResponse))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	return NewChatAdminChecker(SetupClientConfig{
		GatewayID: "main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	}, time.Minute)
}

func chatAdminTestResponse(fields string) string {
	return `{"code":0,"msg":"success","data":{"chat_id":"oc_1",` + strings.TrimSpace(fields) + `}}`
}
