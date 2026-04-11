package orchestrator

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	feishuadapter "github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestLocalPauseWithoutQueuedMessagesDoesNotEmitResumeNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	if len(first) != 1 || first[0].Notice == nil || first[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(second) != 0 {
		t.Fatalf("expected no handoff events when queue is empty, got %#v", second)
	}
	if svc.root.Surfaces["surface-1"].DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected surface to return directly to normal mode, got %q", svc.root.Surfaces["surface-1"].DispatchMode)
	}

	now = now.Add(2 * time.Second)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no delayed resume notice with empty queue, got %#v", tick)
	}
}

func TestDisplayThreadTitleDisambiguatesDuplicateTitles(t *testing.T) {
	inst := &state.InstanceRecord{
		DisplayName:   "dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		WorkspaceRoot: "/data/dl",
		Threads: map[string]*state.ThreadRecord{
			"019d56f0-de5e-7943-bc9a-18c42ef11acb": {ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", Name: "新会话", CWD: "/data/dl"},
			"019d56f0-e48d-7e51-be84-04a5658e4c96": {ThreadID: "019d56f0-e48d-7e51-be84-04a5658e4c96", Name: "新会话", CWD: "/data/dl"},
		},
	}

	first := displayThreadTitle(inst, inst.Threads["019d56f0-de5e-7943-bc9a-18c42ef11acb"], "019d56f0-de5e-7943-bc9a-18c42ef11acb")
	second := displayThreadTitle(inst, inst.Threads["019d56f0-e48d-7e51-be84-04a5658e4c96"], "019d56f0-e48d-7e51-be84-04a5658e4c96")
	if first == second {
		t.Fatalf("expected duplicate thread titles to be disambiguated, got %q and %q", first, second)
	}
	if !strings.Contains(first, "de5e…1acb") || !strings.Contains(second, "e48d…4c96") {
		t.Fatalf("expected disambiguated titles to include short ids, got %q and %q", first, second)
	}
}

func TestThreadTitleFallsBackToPreviewSummary(t *testing.T) {
	title := threadTitle(&state.InstanceRecord{
		DisplayName:  "droid",
		WorkspaceKey: "/data/dl/droid",
		ShortName:    "droid",
	}, &state.ThreadRecord{
		ThreadID: "thread-1",
		Preview:  "我先按 atlas 这个工程统计了入口文件和模块边界。",
		CWD:      "/data/dl/droid",
	}, "thread-1")

	if title != "droid · 我先按 atlas 这个工程统计了入口文件和模块边界。" {
		t.Fatalf("unexpected preview-based title: %q", title)
	}
}

func TestPresentThreadSelectionIncludesStableShortIDInSubtitle(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"019d56f0-de5e-7943-bc9a-18c42ef11acb": {ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", Name: "新会话", CWD: "/data/dl"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 1 {
		t.Fatalf("expected one thread selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if len(prompt.Options) != 1 {
		t.Fatalf("expected one thread selection prompt, got %#v", events)
	}
	if prompt.Title != "最近会话" {
		t.Fatalf("expected recent session prompt title, got %#v", prompt)
	}
	if prompt.Options[0].Subtitle != "可接管" {
		t.Fatalf("expected attached /use subtitle to only show handoff status, got %#v", prompt.Options[0])
	}
}

func TestPresentThreadSelectionShowsMostRecentFive(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := &state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	}
	for i := 1; i <= 6; i++ {
		threadID := "thread-" + string(rune('0'+i))
		inst.Threads[threadID] = &state.ThreadRecord{
			ThreadID:   threadID,
			Name:       "会话" + string(rune('0'+i)),
			CWD:        "/data/dl",
			LastUsedAt: now.Add(time.Duration(i) * time.Minute),
			ListOrder:  i,
		}
	}
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if len(prompt.Options) != 6 {
		t.Fatalf("expected recent prompt plus scoped-all button, got %#v", prompt.Options)
	}
	if prompt.Title != "最近会话" || prompt.Hint != "" {
		t.Fatalf("unexpected recent prompt metadata: %#v", prompt)
	}
	if prompt.Options[0].OptionID != "thread-6" || prompt.Options[4].OptionID != "thread-2" {
		t.Fatalf("expected most recent sessions first, got %#v", prompt.Options)
	}
	if prompt.Options[5].ActionKind != "show_scoped_threads" || prompt.Options[5].ButtonLabel != "当前工作区全部会话" {
		t.Fatalf("expected trailing scoped-all action, got %#v", prompt.Options[5])
	}
}

func TestPresentScopedThreadSelectionShowsAllSessionsInCurrentWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := &state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	}
	for i := 1; i <= 6; i++ {
		threadID := "thread-" + string(rune('0'+i))
		inst.Threads[threadID] = &state.ThreadRecord{
			ThreadID:   threadID,
			Name:       "会话" + string(rune('0'+i)),
			CWD:        "/data/dl",
			LastUsedAt: now.Add(time.Duration(i) * time.Minute),
			ListOrder:  i,
		}
	}
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowScopedThreads,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if prompt.Title != "当前工作区全部会话" || len(prompt.Options) != 7 {
		t.Fatalf("expected all current-workspace sessions, got %#v", prompt)
	}
	if prompt.Options[0].OptionID != "thread-6" || prompt.Options[5].OptionID != "thread-1" {
		t.Fatalf("expected scoped-all prompt to keep recency order, got %#v", prompt.Options)
	}
	if prompt.Options[6].ActionKind != "show_threads" || prompt.Options[6].ButtonLabel != "最近会话" {
		t.Fatalf("expected trailing return-to-recent action, got %#v", prompt.Options[6])
	}
}

func TestPresentAllThreadSelectionShowsAllSessionsByRecency(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "较早会话", CWD: "/data/dl", LastUsedAt: now.Add(1 * time.Minute), ListOrder: 2},
			"thread-2": {ThreadID: "thread-2", Name: "最新会话", CWD: "/data/dl", LastUsedAt: now.Add(2 * time.Minute), ListOrder: 1},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if prompt.Title != "全部会话" || prompt.Hint != "" || prompt.Layout != "workspace_grouped_useall" {
		t.Fatalf("unexpected all-session prompt metadata: %#v", prompt)
	}
	if prompt.ContextTitle != "当前工作区" || prompt.ContextKey != "/data/dl" || !strings.Contains(prompt.ContextText, "dl ·") {
		t.Fatalf("expected attached /useall prompt to expose current workspace summary, got %#v", prompt)
	}
	if len(prompt.Options) != 2 || prompt.Options[0].OptionID != "thread-2" || prompt.Options[1].OptionID != "thread-1" {
		t.Fatalf("expected all sessions sorted by recency, got %#v", prompt.Options)
	}
	if prompt.Options[0].GroupKey != "/data/dl" || prompt.Options[0].GroupLabel != "dl" || prompt.Options[0].AgeText == "" {
		t.Fatalf("expected grouped workspace metadata on /useall options, got %#v", prompt.Options[0])
	}
}

func TestPresentAllThreadSelectionLimitsToRecentFiveWorkspaceGroups(t *testing.T) {
	now := time.Date(2026, 4, 11, 5, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 1; i <= 6; i++ {
		workspaceKey := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: workspaceKey,
			WorkspaceKey:  workspaceKey,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        workspaceKey,
					LastUsedAt: now.Add(time.Duration(i) * time.Minute),
				},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if prompt.Title != "全部会话" || prompt.Layout != "workspace_grouped_useall" {
		t.Fatalf("unexpected prompt metadata: %#v", prompt)
	}
	if len(prompt.Options) != 6 {
		t.Fatalf("expected five workspace groups plus expand action, got %#v", prompt.Options)
	}
	for index, want := range []string{"thread-6", "thread-5", "thread-4", "thread-3", "thread-2"} {
		if prompt.Options[index].OptionID != want {
			t.Fatalf("expected recent workspace thread order, got %#v", prompt.Options)
		}
	}
	last := prompt.Options[len(prompt.Options)-1]
	if last.ActionKind != "show_all_thread_workspaces" || last.ButtonLabel != "全部工作区" || !strings.Contains(last.Subtitle, "还有 1 个工作区未显示") {
		t.Fatalf("expected trailing expand action, got %#v", last)
	}
}

func TestBuildThreadSelectionModelKeepsAllWorkspaceGroupsForProjection(t *testing.T) {
	now := time.Date(2026, 4, 11, 5, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 1; i <= 6; i++ {
		workspaceKey := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: workspaceKey,
			WorkspaceKey:  workspaceKey,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        workspaceKey,
					LastUsedAt: now.Add(time.Duration(i) * time.Minute),
				},
			},
		})
	}

	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	model, events := svc.buildThreadSelectionModel(surface, threadSelectionDisplayAll)
	if len(events) != 0 || model == nil {
		t.Fatalf("expected thread selection model, got model=%#v events=%#v", model, events)
	}
	if model.Mode != control.FeishuThreadSelectionNormalGlobalRecent {
		t.Fatalf("expected recent grouped mode for /useall entry view, got %#v", model)
	}
	if len(model.Entries) != 6 {
		t.Fatalf("expected semantic views for all workspace groups, got %#v", model.Entries)
	}
	if model.Entries[0].ThreadID != "thread-6" || model.Entries[0].WorkspaceKey != "/data/dl/proj-6" || !model.Entries[0].AllowCrossWorkspace {
		t.Fatalf("unexpected first semantic thread view: %#v", model.Entries[0])
	}

	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptUseThread,
		Thread:     model,
	}
	prompt, ok := feishuadapter.FeishuDirectSelectionPromptFromView(view, svc.buildFeishuSelectionContextFromView(surface, view))
	if !ok {
		t.Fatalf("expected selection view to be projectable, got %#v", model)
	}
	if len(prompt.Options) != 6 {
		t.Fatalf("expected five visible groups plus expand action, got %#v", prompt.Options)
	}
	if prompt.Options[4].OptionID != "thread-2" {
		t.Fatalf("expected prompt projection to keep only recent five workspace groups, got %#v", prompt.Options)
	}
	last := prompt.Options[len(prompt.Options)-1]
	if last.ActionKind != "show_all_thread_workspaces" || last.ButtonLabel != "全部工作区" {
		t.Fatalf("expected prompt projection to append expand action, got %#v", last)
	}
}

func TestPresentAllThreadWorkspacesShowsAllGroupsAndReturnAction(t *testing.T) {
	now := time.Date(2026, 4, 11, 5, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 1; i <= 6; i++ {
		workspaceKey := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: workspaceKey,
			WorkspaceKey:  workspaceKey,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        workspaceKey,
					LastUsedAt: now.Add(time.Duration(i) * time.Minute),
				},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreadWorkspaces,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if len(prompt.Options) != 7 {
		t.Fatalf("expected six workspace groups plus return action, got %#v", prompt.Options)
	}
	for index, want := range []string{"thread-6", "thread-5", "thread-4", "thread-3", "thread-2", "thread-1"} {
		if prompt.Options[index].OptionID != want {
			t.Fatalf("expected expanded workspace order, got %#v", prompt.Options)
		}
	}
	last := prompt.Options[len(prompt.Options)-1]
	if last.ActionKind != "show_recent_thread_workspaces" || last.ButtonLabel != "最近工作区" || !strings.Contains(last.Subtitle, "回到最近 5 个工作区") {
		t.Fatalf("expected trailing return action, got %#v", last)
	}
}

func TestPresentAllThreadSelectionDoesNotCountCurrentWorkspaceAgainstGroupLimit(t *testing.T) {
	now := time.Date(2026, 4, 11, 5, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-current",
		DisplayName:   "current",
		WorkspaceRoot: "/data/dl/current",
		WorkspaceKey:  "/data/dl/current",
		ShortName:     "current",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-current": {
				ThreadID:   "thread-current",
				Name:       "当前会话",
				CWD:        "/data/dl/current",
				LastUsedAt: now.Add(30 * time.Minute),
			},
		},
	})
	for i := 1; i <= 5; i++ {
		workspaceKey := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: workspaceKey,
			WorkspaceKey:  workspaceKey,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        workspaceKey,
					LastUsedAt: now.Add(time.Duration(i) * time.Minute),
				},
			},
		})
	}
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-current",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if prompt.ContextKey != "/data/dl/current" || prompt.ContextTitle != "当前工作区" {
		t.Fatalf("expected current workspace context, got %#v", prompt)
	}
	if len(prompt.Options) != 6 {
		t.Fatalf("expected current workspace plus five other groups without expand action, got %#v", prompt.Options)
	}
	if prompt.Options[0].OptionID != "thread-current" {
		t.Fatalf("expected current workspace thread to remain present, got %#v", prompt.Options[0])
	}
	for _, option := range prompt.Options {
		if option.ActionKind == "show_all_thread_workspaces" {
			t.Fatalf("did not expect expand action when only five non-current groups exist, got %#v", prompt.Options)
		}
	}
}

func TestUseThreadSameAsCurrentStillAcknowledgesSelection(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "dl",
		WorkspaceRoot:           "/data/dl",
		WorkspaceKey:            "/data/dl",
		ShortName:               "dl",
		Online:                  true,
		ObservedFocusedThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb",
		Threads: map[string]*state.ThreadRecord{
			"019d56f0-de5e-7943-bc9a-18c42ef11acb": {ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", Name: "修复登录流程", CWD: "/data/dl"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "019d56f0-de5e-7943-bc9a-18c42ef11acb",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "selection_unchanged" {
		t.Fatalf("expected unchanged selection notice, got %#v", events)
	}
}

func TestShowWorkspaceThreadsDisplaysSingleWorkspaceAllSessions(t *testing.T) {
	now := time.Date(2026, 4, 10, 13, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web-1",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "较早", CWD: "/data/dl/web", LastUsedAt: now.Add(-2 * time.Hour)},
			"thread-2": {ThreadID: "thread-2", Name: "最新", CWD: "/data/dl/web", LastUsedAt: now.Add(-10 * time.Minute)},
			"thread-3": {ThreadID: "thread-3", Name: "中间", CWD: "/data/dl/web", LastUsedAt: now.Add(-1 * time.Hour)},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowWorkspaceThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
	})

	if len(events) != 1 {
		t.Fatalf("expected workspace selection prompt, got %#v", events)
	}
	prompt := selectionPromptFromEvent(t, events[0])
	if prompt.Layout != "workspace_grouped_useall" || prompt.Title != "web 全部会话" || len(prompt.Options) != 4 {
		t.Fatalf("unexpected workspace-all prompt: %#v", prompt)
	}
	if prompt.Options[0].OptionID != "thread-2" || prompt.Options[1].OptionID != "thread-3" || prompt.Options[2].OptionID != "thread-1" {
		t.Fatalf("expected workspace-all prompt to keep recency order, got %#v", prompt.Options)
	}
	if prompt.Options[3].ActionKind != "show_all_threads" || prompt.Options[3].ButtonLabel != "全部会话" {
		t.Fatalf("expected trailing return-to-global action, got %#v", prompt.Options[3])
	}
}

func TestNewLocalThreadSequenceAnnouncesSelectionOnlyOnce(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})

	var selectionEvents []control.UIEvent
	selectionEvents = append(selectionEvents, svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadDiscovered,
		ThreadID: "thread-2",
		CWD:      "/data/dl",
	})...)
	selectionEvents = append(selectionEvents, svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})...)
	selectionEvents = append(selectionEvents, svc.renderTextItem("inst-1", "thread-2", "turn-1", "item-1", "你好", true)...)

	count := 0
	for _, event := range selectionEvents {
		if event.ThreadSelection != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one selection change announcement, got %d from %#v", count, selectionEvents)
	}
}

func TestLocalPlaceholderInteractionDoesNotStealSelectionFromRunningThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-6d13": {ThreadID: "thread-6d13", Name: "主线程", CWD: "/data/dl"},
			"thread-81a0": {ThreadID: "thread-81a0", Name: "占位线程", CWD: "/home/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-6d13",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(started) != 2 || started[1].ThreadSelection == nil || started[1].ThreadSelection.ThreadID != "thread-6d13" {
		t.Fatalf("expected selection to switch to executing thread, got %#v", started)
	}

	later := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-81a0",
		CWD:      "/home/dl/droid",
		Action:   "turn_start",
	})
	if len(later) != 0 {
		t.Fatalf("expected placeholder interaction during running local turn not to emit extra selection updates, got %#v", later)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-6d13" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected selected thread to remain on executing thread, got %q", surface.SelectedThreadID)
	}
	inst := svc.root.Instances["inst-1"]
	if inst.ObservedFocusedThreadID != "thread-81a0" {
		t.Fatalf("expected observed focus to still record latest local placeholder thread, got %q", inst.ObservedFocusedThreadID)
	}
	if inst.ActiveThreadID != "thread-6d13" {
		t.Fatalf("expected active thread to remain executing thread, got %q", inst.ActiveThreadID)
	}
}

func TestFollowLocalAutoReevaluationBlockedByPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})
	svc.root.Surfaces["surface-1"].PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	for _, event := range events {
		if event.ThreadSelection != nil {
			t.Fatalf("expected pending request to freeze follow-local retarget, got %#v", events)
		}
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected follow-local selection to remain frozen on prior thread, got %#v", surface)
	}
	if inst := svc.root.Instances["inst-1"]; inst.ObservedFocusedThreadID != "thread-2" {
		t.Fatalf("expected observed focus to still advance, got %q", inst.ObservedFocusedThreadID)
	}
}

func TestFollowLocalAutoReevaluationBlockedByRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 7, 19, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})
	svc.root.Surfaces["surface-1"].ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "req-1"}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	for _, event := range events {
		if event.ThreadSelection != nil {
			t.Fatalf("expected request capture to freeze follow-local retarget, got %#v", events)
		}
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected follow-local selection to remain frozen on prior thread, got %#v", surface)
	}
	if inst := svc.root.Instances["inst-1"]; inst.ObservedFocusedThreadID != "thread-2" {
		t.Fatalf("expected observed focus to still advance, got %q", inst.ObservedFocusedThreadID)
	}
}

func TestThreadsSnapshotDoesNotDropPreviouslyObservedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil {
		t.Fatal("expected observed thread to be preserved after empty snapshot")
	}
	if thread.Name != "修复登录流程" || thread.CWD != "/data/dl/droid" {
		t.Fatalf("expected thread metadata to be preserved, got %#v", thread)
	}
	if thread.Loaded {
		t.Fatalf("expected preserved thread to be marked not loaded after empty snapshot, got %#v", thread)
	}
}

func TestThreadsSnapshotDoesNotBroadenManagedHeadlessWorkspaceRoot(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "atlas",
		WorkspaceRoot: "/data/dl/atlas",
		WorkspaceKey:  "/data/dl/atlas",
		ShortName:     "atlas",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "整理 atlas", CWD: "/data/dl/atlas", Loaded: true},
		},
	})

	svc.ApplyAgentEvent("inst-headless-1", agentproto.Event{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{
			{ThreadID: "thread-ancestor", Name: "污染线程", CWD: "/data/dl", Loaded: true},
			{ThreadID: "thread-1", Name: "整理 atlas", CWD: "/data/dl/atlas", Loaded: true},
		},
	})

	inst := svc.root.Instances["inst-headless-1"]
	if inst.WorkspaceRoot != "/data/dl/atlas" || inst.WorkspaceKey != "/data/dl/atlas" || inst.DisplayName != "atlas" {
		t.Fatalf("expected managed headless workspace metadata to stay on the precise workspace, got %#v", inst)
	}
}

func TestPendingRemoteDispatchKeepsLaterMessageQueuedUntilTurnStarts(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	dispatched := false
	for _, event := range first {
		if event.Command != nil {
			dispatched = true
			break
		}
	}
	if !dispatched {
		t.Fatalf("expected first surface to dispatch immediately, got %#v", first)
	}
	if binding := svc.pendingRemote["inst-1"]; binding == nil || binding.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected pending remote binding for surface-1, got %#v", binding)
	}

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "排队",
	})
	if len(second) != 1 || second[0].PendingInput == nil || second[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected follow-up message to stay queued, got %#v", second)
	}
	for _, event := range second {
		if event.Command != nil {
			t.Fatalf("expected no second dispatch while instance reserved, got %#v", second)
		}
	}
	if svc.root.Surfaces["surface-1"].ActiveQueueItemID == "" {
		t.Fatalf("expected first queue item to remain active while turn start is pending")
	}
	if len(svc.root.Surfaces["surface-1"].QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected queue to retain one waiting item, got %#v", svc.root.Surfaces["surface-1"].QueuedQueueItemIDs)
	}
}

func TestRemoteTurnLifecycleUsesExplicitSurfaceBinding(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-2"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "你好",
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if binding := svc.activeRemote["inst-1"]; binding == nil || binding.SurfaceSessionID != "surface-1" || binding.TurnID != "turn-1" || binding.ThreadID != "thread-2" {
		t.Fatalf("expected active remote binding to follow the queued route, got %#v", binding)
	}
	if len(started) == 0 || started[0].PendingInput == nil || started[0].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected running state to project to queued surface, got %#v", started)
	}

	mid := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "您好"},
	})
	if len(mid) != 0 {
		t.Fatalf("expected assistant text to stay buffered until turn completion, got %#v", mid)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if svc.activeRemote["inst-1"] != nil {
		t.Fatalf("expected active remote binding to clear after completion, got %#v", svc.activeRemote["inst-1"])
	}
	var sawFinal, sawTypingOff bool
	for _, event := range finished {
		if event.Block != nil && event.Block.Final {
			sawFinal = true
			if event.SurfaceSessionID != "surface-1" || event.Block.Text != "您好" {
				t.Fatalf("expected final block on queued surface, got %#v", event)
			}
		}
		if event.PendingInput != nil && event.PendingInput.TypingOff {
			sawTypingOff = true
			if event.SurfaceSessionID != "surface-1" {
				t.Fatalf("expected typing-off on queued surface, got %#v", event)
			}
		}
	}
	if !sawFinal {
		t.Fatalf("expected final block on queued surface, got %#v", finished)
	}
	if !sawTypingOff {
		t.Fatalf("expected typing-off on queued surface, got %#v", finished)
	}
}

func TestTurnCompletedEmbedsFileChangeSummaryIntoFinalAssistantBlock(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1,2 @@\n-old\n+new\n+more",
		}},
	}); len(events) != 0 {
		t.Fatalf("expected file change completion to stay buffered until turn completion, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已完成修改。"},
	}); len(events) != 0 {
		t.Fatalf("expected assistant final text to stay buffered until turn completion, got %#v", events)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		event := finished[i]
		if event.Block != nil && event.Block.Final && event.Block.Text == "已完成修改。" {
			finalBlockEvent = &finished[i]
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected final assistant block, got %#v", finished)
	}
	if finalBlockEvent.FileChangeSummary == nil {
		t.Fatalf("expected final assistant block to embed file summary, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.SourceMessageID != "msg-1" {
		t.Fatalf("expected final assistant block to retain source message id, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.SourceMessagePreview != "处理一下" {
		t.Fatalf("expected final assistant block to retain source message preview, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.FileChangeSummary.FileCount != 1 || finalBlockEvent.FileChangeSummary.AddedLines != 2 || finalBlockEvent.FileChangeSummary.RemovedLines != 1 {
		t.Fatalf("unexpected embedded file change summary payload: %#v", finalBlockEvent.FileChangeSummary)
	}
}

func TestTurnCompletedEmbedsElapsedIntoFinalAssistantBlock(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	now = now.Add(3400 * time.Millisecond)
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已完成。"},
	}); len(events) != 0 {
		t.Fatalf("expected assistant final text to stay buffered until turn completion, got %#v", events)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		event := finished[i]
		if event.Block != nil && event.Block.Final && event.Block.Text == "已完成。" {
			finalBlockEvent = &finished[i]
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected final assistant block, got %#v", finished)
	}
	if finalBlockEvent.FinalTurnSummary == nil {
		t.Fatalf("expected final assistant block to embed elapsed summary, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.FinalTurnSummary.Elapsed != 3400*time.Millisecond {
		t.Fatalf("unexpected elapsed payload: %#v", finalBlockEvent.FinalTurnSummary)
	}
	if finalBlockEvent.FileChangeSummary != nil {
		t.Fatalf("expected no file summary on elapsed-only final block, got %#v", finalBlockEvent.FileChangeSummary)
	}
}

func TestTurnCompletedAggregatesMultipleCompletedFileChangeItemsIntoFinalBlock(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-2",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "docs/guide.md",
			Kind: agentproto.FileChangeAdd,
			Diff: "line 1\nline 2",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已完成修改。"},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var summary *control.FileChangeSummary
	for _, event := range finished {
		if event.Block != nil && event.Block.Final {
			summary = event.FileChangeSummary
		}
	}
	if summary == nil {
		t.Fatalf("expected aggregated file change summary on final block, got %#v", finished)
	}
	if summary.FileCount != 2 || summary.AddedLines != 3 || summary.RemovedLines != 1 {
		t.Fatalf("unexpected aggregated summary totals: %#v", summary)
	}
	if len(summary.Files) != 2 {
		t.Fatalf("expected two file entries, got %#v", summary.Files)
	}
	if summary.Files[0].Path != "docs/guide.md" || summary.Files[1].Path != "pkg/app.go" {
		t.Fatalf("expected summary files to be sorted by path, got %#v", summary.Files)
	}
}

func TestTurnCompletedSynthesizesFinalBlockWhenOnlyFileSummaryExists(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		if finished[i].Block != nil && finished[i].Block.Final {
			finalBlockEvent = &finished[i]
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected synthetic final block when only file summary exists, got %#v", finished)
	}
	if finalBlockEvent.Block.Text != "已完成文件修改。" {
		t.Fatalf("expected synthetic final block text, got %#v", finalBlockEvent.Block)
	}
	if finalBlockEvent.FileChangeSummary == nil || finalBlockEvent.FileChangeSummary.FileCount != 1 {
		t.Fatalf("expected synthetic final block to carry file summary, got %#v", finalBlockEvent)
	}
}

func TestTurnCompletedSynthesizesFinalBlockWhenOnlyElapsedExists(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	now = now.Add(2100 * time.Millisecond)

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		if finished[i].Block != nil && finished[i].Block.Final {
			finalBlockEvent = &finished[i]
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected synthetic final block when only elapsed exists, got %#v", finished)
	}
	if finalBlockEvent.Block.Text != "已完成。" {
		t.Fatalf("expected synthetic elapsed final block text, got %#v", finalBlockEvent.Block)
	}
	if finalBlockEvent.FinalTurnSummary == nil || finalBlockEvent.FinalTurnSummary.Elapsed != 2100*time.Millisecond {
		t.Fatalf("expected synthetic final block to carry elapsed summary, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.FinalTurnSummary.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected synthetic final block to carry thread cwd, got %#v", finalBlockEvent.FinalTurnSummary)
	}
	if finalBlockEvent.FileChangeSummary != nil {
		t.Fatalf("expected no file summary on elapsed-only synthetic block, got %#v", finalBlockEvent.FileChangeSummary)
	}
}

func TestDeclinedFileChangeDoesNotEmbedFinalSummary(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "declined",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "未执行文件改动。"},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	for _, event := range finished {
		if event.FileChangeSummary != nil {
			t.Fatalf("expected declined file change not to produce final summary, got %#v", finished)
		}
	}
}

func TestHandleCommandDispatchFailureClearsPendingRemoteState(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	svc.BindPendingRemoteCommand("surface-1", "cmd-1")
	events := svc.HandleCommandDispatchFailure("surface-1", "cmd-1", errors.New("relay unavailable"))
	if svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected pending remote binding to clear after dispatch failure")
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected surface active queue to clear after dispatch failure")
	}
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected queue item to be marked failed, got %#v", item)
	}
	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "dispatch_failed" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and failure notice, got %#v", events)
	}
}

func TestHandleCommandRejectedClearsPendingRemoteState(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.BindPendingRemoteCommand("surface-1", "cmd-1")

	events := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-1",
		Accepted:  false,
		Error:     "translator failed",
		Problem: &agentproto.ErrorInfo{
			Code:      "translate_command_failed",
			Layer:     "wrapper",
			Stage:     "translate_command",
			Message:   "wrapper 无法把 relay 命令转换成 Codex 请求。",
			Details:   "translator failed",
			CommandID: "cmd-1",
		},
	})
	if svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected pending remote binding to clear after rejected command")
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected active queue to clear after rejected command")
	}
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected queue item to be marked failed, got %#v", item)
	}
	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "command_rejected" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and rejection notice, got %#v", events)
	}
	for _, event := range events {
		if event.Notice == nil || event.Notice.Code != "command_rejected" {
			continue
		}
		if !strings.Contains(event.Notice.Title, "wrapper.translate_command") || !strings.Contains(event.Notice.Text, "translator failed") {
			t.Fatalf("expected structured rejection notice, got %#v", event.Notice)
		}
	}
}

func TestApplyAgentSystemErrorTargetsAttachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:      "stdout_parse_failed",
		Layer:     "wrapper",
		Stage:     "observe_codex_stdout",
		Operation: "codex.stdout",
		Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
		Details:   "invalid character 'x' looking for beginning of value",
	}))
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected one problem notice, got %#v", events)
	}
	if events[0].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected notice on attached surface, got %#v", events[0])
	}
	if events[0].Notice.Code != debugErrorNoticeCode {
		t.Fatalf("unexpected notice code: %#v", events[0].Notice)
	}
	if !strings.Contains(events[0].Notice.Title, "wrapper.observe_codex_stdout") || !strings.Contains(events[0].Notice.Text, "invalid character") {
		t.Fatalf("expected structured problem text, got %#v", events[0].Notice)
	}
}

func TestRemoteTurnInterruptedWithProblemFailsQueueAndEmitsStructuredNotice(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		Status:       "interrupted",
		ErrorMessage: "stream disconnected before completion: stream closed before response.completed",
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "stream disconnected before completion: stream closed before response.completed",
			Details:   "stream disconnected before completion: stream closed before response.completed",
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			Retryable: true,
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected interrupted remote turn with problem to fail queue item, got %#v", item)
	}

	var sawFailedPending, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueItemID == "queue-1" && event.PendingInput.Status == string(state.QueueItemFailed) {
			sawFailedPending = true
		}
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			sawNotice = true
			if !strings.Contains(event.Notice.Title, "codex.runtime_error") || !strings.Contains(event.Notice.Text, "responseStreamDisconnected") {
				t.Fatalf("expected structured turn failure notice, got %#v", event.Notice)
			}
		}
	}
	if !sawFailedPending || !sawNotice {
		t.Fatalf("expected failed queue state and structured notice, got %#v", events)
	}
}

func TestApplyInstanceDisconnectedFailsActiveRemoteItem(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "第二条",
	})

	events := svc.ApplyInstanceDisconnected("inst-1")
	if svc.activeRemote["inst-1"] != nil || svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected remote ownership to clear on disconnect")
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected active queue to clear on disconnect")
	}
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected dispatch mode to reset on disconnect, got %s", surface.DispatchMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" {
		t.Fatalf("expected surface to detach on disconnect, got %+v", surface)
	}
	active := surface.QueueItems["queue-1"]
	if active == nil || active.Status != state.QueueItemFailed {
		t.Fatalf("expected active queue item to fail on disconnect, got %#v", active)
	}
	queued := surface.QueueItems["queue-2"]
	if queued == nil || queued.Status != state.QueueItemQueued {
		t.Fatalf("expected queued item to remain queued on disconnect, got %#v", queued)
	}
	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueItemID == "queue-1" && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "attached_instance_offline" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and offline notice, got %#v", events)
	}
}

func TestApplyInstanceTransportDegradedKeepsAttachmentAndQueuedWork(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Delta:    "部分输出",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "第二条",
	})

	events := svc.ApplyInstanceTransportDegraded("inst-1", true)
	if svc.activeRemote["inst-1"] == nil {
		t.Fatalf("expected active remote ownership to stay during transport degrade")
	}
	if svc.root.Instances["inst-1"].Online || svc.root.Instances["inst-1"].ActiveTurnID != "" {
		t.Fatalf("expected instance to become offline without active turn, got %#v", svc.root.Instances["inst-1"])
	}
	if len(svc.itemBuffers) == 0 {
		t.Fatalf("expected turn buffers to remain available while waiting for recovery")
	}

	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "queue-1" {
		t.Fatalf("expected active queue item to stay attached on transport degrade, got %s", surface.ActiveQueueItemID)
	}
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected attachment and selected thread to stay, got %+v", surface)
	}
	active := surface.QueueItems["queue-1"]
	if active == nil || active.Status != state.QueueItemRunning {
		t.Fatalf("expected active queue item to stay running on transport degrade, got %#v", active)
	}
	queued := surface.QueueItems["queue-2"]
	if queued == nil || queued.Status != state.QueueItemQueued {
		t.Fatalf("expected queued item to remain queued on transport degrade, got %#v", queued)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" || snapshot.Dispatch.InstanceOnline || snapshot.Dispatch.QueuedCount != 1 || snapshot.Dispatch.ActiveItemStatus != string(state.QueueItemRunning) {
		t.Fatalf("expected snapshot to retain offline attachment and queued work, got %#v", snapshot)
	}

	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueItemID == "queue-1" && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "attached_instance_transport_degraded" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and degraded notice, got %#v", events)
	}

	recovery := svc.ApplyInstanceConnected("inst-1")
	if surface.ActiveQueueItemID != "queue-1" {
		t.Fatalf("expected original active work to stay bound after reconnect, got active=%s", surface.ActiveQueueItemID)
	}
	if len(recovery) != 0 {
		t.Fatalf("expected reconnect to wait for the in-flight turn before dispatching queued work, got %#v", recovery)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if surface.ActiveQueueItemID != "queue-2" {
		t.Fatalf("expected queued work to resume after preserved turn completed, got active=%s", surface.ActiveQueueItemID)
	}
	resumed := surface.QueueItems["queue-2"]
	if resumed == nil || resumed.Status != state.QueueItemDispatching {
		t.Fatalf("expected queued item to re-dispatch after preserved turn completed, got %#v", resumed)
	}
	var sawCommand bool
	for _, event := range finished {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawCommand = true
		}
	}
	if !sawCommand {
		t.Fatalf("expected preserved turn completion to dispatch queued work, got %#v", finished)
	}
}

func TestDetachAfterTransportDegradedDetachesImmediately(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 30, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyInstanceTransportDegraded("inst-1", true)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.Abandoning {
		t.Fatalf("expected degraded offline detach to finalize immediately, got %#v", surface)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected detach to release instance claim, got %#v", claim)
	}
	var sawDetached, sawInterrupt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "detached" {
			sawDetached = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandTurnInterrupt {
			sawInterrupt = true
		}
	}
	if !sawDetached || sawInterrupt {
		t.Fatalf("expected immediate detach notice without interrupt, got %#v", events)
	}
}

func TestStopWhileTransportDegradedReportsInstanceOffline(t *testing.T) {
	now := time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyInstanceTransportDegraded("inst-1", true)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "stop_instance_offline" {
		t.Fatalf("expected stop_instance_offline notice, got %#v", events)
	}
	if strings.Contains(events[0].Notice.Text, "已发送停止请求") {
		t.Fatalf("expected offline stop notice instead of sent interrupt, got %#v", events[0].Notice)
	}
}

func TestApplyInstanceConnectedDoesNotResumeDetachedSurfaceQueue(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyInstanceDisconnected("inst-1")
	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].Notice == nil || queued[0].Notice.Code != "not_attached" {
		t.Fatalf("expected detached surface to reject new input, got %#v", queued)
	}

	events := svc.ApplyInstanceConnected("inst-1")
	if len(events) != 0 {
		t.Fatalf("expected reconnect not to resume a detached surface, got %#v", events)
	}
}

func TestTextMessageDetachedNormalUsesWorkspaceWording(t *testing.T) {
	now := time.Date(2026, 4, 9, 19, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "not_attached" {
		t.Fatalf("expected detached normal text to reject with not_attached, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "您没有接管任何工作区") || strings.Contains(events[0].Notice.Text, "实例") {
		t.Fatalf("expected workspace-first detached wording, got %#v", events[0].Notice)
	}
}

func TestRemovedNewInstanceCommandShowsMigrationNotice(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRemovedCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/newinstance",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "command_removed_newinstance" {
		t.Fatalf("expected removed command notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/use") || !strings.Contains(events[0].Notice.Text, "/useall") {
		t.Fatalf("expected migration guidance in removed command notice, got %#v", events[0].Notice)
	}
}

func TestRemovedResumeHeadlessCardShowsMigrationNotice(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRemovedCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "resume_headless_thread",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "selection_expired" {
		t.Fatalf("expected stale headless card notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/newinstance") {
		t.Fatalf("expected stale headless card notice to mention removed command, got %#v", events[0].Notice)
	}
	if !strings.Contains(events[0].Notice.Text, "/use") || !strings.Contains(events[0].Notice.Text, "/useall") {
		t.Fatalf("expected migration guidance for stale headless card, got %#v", events[0].Notice)
	}
}

func TestRemovedKillInstanceCommandShowsDetachMigrationNotice(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 5, 30, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRemovedCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/killinstance",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "command_removed_killinstance" {
		t.Fatalf("expected killinstance migration notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/detach") {
		t.Fatalf("expected killinstance migration to mention /detach, got %#v", events[0].Notice)
	}
}

func TestRemovedUnknownCommandShowsConcreteCommand(t *testing.T) {
	now := time.Date(2026, 4, 8, 10, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRemovedCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/legacy-command",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "command_removed" {
		t.Fatalf("expected generic removed command notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/legacy-command") {
		t.Fatalf("expected generic removed notice to mention concrete command, got %#v", events[0].Notice)
	}
}
