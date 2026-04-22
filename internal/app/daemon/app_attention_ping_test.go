package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestHandleUIEventsAddsAttentionPingForRequestOncePerRevision(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	requestEvent := control.UIEvent{
		Kind:             control.UIEventFeishuRequestView,
		SurfaceSessionID: "surface-1",
		FeishuRequestView: &control.FeishuRequestView{
			RequestID:       "req-1",
			RequestType:     "approval",
			RequestRevision: 1,
			Title:           "需要确认",
			Sections: []control.FeishuCardTextSection{{
				Lines: []string{"请确认是否继续。"},
			}},
			Options: []control.RequestPromptOption{{
				OptionID: "accept",
				Label:    "允许执行",
				Style:    "primary",
			}},
		},
	}

	app.handleUIEvents(context.Background(), []control.UIEvent{requestEvent})
	app.handleUIEvents(context.Background(), []control.UIEvent{requestEvent})

	if len(gateway.operations) != 3 {
		t.Fatalf("expected request card + ping + request card, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected first operation to be request card, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].Kind != feishu.OperationSendText || gateway.operations[1].MentionUserID != "ou-user-1" {
		t.Fatalf("expected second operation to be attention ping, got %#v", gateway.operations[1])
	}
	if gateway.operations[1].ReplyToMessageID != "" || gateway.operations[1].Text != "需要你回来处理：请确认这条请求。" {
		t.Fatalf("unexpected request attention ping: %#v", gateway.operations[1])
	}
	if gateway.operations[2].Kind != feishu.OperationSendCard {
		t.Fatalf("expected rerender to keep original request card only, got %#v", gateway.operations[2])
	}
}

func TestHandleUIEventsRetriesRequestAttentionPingAfterAnchorDeliveryFailure(t *testing.T) {
	gateway := &flakyGateway{failures: 1}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	requestEvent := control.UIEvent{
		Kind:             control.UIEventFeishuRequestView,
		SurfaceSessionID: "surface-1",
		FeishuRequestView: &control.FeishuRequestView{
			RequestID:       "req-1",
			RequestType:     "approval",
			RequestRevision: 1,
			Title:           "需要确认",
			Options: []control.RequestPromptOption{{
				OptionID: "accept",
				Label:    "允许执行",
			}},
		},
	}

	app.handleUIEvents(context.Background(), []control.UIEvent{requestEvent})

	if len(gateway.operations) != 0 {
		t.Fatalf("expected failed request delivery not to emit orphan attention ping, got %#v", gateway.operations)
	}
	if got := len(app.feishuRuntime.attentionRequests); got != 0 {
		t.Fatalf("expected failed request delivery not to consume dedupe state, got %#v", app.feishuRuntime.attentionRequests)
	}

	delete(app.pendingGlobalRuntimeNotices, "surface-1")
	app.handleUIEvents(context.Background(), []control.UIEvent{requestEvent})

	if len(gateway.operations) != 2 {
		t.Fatalf("expected successful retry to deliver request card and ping, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected retried request card first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].Kind != feishu.OperationSendText || gateway.operations[1].Text != "需要你回来处理：请确认这条请求。" {
		t.Fatalf("expected retried attention ping after successful request delivery, got %#v", gateway.operations[1])
	}
	if got := len(app.feishuRuntime.attentionRequests); got != 1 {
		t.Fatalf("expected successful request ping to record dedupe state, got %#v", app.feishuRuntime.attentionRequests)
	}
}

func TestHandleUIEventsMergesFinalAndPlanProposalIntoOneAttentionPing(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []control.UIEvent{
		{
			Kind:             control.UIEventBlockCommitted,
			SurfaceSessionID: "surface-1",
			SourceMessageID:  "om-source-1",
			Block: &render.Block{
				Kind:        render.BlockAssistantMarkdown,
				Text:        "已完成修改。",
				ThreadID:    "thread-1",
				ThreadTitle: "droid · 修复登录流程",
				ThemeKey:    "thread-1",
				Final:       true,
			},
		},
		{
			Kind:             control.UIEventFeishuPageView,
			SurfaceSessionID: "surface-1",
			FeishuPageView: &control.FeishuPageView{
				CommandID: control.FeishuCommandPlan,
				Title:     "提案计划",
				Sections: []control.CommandCatalogSection{{
					Title: "下一步",
					Entries: []control.CommandCatalogEntry{{
						Buttons: []control.CommandCatalogButton{{
							Label: "直接执行",
						}},
					}},
				}},
			},
		},
	})

	if len(gateway.operations) != 3 {
		t.Fatalf("expected final + plan + ping, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].ReplyToMessageID != "om-source-1" {
		t.Fatalf("expected final reply card first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].Kind != feishu.OperationSendCard || gateway.operations[1].ReplyToMessageID != "" {
		t.Fatalf("expected plan proposal card second, got %#v", gateway.operations[1])
	}
	if gateway.operations[2].Kind != feishu.OperationSendText || gateway.operations[2].ReplyToMessageID != "" {
		t.Fatalf("expected merged attention ping after plan proposal, got %#v", gateway.operations[2])
	}
	if gateway.operations[2].Text != "需要你回来处理：本轮执行已结束，并生成了提案计划。" {
		t.Fatalf("unexpected plan attention ping text: %#v", gateway.operations[2])
	}
}

func TestHandleUIEventsUsesFailureAttentionPingWhenTurnFails(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []control.UIEvent{
		{
			Kind:             control.UIEventBlockCommitted,
			SurfaceSessionID: "surface-1",
			SourceMessageID:  "om-source-1",
			Block: &render.Block{
				Kind:        render.BlockAssistantMarkdown,
				Text:        "我先看了一下问题。",
				ThreadID:    "thread-1",
				ThreadTitle: "droid · 修复登录流程",
				ThemeKey:    "thread-1",
				Final:       true,
			},
		},
		{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: "surface-1",
			Notice: &control.Notice{
				Code: "turn_failed",
				Text: "stream disconnected before completion",
			},
		},
	})

	if len(gateway.operations) != 3 {
		t.Fatalf("expected final card + failure notice + ping, got %#v", gateway.operations)
	}
	if gateway.operations[2].Kind != feishu.OperationSendText || gateway.operations[2].ReplyToMessageID != "" {
		t.Fatalf("expected failure attention ping to follow top-level notice, got %#v", gateway.operations[2])
	}
	if gateway.operations[2].Text != "需要你回来处理：本轮执行已停止。" {
		t.Fatalf("unexpected failure attention ping text: %#v", gateway.operations[2])
	}
}

func TestHandleUIEventsAddsAttentionPingOnlyForTargetedGlobalRuntimeNotices(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:             "attached_instance_transport_degraded",
			Text:             "实例离线。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyTransportDegraded,
			DeliveryDedupKey: "attached_instance_transport_degraded",
		},
	}})
	if len(gateway.operations) != 2 {
		t.Fatalf("expected targeted runtime notice to add one ping, got %#v", gateway.operations)
	}
	if gateway.operations[1].Kind != feishu.OperationSendText || gateway.operations[1].Text != "需要你回来处理：当前连接状态异常。" {
		t.Fatalf("unexpected transport attention ping: %#v", gateway.operations[1])
	}

	gateway = &recordingGateway{}
	app = New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")
	runtimeNotice := control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:             "attached_instance_transport_degraded",
			Text:             "实例离线。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyTransportDegraded,
			DeliveryDedupKey: "attached_instance_transport_degraded",
		},
	}
	app.handleUIEvents(context.Background(), []control.UIEvent{runtimeNotice, runtimeNotice})
	if len(gateway.operations) != 2 {
		t.Fatalf("expected suppressed same-batch runtime notice not to emit extra ping, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected first runtime notice card, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].Kind != feishu.OperationSendText || gateway.operations[1].Text != "需要你回来处理：当前连接状态异常。" {
		t.Fatalf("expected only one runtime attention ping after unsuppressed notice, got %#v", gateway.operations[1])
	}

	gateway.operations = nil
	app.handleUIEvents(context.Background(), []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:             "surface_resume_failed",
			Text:             "恢复失败。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilySurfaceResume,
			DeliveryDedupKey: "surface_resume_failed",
		},
	}})
	if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected non-targeted runtime notice to skip attention ping, got %#v", gateway.operations)
	}
}

func TestHandleUIEventsSkipsAttentionPingWithoutActorIdentity(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "")

	app.handleUIEvents(context.Background(), []control.UIEvent{{
		Kind:             control.UIEventFeishuRequestView,
		SurfaceSessionID: "surface-1",
		FeishuRequestView: &control.FeishuRequestView{
			RequestID:       "req-1",
			RequestType:     "approval",
			RequestRevision: 1,
			Title:           "需要确认",
			Options: []control.RequestPromptOption{{
				OptionID: "accept",
				Label:    "允许执行",
			}},
		},
	}})

	if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected original request card without attention ping when actor missing, got %#v", gateway.operations)
	}
}
