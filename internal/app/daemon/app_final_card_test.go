package daemon

import (
	"context"
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type messageIDAssigningGateway struct {
	next       int
	operations []feishu.Operation
}

func (g *messageIDAssigningGateway) Start(context.Context, feishu.ActionHandler) error { return nil }

func (g *messageIDAssigningGateway) Apply(_ context.Context, operations []feishu.Operation) error {
	for i := range operations {
		if operations[i].Kind != feishu.OperationSendCard || operations[i].MessageID != "" {
			continue
		}
		g.next++
		operations[i].MessageID = fmt.Sprintf("om-card-%d", g.next)
	}
	g.operations = append(g.operations, operations...)
	return nil
}

func TestDeliverUIEventRecordsFinalCardAnchorFromPrimaryFinalReply(t *testing.T) {
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetFinalBlockPreviewer(&stubMarkdownPreviewer{
		supplements: []feishu.PreviewSupplement{{
			Kind: "card",
			Data: map[string]any{
				"title": "补充信息",
				"body":  "preview link",
			},
		}},
	})

	app.service.MaterializeSurface("feishu:chat:1", "app-1", "chat-1", "ou_user")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})

	event := control.UIEvent{
		Kind:             control.UIEventBlockCommitted,
		SurfaceSessionID: "feishu:chat:1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-1",
			TurnID:     "turn-1",
			ItemID:     "item-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	}
	if err := app.deliverUIEventWithContext(context.Background(), event); err != nil {
		t.Fatalf("deliver final block: %v", err)
	}

	got := app.service.LookupFinalCardForBlock("feishu:chat:1", *event.Block, app.daemonLifecycleID)
	if got == nil {
		t.Fatal("expected retained final card anchor")
	}
	if got.MessageID != "om-card-1" || got.SourceMessageID != "msg-1" {
		t.Fatalf("unexpected final card anchor: %#v", got)
	}
	if len(gateway.operations) != 2 {
		t.Fatalf("expected final card plus supplement send ops, got %#v", gateway.operations)
	}
	if gateway.operations[0].MessageID != "om-card-1" || gateway.operations[1].MessageID != "om-card-2" {
		t.Fatalf("unexpected sent message ids: %#v", gateway.operations)
	}
}
