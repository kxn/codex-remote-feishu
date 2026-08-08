package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLiveGatewayGetChatInfoReturnsBotCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/im/v1/chats/oc_chat":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q, want tenant token", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"bot_count":"1","chat_mode":"group"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	gateway := NewLiveGateway(LiveGatewayConfig{
		GatewayID: "main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})

	info, err := gateway.GetChatInfo(context.Background(), "oc_chat")
	if err != nil {
		t.Fatalf("GetChatInfo: %v", err)
	}
	if info.BotCount != 1 || info.ChatMode != "group" {
		t.Fatalf("unexpected chat info: %#v", info)
	}
}

func TestLiveGatewayGetChatInfoRejectsInvalidBotCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"bot_count":"unknown"}}`))
	}))
	defer server.Close()
	gateway := NewLiveGateway(LiveGatewayConfig{
		GatewayID: "main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})

	_, err := gateway.GetChatInfo(context.Background(), "oc_chat")
	if err == nil {
		t.Fatal("expected invalid bot_count error")
	}
	if !strings.Contains(err.Error(), "bot_count") {
		t.Fatalf("error = %q, want bot_count context", err.Error())
	}
}
