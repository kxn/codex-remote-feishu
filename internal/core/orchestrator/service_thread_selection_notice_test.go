package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestThreadSelectionEventsEmitNoticeFamilyEvent(t *testing.T) {
	now := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "修复登录流程",
				CWD:      "/data/dl/droid",
			},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	surface := svc.root.Surfaces["surface-1"]

	events := svc.threadSelectionEvents(surface, "thread-1", string(state.RouteModePinned), "droid · 修复登录流程")
	if len(events) != 1 {
		t.Fatalf("expected one selection event, got %#v", events)
	}
	event := events[0]
	if event.Kind != eventcontract.KindNotice {
		t.Fatalf("expected notice-family event, got %#v", event)
	}
	if event.ThreadSelection == nil || event.ThreadSelection.ThreadID != "thread-1" {
		t.Fatalf("expected compatibility thread selection metadata, got %#v", event.ThreadSelection)
	}
	if event.Notice == nil || event.Notice.Code != "thread_selection_changed" {
		t.Fatalf("expected thread-selection notice, got %#v", event.Notice)
	}
	if len(event.Notice.Sections) != 1 {
		t.Fatalf("expected title-only section, got %#v", event.Notice.Sections)
	}
	if event.Notice.Sections[0].Label != "当前输入目标" || event.Notice.Sections[0].Lines[0] != "droid · 修复登录流程" {
		t.Fatalf("unexpected first section: %#v", event.Notice.Sections[0])
	}
}

func TestThreadSelectionEventsEmitNewThreadReadyNoticeFamilyEvent(t *testing.T) {
	now := time.Date(2026, 4, 21, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	surface := svc.root.Surfaces["surface-1"]

	events := svc.threadSelectionEvents(surface, "", string(state.RouteModeNewThreadReady), preparedNewThreadSelectionTitle())
	if len(events) != 1 {
		t.Fatalf("expected one selection event, got %#v", events)
	}
	event := events[0]
	if event.Kind != eventcontract.KindNotice {
		t.Fatalf("expected notice-family event, got %#v", event)
	}
	if event.Notice == nil || event.Notice.Code != "thread_selection_changed" {
		t.Fatalf("expected thread-selection notice, got %#v", event.Notice)
	}
	if len(event.Notice.Sections) != 1 {
		t.Fatalf("expected one status section, got %#v", event.Notice.Sections)
	}
	section := event.Notice.Sections[0]
	if section.Label != "当前状态" {
		t.Fatalf("unexpected section label: %#v", section)
	}
	if len(section.Lines) != 2 || section.Lines[0] != "已准备新建会话。" {
		t.Fatalf("unexpected status lines: %#v", section.Lines)
	}
}

func TestThreadSelectionEventsDoNotTreatPreviewAsRecentReply(t *testing.T) {
	now := time.Date(2026, 4, 21, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:             "thread-1",
				Name:                 "修复登录流程",
				LastUserMessage:      "用户自己的首条消息预览",
				LastAssistantMessage: "",
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	surface := svc.root.Surfaces["surface-1"]

	events := svc.threadSelectionEvents(surface, "thread-1", string(state.RouteModePinned), "droid · 修复登录流程")
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected one notice-family event, got %#v", events)
	}
	for _, section := range events[0].Notice.Sections {
		if section.Label == "最近回复" {
			t.Fatalf("expected no recent-reply section without assistant message, got %#v", events[0].Notice.Sections)
		}
	}
	if len(events[0].Notice.Sections) < 2 {
		t.Fatalf("expected title and recent-user sections, got %#v", events[0].Notice.Sections)
	}
}
