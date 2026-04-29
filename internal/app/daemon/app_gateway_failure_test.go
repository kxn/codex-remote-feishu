package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestDaemonFlushesQueuedGatewayFailureNoticeOnNextSuccess(t *testing.T) {
	gateway := &flakyGateway{failures: 1}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected first gateway apply to fail without delivered operations, got %#v", gateway.operations)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) < 2 {
		t.Fatalf("expected queued error notice with attention and current card after recovery, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardTitle, "链路错误") || gateway.operations[0].CardBody != "" || !strings.Contains(fmt.Sprint(gateway.operations[0].CardElements), "位置：gateway_apply") {
		t.Fatalf("expected queued gateway failure notice first, got %#v", gateway.operations[0])
	}
	if gateway.operations[0].AttentionText != "需要你回来处理：飞书投递失败。" || gateway.operations[0].AttentionUserID != "user-1" {
		t.Fatalf("expected queued gateway failure notice to carry attention, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].CardTitle != "切换工作区与会话" {
		t.Fatalf("expected recovered response to be target picker card, got %#v", gateway.operations[1])
	}
	if !strings.Contains(fmt.Sprint(gateway.operations[1].CardElements), "当前还没有可切换的工作区") {
		t.Fatalf("expected recovered target picker to explain missing workspaces, got %#v", gateway.operations[1])
	}
	sawConfirmSwitch := false
	sawDisabledSwitch := false
	for _, button := range operationCardButtons(gateway.operations[1]) {
		textNode, _ := button["text"].(map[string]any)
		label, _ := textNode["content"].(string)
		switch label {
		case "切换":
			sawConfirmSwitch = true
			sawDisabledSwitch = button["disabled"] == true
		}
	}
	if !sawConfirmSwitch || !sawDisabledSwitch {
		t.Fatalf("expected recovered target picker to expose disabled switch action, got %#v", gateway.operations[1])
	}
}
