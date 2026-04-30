package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeModeListUsesHeadlessWorkspacePicker(t *testing.T) {
	now := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-claude-1",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-claude",
		ChatID:           "chat-claude",
		ActorUserID:      "user-claude",
		Text:             "/mode claude",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-claude",
		ChatID:           "chat-claude",
		ActorUserID:      "user-claude",
	})

	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected headless /list to open target picker in claude mode, got %#v", events)
	}
	view := events[0].TargetPickerView
	if view.Source != control.TargetPickerRequestSourceList {
		t.Fatalf("expected claude /list to keep headless source, got %#v", view)
	}
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/repo"); !ok {
		t.Fatalf("expected claude /list to include workspace option, got %#v", view.WorkspaceOptions)
	}
}

func TestVSCodeModeListFiltersOutHeadlessInstances(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/runtime/headless",
		WorkspaceKey:  "/data/dl/runtime/headless",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	view := selectionViewFromEvent(t, events[0])
	ctx := selectionContextFromEvent(t, events[0])
	if view.Instance == nil || ctx.Title != "在线 VS Code 实例" || ctx.Layout != "vscode_instance_list" {
		t.Fatalf("expected vscode attach selection view metadata, got view=%#v ctx=%#v", view, ctx)
	}
	if len(view.Instance.Entries) != 1 || view.Instance.Entries[0].InstanceID != "inst-vscode-1" {
		t.Fatalf("expected only vscode instance in list view, got %#v", view.Instance.Entries)
	}
	if view.Instance.Entries[0].MetaText != "等待 VS Code 焦点" {
		t.Fatalf("expected vscode instance view to show compact waiting meta, got %#v", view.Instance.Entries[0])
	}
}

func TestVSCodeModeListShowsCurrentInstanceSummaryAndFocusSortedCandidates(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-main")
	materializeVSCodeSurfaceForTest(svc, "surface-busy")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-current",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-current",
		Threads: map[string]*state.ThreadRecord{
			"thread-current": {ThreadID: "thread-current", Name: "当前实例会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(-5 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-focus",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-focus",
		Threads: map[string]*state.ThreadRecord{
			"thread-focus": {ThreadID: "thread-focus", Name: "当前焦点线程", CWD: "/data/dl/web", LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-wait",
		DisplayName:   "admin",
		WorkspaceRoot: "/data/dl/admin",
		WorkspaceKey:  "/data/dl/admin",
		ShortName:     "admin",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-wait": {ThreadID: "thread-wait", Name: "旧会话", CWD: "/data/dl/admin", LastUsedAt: now.Add(-1 * time.Hour)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-busy",
		DisplayName:   "ops",
		WorkspaceRoot: "/data/dl/ops",
		WorkspaceKey:  "/data/dl/ops",
		ShortName:     "ops",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-busy": {ThreadID: "thread-busy", Name: "值班处理", CWD: "/data/dl/ops", LastUsedAt: now.Add(-30 * time.Minute)},
		},
	})

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-main", ChatID: "chat-main", ActorUserID: "user-main", InstanceID: "inst-current"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-busy", ChatID: "chat-busy", ActorUserID: "user-busy", InstanceID: "inst-busy"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-main",
		ChatID:           "chat-main",
		ActorUserID:      "user-main",
	})

	if len(events) != 1 {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	view := selectionViewFromEvent(t, events[0])
	ctx := selectionContextFromEvent(t, events[0])
	if view.Instance == nil || ctx.Layout != "vscode_instance_list" || ctx.ContextTitle != "当前实例" {
		t.Fatalf("unexpected vscode instance view metadata: view=%#v ctx=%#v", view, ctx)
	}
	if !strings.Contains(ctx.ContextText, "droid · 当前跟随中") || !strings.Contains(ctx.ContextText, "换实例才用 /list") {
		t.Fatalf("expected current instance summary, got %#v", ctx.ContextText)
	}
	if view.Instance.Current == nil || !strings.Contains(view.Instance.Current.ContextText, "droid · 当前跟随中") {
		t.Fatalf("expected current instance summary in structured view, got %#v", view.Instance.Current)
	}
	if len(view.Instance.Entries) != 3 {
		t.Fatalf("expected current instance to be summarized instead of listed, got %#v", view.Instance.Entries)
	}
	if view.Instance.Entries[0].InstanceID != "inst-focus" || view.Instance.Entries[0].ButtonLabel != "切换" || view.Instance.Entries[0].MetaText != "2分前 · 当前焦点可跟随" {
		t.Fatalf("expected focused candidate first, got %#v", view.Instance.Entries[0])
	}
	if view.Instance.Entries[1].InstanceID != "inst-wait" || view.Instance.Entries[1].MetaText != "1小时前 · 等待 VS Code 焦点" {
		t.Fatalf("expected waiting candidate after focused one, got %#v", view.Instance.Entries[1])
	}
	if view.Instance.Entries[2].InstanceID != "inst-busy" || !view.Instance.Entries[2].Disabled || view.Instance.Entries[2].MetaText != "30分前 · 当前被其他飞书会话接管" {
		t.Fatalf("expected busy instance in unavailable section, got %#v", view.Instance.Entries[2])
	}
}

func TestVSCodeModeListBuildsStructuredInstanceSelectionView(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-main")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-current",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-current",
		Threads: map[string]*state.ThreadRecord{
			"thread-current": {ThreadID: "thread-current", Name: "当前实例会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(-5 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-focus",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-focus",
		Threads: map[string]*state.ThreadRecord{
			"thread-focus": {ThreadID: "thread-focus", Name: "当前焦点线程", CWD: "/data/dl/web", LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-main", ChatID: "chat-main", ActorUserID: "user-main", InstanceID: "inst-current"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-main",
		ChatID:           "chat-main",
		ActorUserID:      "user-main",
	})

	if len(events) != 1 {
		t.Fatalf("expected one selection view, got %#v", events)
	}
	view := selectionViewFromEvent(t, events[0])
	if view.Instance == nil {
		t.Fatalf("expected structured instance selection view, got %#v", view)
	}
	if view.Instance.Current == nil || !strings.Contains(view.Instance.Current.ContextText, "droid · 当前跟随中") {
		t.Fatalf("expected current instance summary in structured view, got %#v", view.Instance.Current)
	}
	if len(view.Instance.Entries) != 1 || view.Instance.Entries[0].InstanceID != "inst-focus" {
		t.Fatalf("expected only non-current candidates in structured view, got %#v", view.Instance.Entries)
	}
}
