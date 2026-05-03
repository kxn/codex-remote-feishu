package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestClassifyInboundActionMarksOldMessage(t *testing.T) {
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	action := app.classifyInboundAction(control.Action{
		Inbound: &control.ActionInboundMeta{
			MessageCreateTime: startedAt.Add(-3 * time.Minute),
		},
	})

	if action.Inbound == nil || action.Inbound.LifecycleVerdict != control.InboundLifecycleOld || action.Inbound.LifecycleReason != "message_before_start_window" {
		t.Fatalf("expected old message verdict, got %#v", action.Inbound)
	}
}

func TestClassifyInboundActionKeepsBoundaryMessageCurrent(t *testing.T) {
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	action := app.classifyInboundAction(control.Action{
		Inbound: &control.ActionInboundMeta{
			MessageCreateTime: startedAt.Add(-90 * time.Second),
		},
	})

	if action.Inbound == nil || action.Inbound.LifecycleVerdict != control.InboundLifecycleCurrent || action.Inbound.LifecycleReason != "" {
		t.Fatalf("expected current message verdict, got %#v", action.Inbound)
	}
}

func TestClassifyInboundActionMarksOldMenu(t *testing.T) {
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	action := app.classifyInboundAction(control.Action{
		Inbound: &control.ActionInboundMeta{
			MenuClickTime: startedAt.Add(-5 * time.Minute),
		},
	})

	if action.Inbound == nil || action.Inbound.LifecycleVerdict != control.InboundLifecycleOld || action.Inbound.LifecycleReason != "menu_before_start_window" {
		t.Fatalf("expected old menu verdict, got %#v", action.Inbound)
	}
}

func TestClassifyInboundActionMarksOldCard(t *testing.T) {
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	action := app.classifyInboundAction(control.Action{
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "older-life",
		},
	})

	if action.Inbound == nil || action.Inbound.LifecycleVerdict != control.InboundLifecycleOldCard || action.Inbound.LifecycleReason != "card_lifecycle_mismatch" {
		t.Fatalf("expected old card verdict, got %#v", action.Inbound)
	}
}

func TestClassifyInboundActionRejectsUnstampedFeishuUICallback(t *testing.T) {
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", nil, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	action := app.classifyInboundAction(control.Action{
		Kind: control.ActionShowCommandMenu,
		Text: "/menu send_settings",
		Inbound: &control.ActionInboundMeta{
			CardCallback: true,
		},
	})

	if action.Inbound == nil || action.Inbound.LifecycleVerdict != control.InboundLifecycleOldCard || action.Inbound.LifecycleReason != "card_callback_missing_lifecycle" {
		t.Fatalf("expected unstamped feishu-ui callback to be rejected as old card, got %#v", action.Inbound)
	}
}

func TestRejectedInboundActionDetailShowsCommandText(t *testing.T) {
	got := rejectedInboundActionDetail(control.Action{
		Kind: control.ActionDetach,
		Text: "/detach",
	})
	if got != "命令“/detach”" {
		t.Fatalf("rejectedInboundActionDetail() = %q, want %q", got, "命令“/detach”")
	}
}

func TestRejectedInboundActionDetailShowsMenuFallback(t *testing.T) {
	got := rejectedInboundActionDetail(control.Action{
		Kind: control.ActionStop,
	})
	if got != "停止（对应 /stop）" {
		t.Fatalf("rejectedInboundActionDetail() = %q, want %q", got, "停止（对应 /stop）")
	}
}

func TestRejectedInboundActionDetailUsesIntentLabelForWorkspaceListNavigation(t *testing.T) {
	got := rejectedInboundActionDetail(control.Action{
		Kind: control.ActionShowAllWorkspaces,
	})
	if got != "查看工作区列表（对应 /list）" {
		t.Fatalf("rejectedInboundActionDetail() = %q, want %q", got, "查看工作区列表（对应 /list）")
	}
}

func TestRejectedInboundActionDetailUsesIntentLabelForWorkspaceThreadExpansion(t *testing.T) {
	got := rejectedInboundActionDetail(control.Action{
		Kind:         control.ActionShowWorkspaceThreads,
		WorkspaceKey: "/data/dl/web",
	})
	if got != "展开该工作区下的会话列表" {
		t.Fatalf("rejectedInboundActionDetail() = %q, want %q", got, "展开该工作区下的会话列表")
	}
}

func TestRejectedInboundActionDetailShowsMessagePreview(t *testing.T) {
	got := rejectedInboundActionDetail(control.Action{
		Kind: control.ActionTextMessage,
		Text: "  这是  一条\n重启前的旧消息  ",
	})
	if got != "消息“这是 一条 重启前的旧消息”" {
		t.Fatalf("rejectedInboundActionDetail() = %q, want %q", got, "消息“这是 一条 重启前的旧消息”")
	}
}
