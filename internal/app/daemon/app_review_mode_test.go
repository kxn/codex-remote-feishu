package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newReviewModeAppForTest(t *testing.T) (*App, *messageIDAssigningGateway) {
	t.Helper()
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       7,
		StartedAt: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {
				ThreadID: "thread-main",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
			"thread-review": {
				ThreadID: "thread-review",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})
	materializeAttachedSurfaceForFinalCardTest(app, "surface-1", "app-1", "chat-1", "user-1", "inst-1", "/data/dl/droid")
	return app, gateway
}

func TestDeliverUIEventDoesNotAddReviewEntryButtonToNormalFinalCardWithoutFileChanges(t *testing.T) {
	app, gateway := newReviewModeAppForTest(t)

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver final block: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if ops[0].CardTitle != "✅ 最后答复" {
		t.Fatalf("unexpected normal final title: %#v", ops[0])
	}
	if operationHasActionValue(ops[0], "page_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("did not expect review entry button without file changes, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventAddsReviewEntryButtonWhenFinalCardIncludesFileChanges(t *testing.T) {
	app, gateway := newReviewModeAppForTest(t)

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
		FileChangeSummary: &control.FileChangeSummary{
			FileCount:    1,
			AddedLines:   1,
			RemovedLines: 0,
			Files: []control.FileChangeSummaryEntry{
				{Path: "docs/guide.md", AddedLines: 1},
			},
		},
	})
	if err != nil {
		t.Fatalf("deliver final block with file changes: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if !operationHasActionValue(ops[0], "page_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("expected review entry button with file changes, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventMarksReviewFinalCardAndAddsExitButtons(t *testing.T) {
	app, gateway := newReviewModeAppForTest(t)
	app.service.Surfaces()[0].ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		TargetLabel:    "未提交变更",
	}

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-final-main-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-review",
			TurnID:     "turn-review-1",
			ItemID:     "item-review-1",
			Text:       "建议先补一条测试。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver review final block: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one review final card, got %#v", ops)
	}
	if !strings.HasPrefix(ops[0].CardTitle, reviewCardTitlePrefix) {
		t.Fatalf("expected review title prefix, got %#v", ops[0])
	}
	if !operationHasActionValue(ops[0], "page_action", "action_kind", string(control.ActionReviewDiscard)) {
		t.Fatalf("expected discard button, got %#v", ops[0].CardElements)
	}
	if !operationHasActionValue(ops[0], "page_action", "action_kind", string(control.ActionReviewApply)) {
		t.Fatalf("expected apply button, got %#v", ops[0].CardElements)
	}
	if operationHasActionValue(ops[0], "page_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("did not expect review entry button on review final card, got %#v", ops[0].CardElements)
	}
}

func TestHandleGatewayActionStartsDetachedReviewFromFinalCard(t *testing.T) {
	app, gateway := newReviewModeAppForTest(t)
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}
	app.service.RecordFinalCardMessage("surface-1", render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		TurnID:     "turn-main-1",
		ItemID:     "item-main-1",
		Text:       "已经处理完成。",
		Final:      true,
	}, "msg-1", "om-final-1", app.daemonLifecycleID)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionReviewStart,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-final-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil || result.ReplaceCurrentCard.CardTitle != "正在进入审阅" {
		t.Fatalf("expected inline entering-review card, got %#v", result)
	}
	if len(gateway.snapshotOperations()) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.snapshotOperations())
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandReviewStart || sent[0].Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start command: %#v", sent)
	}
}

func TestHandleGatewayActionAppliesReviewResultBackToParentThread(t *testing.T) {
	app, _ := newReviewModeAppForTest(t)
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}
	surface := app.service.Surfaces()[0]
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "建议先补一条 review 回归测试。",
	}

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionReviewApply,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-review-final-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil || result.ReplaceCurrentCard.CardTitle != "正在继续修改" {
		t.Fatalf("expected inline continue-review card, got %#v", result)
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandPromptSend {
		t.Fatalf("unexpected apply-review command: %#v", sent)
	}
	if sent[0].Target.ThreadID != "thread-main" || sent[0].Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting {
		t.Fatalf("unexpected apply-review target: %#v", sent[0].Target)
	}
	if len(sent[0].Prompt.Inputs) != 1 || sent[0].Prompt.Inputs[0].Text != "请根据以下审阅意见继续修改：\n\n建议先补一条 review 回归测试。" {
		t.Fatalf("unexpected apply-review prompt: %#v", sent[0].Prompt)
	}
	if app.service.ReviewSession("surface-1") != nil {
		t.Fatalf("expected review session to clear after continue")
	}
}

func TestHandleGatewayActionRejectsExpiredReviewCard(t *testing.T) {
	app, gateway := newReviewModeAppForTest(t)
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		t.Fatalf("did not expect command dispatch for expired review card: %#v", command)
		return nil
	}

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionReviewStart,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-final-old-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "old-daemon",
		},
	})

	if result != nil {
		t.Fatalf("expected expired review card to bypass inline result, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != "旧卡片已过期" {
		t.Fatalf("expected old-card rejection notice, got %#v", ops)
	}
}
