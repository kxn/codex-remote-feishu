package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestNormalModeAttachClaimsWorkspaceAndProjectsSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected claimed workspace key, got %#v", surface)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.WorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected snapshot workspace key, got %#v", snapshot)
	}
}

func TestWorkspaceAttachProjectsUnboundSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 2, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.ProductMode != "normal" || snapshot.WorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected normal-mode workspace snapshot, got %#v", snapshot)
	}
	if snapshot.Attachment.InstanceID != "inst-1" || snapshot.Attachment.SelectedThreadID != "" || snapshot.Attachment.RouteMode != string(state.RouteModeUnbound) {
		t.Fatalf("expected attached-unbound snapshot, got %#v", snapshot.Attachment)
	}
}

func TestHeadlessAttachInstanceCallbackIsRejected(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 3, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-vscode",
		DisplayName:             "vscode",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "旧 VS Code 焦点", CWD: "/data/dl/droid"},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode",
		Inbound:          &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "life-1"},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound || surface.ClaimedWorkspaceKey != "" {
		t.Fatalf("expected headless attach_instance callback to leave surface detached, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "attach_instance_headless_rejected" {
		t.Fatalf("expected headless attach_instance rejection notice, got %#v", events)
	}
}

func TestHeadlessAttachInstanceCallbackDoesNotOverrideWorkspaceClaim(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 3, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-headless": {ThreadID: "thread-headless", Name: "当前工作区会话", CWD: "/data/dl/droid"},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-vscode",
		DisplayName:             "vscode",
		WorkspaceRoot:           "/data/dl/other",
		WorkspaceKey:            "/data/dl/other",
		ShortName:               "other",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-vscode",
		Threads: map[string]*state.ThreadRecord{
			"thread-vscode": {ThreadID: "thread-vscode", Name: "旧 VS Code 焦点", CWD: "/data/dl/other"},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode",
		Inbound:          &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "life-1"},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-headless" || surface.ClaimedWorkspaceKey != "/data/dl/droid" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected old instance callback to preserve current workspace claim, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "attach_instance_headless_rejected" {
		t.Fatalf("expected headless attach_instance rejection notice, got %#v", events)
	}
}

func TestHeadlessAttachInstanceCallbackPreservesActiveTargetPicker(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 3, 45, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-vscode",
		DisplayName:             "vscode",
		WorkspaceRoot:           "/data/dl/other",
		WorkspaceKey:            "/data/dl/other",
		ShortName:               "other",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-vscode",
		Threads: map[string]*state.ThreadRecord{
			"thread-vscode": {ThreadID: "thread-vscode", Name: "旧 VS Code 焦点", CWD: "/data/dl/other"},
		},
	})
	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	picker := &activeTargetPickerRecord{
		PickerID:             "picker-1",
		OwnerUserID:          "user-1",
		Source:               control.TargetPickerRequestSourceList,
		Stage:                control.FeishuTargetPickerStageEditing,
		Page:                 control.FeishuTargetPickerPageTarget,
		SelectedWorkspaceKey: "/data/dl/current",
	}
	flow := newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, picker.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseEditing)
	svc.setActiveTargetPicker(surface, picker)
	svc.setActiveOwnerCardFlow(surface, flow)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode",
		Inbound:          &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "life-1"},
	})

	if got := svc.activeTargetPicker(surface); got != picker {
		t.Fatalf("expected old instance callback to preserve active target picker, got %#v", got)
	}
	if got := svc.activeOwnerCardFlow(surface); got != flow {
		t.Fatalf("expected old instance callback to preserve active owner-card flow, got %#v", got)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "attach_instance_headless_rejected" {
		t.Fatalf("expected headless attach_instance rejection notice, got %#v", events)
	}
}

func TestOldVSCodeAttachInstanceCardIsRejectedAfterSwitchingToHeadless(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 4, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-vscode",
		DisplayName:             "vscode",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "旧 VS Code 焦点", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode codex",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode",
		Inbound:          &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "life-1"},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeNormal || surface.Backend != "codex" || surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected old VS Code instance card to leave switched headless surface detached, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "attach_instance_headless_rejected" {
		t.Fatalf("expected headless attach_instance rejection notice, got %#v", events)
	}
}

func TestAttachWorkspaceCanonicalizesWorkspaceKey(t *testing.T) {
	now := time.Date(2026, 4, 9, 13, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/work/../droid/",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     " /data/dl/droid/./ ",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected materialized surface")
	}
	if surface.AttachedInstanceID != "inst-1" || surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected canonical workspace attach, got %#v", surface)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.WorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected canonical workspace key in snapshot, got %#v", snapshot)
	}
	if len(snapshot.Instances) != 1 || snapshot.Instances[0].WorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected canonical instance workspace key in snapshot, got %#v", snapshot)
	}
}

func TestNormalModeAttachRejectsBusyWorkspaceAcrossInstances(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid-a",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-a",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "droid-b",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-b",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "workspace_busy" {
		t.Fatalf("expected workspace_busy notice, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-2"]; surface.AttachedInstanceID != "" || surface.ClaimedWorkspaceKey != "" {
		t.Fatalf("expected second surface to remain detached without workspace claim, got %#v", surface)
	}
}

func TestVSCodeModeDoesNotUseWorkspaceClaimForAttach(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid-a",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-a",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "droid-b",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid-b",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-2", ChatID: "chat-2", ActorUserID: "user-2", Text: "/mode vscode"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})

	surface := svc.root.Surfaces["surface-2"]
	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected vscode mode, got %#v", surface)
	}
	if surface.AttachedInstanceID != "inst-2" || surface.ClaimedWorkspaceKey != "" {
		t.Fatalf("expected vscode attach without workspace claim, got %#v", surface)
	}
	if surface.SelectedThreadID != "thread-2" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected vscode attach to land in follow-local on observed thread, got %#v", surface)
	}
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Code != "attached" {
		t.Fatalf("expected attach success, got %#v", events)
	}
}

func TestVSCodeAttachWaitsWithoutObservedFocusedThread(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 12, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "已知会话", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected vscode attach to enter follow waiting, got %#v", surface)
	}
	var sawWaitingSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "" && event.ThreadSelection.RouteMode == string(state.RouteModeFollowLocal) {
			sawWaitingSelection = true
		}
	}
	if !sawWaitingSelection {
		t.Fatalf("expected attach to publish follow waiting selection, got %#v", events)
	}
	if len(events) == 0 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "已进入跟随模式") {
		t.Fatalf("expected attach waiting notice, got %#v", events)
	}
}

func TestVSCodeFollowWaitingTextGuidesVSCodeOrUse(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 12, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "直接发消息",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "follow_waiting" {
		t.Fatalf("expected follow waiting notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "VS Code") || !strings.Contains(events[0].Notice.Text, "/use") {
		t.Fatalf("expected follow waiting guidance to mention VS Code and /use, got %#v", events[0].Notice)
	}
}
