package daemon

import (
	"context"
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
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected first gateway apply to fail without delivered operations, got %#v", gateway.operations)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(gateway.operations) < 2 {
		t.Fatalf("expected queued error notice and current card after recovery, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardTitle, "链路错误") || !strings.Contains(gateway.operations[0].CardBody, "位置：<text_tag color='neutral'>gateway_apply</text_tag>") {
		t.Fatalf("expected queued gateway failure notice first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].CardTitle != "选择工作区与会话" {
		t.Fatalf("expected recovered response to be target picker card, got %#v", gateway.operations[1])
	}
	sawAddWorkspaceText := false
	for _, element := range gateway.operations[1].CardElements {
		content, _ := element["content"].(string)
		sawAddWorkspaceText = sawAddWorkspaceText || strings.Contains(content, "完成后会直接进入新会话待命") || strings.Contains(content, "工作区来源")
	}
	if !sawAddWorkspaceText {
		t.Fatalf("expected recovered target picker to expose add-workspace flow, got %#v", gateway.operations[1])
	}
	sawChooseDirectory := false
	sawDisabledAttach := false
	for _, button := range operationCardButtons(gateway.operations[1]) {
		textNode, _ := button["text"].(map[string]any)
		label, _ := textNode["content"].(string)
		switch label {
		case "选择目录":
			sawChooseDirectory = true
		case "接入并继续":
			sawDisabledAttach = button["disabled"] == true
		}
	}
	if !sawChooseDirectory || !sawDisabledAttach {
		t.Fatalf("expected recovered target picker to expose choose-directory and disabled attach actions, got %#v", gateway.operations[1])
	}
}
