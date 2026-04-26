package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestHandleUIEventsAddsAttentionToRequestOncePerRevision(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	requestEvent := eventcontract.Event{
		Kind:             eventcontract.KindRequest,
		SurfaceSessionID: "surface-1",
		RequestView: &control.FeishuRequestView{
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

	app.handleUIEvents(context.Background(), []eventcontract.Event{requestEvent})
	app.handleUIEvents(context.Background(), []eventcontract.Event{requestEvent})

	if len(gateway.operations) != 2 {
		t.Fatalf("expected request card with attention + rerender without attention, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected first operation to be request card, got %#v", gateway.operations[0])
	}
	if gateway.operations[0].AttentionUserID != "ou-user-1" || gateway.operations[0].AttentionText != "需要你回来处理：请确认这条请求。" {
		t.Fatalf("expected first request card to carry attention, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].Kind != feishu.OperationSendCard {
		t.Fatalf("expected rerender to keep original request card only, got %#v", gateway.operations[1])
	}
	if gateway.operations[1].AttentionUserID != "" || gateway.operations[1].AttentionText != "" {
		t.Fatalf("expected rerendered request card to skip duplicate attention, got %#v", gateway.operations[1])
	}
}

func TestHandleUIEventsAddsAttentionToPayloadFirstRequest(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	requestEvent := eventcontract.Event{
		SurfaceSessionID: "surface-1",
		Payload: eventcontract.RequestPayload{
			View: control.FeishuRequestView{
				RequestID:       "req-payload-1",
				RequestType:     "approval",
				RequestRevision: 1,
				Title:           "需要确认",
				Options: []control.RequestPromptOption{{
					OptionID: "accept",
					Label:    "允许执行",
				}},
			},
		},
	}

	app.handleUIEvents(context.Background(), []eventcontract.Event{requestEvent})

	if len(gateway.operations) != 1 {
		t.Fatalf("expected payload-first request card with attention, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].AttentionText != "需要你回来处理：请确认这条请求。" {
		t.Fatalf("unexpected payload-first request attention: %#v", gateway.operations[0])
	}
}

func TestHandleUIEventsUsesSemanticAttentionForApprovalCommand(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	requestEvent := eventcontract.Event{
		Kind:             eventcontract.KindRequest,
		SurfaceSessionID: "surface-1",
		RequestView: &control.FeishuRequestView{
			RequestID:       "req-cmd-1",
			RequestType:     "approval",
			SemanticKind:    control.RequestSemanticApprovalCommand,
			RequestRevision: 1,
			Title:           "需要确认执行命令",
			Options: []control.RequestPromptOption{{
				OptionID: "accept",
				Label:    "允许执行",
			}},
		},
	}

	app.handleUIEvents(context.Background(), []eventcontract.Event{requestEvent})

	if len(gateway.operations) != 1 || gateway.operations[0].AttentionText != "需要你回来处理：请确认是否执行命令。" {
		t.Fatalf("expected semantic approval attention text, got %#v", gateway.operations)
	}
}

func TestHandleUIEventsRetriesRequestAttentionAfterAnchorDeliveryFailure(t *testing.T) {
	gateway := &flakyGateway{failures: 1}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	requestEvent := eventcontract.Event{
		Kind:             eventcontract.KindRequest,
		SurfaceSessionID: "surface-1",
		RequestView: &control.FeishuRequestView{
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

	app.handleUIEvents(context.Background(), []eventcontract.Event{requestEvent})

	if len(gateway.operations) != 0 {
		t.Fatalf("expected failed request delivery not to emit orphan attention ping, got %#v", gateway.operations)
	}
	if got := len(app.feishuRuntime.attentionRequests); got != 0 {
		t.Fatalf("expected failed request delivery not to consume dedupe state, got %#v", app.feishuRuntime.attentionRequests)
	}

	delete(app.pendingGlobalRuntimeNotices, "surface-1")
	app.handleUIEvents(context.Background(), []eventcontract.Event{requestEvent})

	if len(gateway.operations) != 1 {
		t.Fatalf("expected successful retry to deliver request card with attention, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected retried request card first, got %#v", gateway.operations[0])
	}
	if gateway.operations[0].AttentionText != "需要你回来处理：请确认这条请求。" {
		t.Fatalf("expected retried attention after successful request delivery, got %#v", gateway.operations[0])
	}
	if got := len(app.feishuRuntime.attentionRequests); got != 1 {
		t.Fatalf("expected successful request attention to record dedupe state, got %#v", app.feishuRuntime.attentionRequests)
	}
}

func TestHandleUIEventsMergesFinalAndPlanProposalIntoOneAttentionAnchor(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []eventcontract.Event{
		{
			Kind:             eventcontract.KindBlockCommitted,
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
			Kind:             eventcontract.KindPage,
			SurfaceSessionID: "surface-1",
			PageView: &control.FeishuPageView{
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

	if len(gateway.operations) != 2 {
		t.Fatalf("expected final + plan with attention on plan card, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].ReplyToMessageID != "om-source-1" {
		t.Fatalf("expected final reply card first, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].Kind != feishu.OperationSendCard || gateway.operations[1].ReplyToMessageID != "" {
		t.Fatalf("expected plan proposal card second, got %#v", gateway.operations[1])
	}
	if gateway.operations[0].AttentionText != "" || gateway.operations[0].AttentionUserID != "" {
		t.Fatalf("expected final reply card to stay unmentioned when plan proposal exists, got %#v", gateway.operations[0])
	}
	if gateway.operations[1].AttentionText != "需要你回来处理：本轮执行已结束，并生成了提案计划。" {
		t.Fatalf("unexpected plan attention text: %#v", gateway.operations[1])
	}
}

func TestHandleUIEventsRecognizesPayloadFirstPlanProposal(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []eventcontract.Event{
		{
			SurfaceSessionID: "surface-1",
			Payload: eventcontract.BlockCommittedPayload{
				Block: render.Block{
					Kind:        render.BlockAssistantMarkdown,
					Text:        "已完成修改。",
					ThreadID:    "thread-1",
					ThreadTitle: "droid · 修复登录流程",
					ThemeKey:    "thread-1",
					Final:       true,
				},
			},
		},
		{
			SurfaceSessionID: "surface-1",
			Payload: eventcontract.PagePayload{
				View: control.FeishuPageView{
					CommandID: control.FeishuCommandPlan,
					Title:     "提案计划",
				},
			},
		},
	})

	if len(gateway.operations) != 2 {
		t.Fatalf("expected payload-first final + plan with attention on plan card, got %#v", gateway.operations)
	}
	if gateway.operations[1].Kind != feishu.OperationSendCard || gateway.operations[1].AttentionText != "需要你回来处理：本轮执行已结束，并生成了提案计划。" {
		t.Fatalf("unexpected payload-first plan attention: %#v", gateway.operations[1])
	}
}

func TestHandleUIEventsUsesFailureAttentionWhenTurnFails(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []eventcontract.Event{
		{
			Kind:             eventcontract.KindBlockCommitted,
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
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: "surface-1",
			Notice: &control.Notice{
				Code: "turn_failed",
				Text: "stream disconnected before completion",
			},
		},
	})

	if len(gateway.operations) != 2 {
		t.Fatalf("expected final card + failure notice with attention, got %#v", gateway.operations)
	}
	if gateway.operations[1].Kind != feishu.OperationSendCard || gateway.operations[1].ReplyToMessageID != "" {
		t.Fatalf("expected failure attention to stay on top-level notice card, got %#v", gateway.operations[1])
	}
	if gateway.operations[1].AttentionText != "需要你回来处理：本轮执行已停止。" {
		t.Fatalf("unexpected failure attention text: %#v", gateway.operations[1])
	}
}

func TestHandleUIEventsAddsAttentionOnlyForTargetedGlobalRuntimeNotices(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")

	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:             "attached_instance_transport_degraded",
			Text:             "实例离线。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyTransportDegraded,
			DeliveryDedupKey: "attached_instance_transport_degraded",
		},
	}})
	if len(gateway.operations) != 1 {
		t.Fatalf("expected targeted runtime notice to stay one card with attention, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].AttentionText != "需要你回来处理：当前连接状态异常。" {
		t.Fatalf("unexpected transport attention: %#v", gateway.operations[0])
	}

	gateway = &recordingGateway{}
	app = New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "ou-user-1")
	runtimeNotice := eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: "surface-1",
		Notice: &control.Notice{
			Code:             "attached_instance_transport_degraded",
			Text:             "实例离线。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyTransportDegraded,
			DeliveryDedupKey: "attached_instance_transport_degraded",
		},
	}
	app.handleUIEvents(context.Background(), []eventcontract.Event{runtimeNotice, runtimeNotice})
	if len(gateway.operations) != 1 {
		t.Fatalf("expected suppressed same-batch runtime notice not to emit extra card, got %#v", gateway.operations)
	}
	if gateway.operations[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected first runtime notice card, got %#v", gateway.operations[0])
	}
	if gateway.operations[0].AttentionText != "需要你回来处理：当前连接状态异常。" {
		t.Fatalf("expected only one runtime attention after unsuppressed notice, got %#v", gateway.operations[0])
	}

	gateway.operations = nil
	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
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
		t.Fatalf("expected non-targeted runtime notice to skip attention, got %#v", gateway.operations)
	}
}

func TestHandleUIEventsSkipsAttentionWithoutActorIdentity(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "")

	app.handleUIEvents(context.Background(), []eventcontract.Event{{
		Kind:             eventcontract.KindRequest,
		SurfaceSessionID: "surface-1",
		RequestView: &control.FeishuRequestView{
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
		t.Fatalf("expected original request card without attention when actor missing, got %#v", gateway.operations)
	}
	if gateway.operations[0].AttentionText != "" || gateway.operations[0].AttentionUserID != "" {
		t.Fatalf("expected original request card to stay unmentioned when actor missing, got %#v", gateway.operations[0])
	}
}
