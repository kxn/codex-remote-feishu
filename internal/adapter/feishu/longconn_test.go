package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestVerifyGatewayConnectionSuccess(t *testing.T) {
	server := newGatewayWSTestServer(t, gatewayWSTestServerConfig{})
	defer server.Close()

	result, err := VerifyGatewayConnection(context.Background(), LiveGatewayConfig{
		GatewayID: "app-1",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})
	if err != nil {
		t.Fatalf("VerifyGatewayConnection: %v", err)
	}
	if !result.Connected {
		t.Fatalf("expected successful verification, got %#v", result)
	}
}

func TestVerifyGatewayConnectionAuthFailure(t *testing.T) {
	server := newGatewayWSTestServer(t, gatewayWSTestServerConfig{
		EndpointCode: larkws.AuthFailed,
		EndpointMsg:  "auth failed",
	})
	defer server.Close()

	result, err := VerifyGatewayConnection(context.Background(), LiveGatewayConfig{
		GatewayID: "app-1",
		AppID:     "cli_xxx",
		AppSecret: "bad_secret",
		Domain:    server.URL,
	})
	if err == nil {
		t.Fatal("expected verification error")
	}
	if result.ErrorCode != "auth_failed" {
		t.Fatalf("unexpected verify result: %#v", result)
	}
}

func TestLiveGatewayStartStopsOnContextCancel(t *testing.T) {
	server := newGatewayWSTestServer(t, gatewayWSTestServerConfig{})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectedCh := make(chan struct{}, 1)
	gateway := NewLiveGateway(LiveGatewayConfig{
		GatewayID: "app-1",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})
	gateway.SetStateHook(func(state GatewayState, err error) {
		if state == GatewayStateConnected {
			select {
			case connectedCh <- struct{}{}:
			default:
			}
		}
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- gateway.Start(ctx, func(context.Context, control.Action) {})
	}()

	select {
	case <-connectedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway to connect")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("gateway.Start returned error after cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway to stop after cancel")
	}
}

type gatewayWSTestServerConfig struct {
	EndpointCode int
	EndpointMsg  string
}

func newGatewayWSTestServer(t *testing.T, cfg gatewayWSTestServerConfig) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?service_id=1&device_id=test"
	if cfg.EndpointCode == 0 {
		cfg.EndpointCode = larkws.OK
	}

	mux.HandleFunc(larkws.GenEndpointUri, func(w http.ResponseWriter, r *http.Request) {
		payload := larkws.EndpointResp{
			Code: cfg.EndpointCode,
			Msg:  cfg.EndpointMsg,
		}
		if cfg.EndpointCode == larkws.OK {
			payload.Data = &larkws.Endpoint{
				Url: wsURL,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	return server
}
