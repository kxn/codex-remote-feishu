package feishu

import (
	"context"
	"errors"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestPrivatePublicationGatewayPinsExactApplication(t *testing.T) {
	c := NewMultiGatewayController()
	oldApp := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "old-app", AppSecret: "fixture"})
	newApp := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "new-app", AppSecret: "fixture"})
	sendErr := errors.New("controlled original transport failure")
	var oldWrites, newWrites int
	oldApp.createMessageFn = func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error) {
		oldWrites++
		return nil, sendErr
	}
	newApp.createMessageFn = func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error) {
		newWrites++
		return nil, nil
	}
	worker := &gatewayWorker{
		config:  GatewayAppConfig{GatewayID: "gateway", AppID: "old-app", Enabled: true},
		status:  GatewayStatus{State: GatewayStateConnected},
		runtime: oldApp,
	}
	c.workers["gateway"] = worker
	selected, err := c.PrivatePublicationGateway("gateway", "old-app")
	if err != nil {
		t.Fatal(err)
	}
	operation := Operation{Kind: OperationSendText, GatewayID: "gateway", ChatID: "oc_chat", Text: "private reply"}
	if err = selected.Apply(t.Context(), []Operation{operation}); !errors.Is(err, sendErr) || worker.status.State != GatewayStateDegraded {
		t.Fatalf("current gateway status did not record the delivery error: %s %v", worker.status.State, err)
	}
	worker.config.AppID = "new-app"
	worker.status = GatewayStatus{State: GatewayStateConnected}
	worker.runtime = newApp
	if _, err = c.PrivatePublicationGateway("gateway", "old-app"); err == nil {
		t.Fatal("old application resolved to the replacement runtime")
	}
	if err = selected.Apply(t.Context(), []Operation{operation}); !errors.Is(err, sendErr) {
		t.Fatalf("captured runtime was replaced: %v", err)
	}
	if oldWrites != 2 || newWrites != 0 || worker.status.State != GatewayStateConnected {
		t.Fatalf("old send affected replacement: oldWrites=%d newWrites=%d replacementState=%s", oldWrites, newWrites, worker.status.State)
	}
}
