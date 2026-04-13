package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type permissionDeniedOnceGateway struct {
	calls      int
	operations []feishu.Operation
}

func (g *permissionDeniedOnceGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *permissionDeniedOnceGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	if g.calls == 0 {
		g.calls++
		return &feishu.APIError{
			API:  "im.v1.message.create",
			Code: 99990001,
			Msg:  "permission denied",
			PermissionViolations: []feishu.APIErrorPermissionViolation{
				{Type: "tenant", Subject: "drive:drive"},
			},
			Helps: []feishu.APIErrorHelp{
				{URL: "https://open.feishu.cn/permission/apply"},
			},
		}
	}
	g.calls++
	g.operations = append(g.operations, operations...)
	return nil
}

func TestDaemonSuppressesImmediatePermissionFailureAndProjectsItInStatus(t *testing.T) {
	gateway := &permissionDeniedOnceGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected permission failure to stay silent immediately, got %#v", gateway.operations)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one status card after retry, got %#v", gateway.operations)
	}
	body := gateway.operations[0].CardBody
	if !strings.Contains(body, "**已知缺权限：**") || !strings.Contains(body, "drive:drive") {
		t.Fatalf("expected status card to include known permission gap, got %#v", body)
	}
	if !strings.Contains(body, "https://open.feishu.cn/permission/apply") {
		t.Fatalf("expected status card to include apply link, got %#v", body)
	}
}
