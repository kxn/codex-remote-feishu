package orchestrator

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newServiceForTest(now *time.Time) *Service {
	return NewService(func() time.Time { return *now }, Config{TurnHandoffWait: 800 * time.Millisecond}, renderer.NewPlanner())
}

func materializeVSCodeSurfaceForTest(svc *Service, surfaceID string) {
	svc.MaterializeSurface(surfaceID, "app-1", "chat-1", "user-1")
	svc.root.Surfaces[surfaceID].ProductMode = state.ProductModeVSCode
}

func firstCommands(entries []control.CommandCatalogEntry) []string {
	commands := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Commands) == 0 {
			continue
		}
		commands = append(commands, entry.Commands[0])
	}
	return commands
}

type fakePersistedThreadCatalog struct {
	recent    []state.ThreadRecord
	byID      map[string]state.ThreadRecord
	recentErr error
	byIDErr   error
}

func (f *fakePersistedThreadCatalog) RecentThreads(limit int) ([]state.ThreadRecord, error) {
	if f == nil {
		return nil, nil
	}
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	if limit <= 0 || limit >= len(f.recent) {
		return append([]state.ThreadRecord(nil), f.recent...), nil
	}
	return append([]state.ThreadRecord(nil), f.recent[:limit]...), nil
}

func (f *fakePersistedThreadCatalog) ThreadByID(threadID string) (*state.ThreadRecord, error) {
	if f == nil {
		return nil, nil
	}
	if f.byIDErr != nil {
		return nil, f.byIDErr
	}
	thread, ok := f.byID[threadID]
	if !ok {
		return nil, nil
	}
	threadCopy := thread
	return &threadCopy, nil
}

func recordLocalFinalText(t *testing.T, svc *Service, instanceID, threadID, turnID, itemID, text string) []control.UIEvent {
	t.Helper()
	if events := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		ItemKind: "agent_message",
		Delta:    text,
	}); len(events) != 0 {
		t.Fatalf("expected no UI events while collecting local text, got %#v", events)
	}
	if events := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		ItemKind: "agent_message",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events before local turn completion, got %#v", events)
	}
	return svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  threadID,
		TurnID:    turnID,
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
}

func setupAutoContinueSurface(t *testing.T, svc *Service) *state.SurfaceConsoleRecord {
	t.Helper()
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
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected attached surface")
	}
	surface.AutoContinue.Enabled = true
	return surface
}

func startRemoteTurnForAutoContinueTest(t *testing.T, svc *Service, messageID, text, turnID string) {
	t.Helper()
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        messageID,
		Text:             text,
	})
	if len(events) == 0 {
		t.Fatal("expected enqueue events")
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    turnID,
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
}

func completeRemoteTurnWithFinalText(t *testing.T, svc *Service, turnID, status, errorMessage, finalText string, problem *agentproto.ErrorInfo) []control.UIEvent {
	t.Helper()
	if strings.TrimSpace(finalText) != "" {
		if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     agentproto.EventItemDelta,
			ThreadID: "thread-1",
			TurnID:   turnID,
			ItemID:   "item-" + turnID,
			ItemKind: "agent_message",
			Delta:    finalText,
		}); len(events) != 0 {
			t.Fatalf("expected no UI events while collecting remote final text, got %#v", events)
		}
		if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: "thread-1",
			TurnID:   turnID,
			ItemID:   "item-" + turnID,
			ItemKind: "agent_message",
		}); len(events) != 0 {
			t.Fatalf("expected no UI events before remote turn completion, got %#v", events)
		}
	}
	return svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       turnID,
		Status:       status,
		ErrorMessage: errorMessage,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
		Problem:      problem,
	})
}

func TestItemBufferTextMaterializesLazily(t *testing.T) {
	buf := &itemBuffer{}

	buf.replaceText("hello")
	buf.appendText(", ")
	buf.appendText("world")

	if buf.textValue != "" {
		t.Fatalf("expected joined text cache to stay empty before materialization, got %q", buf.textValue)
	}
	if len(buf.textChunks) != 3 {
		t.Fatalf("expected three text chunks before materialization, got %#v", buf.textChunks)
	}

	if got := buf.text(); got != "hello, world" {
		t.Fatalf("buf.text() = %q, want %q", got, "hello, world")
	}
	if buf.textValue != "hello, world" {
		t.Fatalf("expected joined text cache after materialization, got %q", buf.textValue)
	}
	if len(buf.textChunks) != 1 || buf.textChunks[0] != "hello, world" {
		t.Fatalf("expected chunks to collapse after materialization, got %#v", buf.textChunks)
	}
}

func TestCompleteItemUsesMaterializedBufferedText(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
		Delta:    "hello",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events on item delta, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
		Delta:    " world",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events on item delta, got %#v", events)
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
	})
	found := false
	for _, event := range events {
		if event.Block != nil && event.Block.Text == "hello world" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected completed item to render full materialized text, got %#v", events)
	}
}

func TestAttachPinsObservedFocusedThread(t *testing.T) {
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	if len(events) < 2 {
		t.Fatalf("expected snapshot and notice, got %d events", len(events))
	}
	surface := svc.root.Surfaces["feishu:chat:1"]
	if surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected selected thread to be pinned, got %q", surface.SelectedThreadID)
	}
	if surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected route mode pinned, got %q", surface.RouteMode)
	}
}

func TestAttachFallsBackToActiveThreadWhenFocusedThreadUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-1",
		DisplayName:    "droid",
		WorkspaceRoot:  "/data/dl/droid",
		WorkspaceKey:   "/data/dl/droid",
		ShortName:      "droid",
		Online:         true,
		ActiveThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["feishu:chat:1"]
	if surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected selected thread to fall back to active thread, got %q", surface.SelectedThreadID)
	}
	if surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected route mode pinned, got %q", surface.RouteMode)
	}
}

func TestAttachBusyInstanceRejectsSecondSurface(t *testing.T) {
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-2", ChatID: "chat-2", ActorUserID: "user-2", Text: "/mode vscode"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-2"]
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected second surface to remain detached, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "instance_busy" {
		t.Fatalf("expected instance_busy notice, got %#v", events)
	}
}

func TestAttachWithoutDefaultThreadEntersUnboundAndPromptsUse(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
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
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected surface to enter attached_unbound, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if event.SelectionPrompt != nil && event.SelectionPrompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected attach notice plus /use prompt, got %#v", events)
	}
}

func TestAttachWorkspaceEntersUnboundAndPromptsUse(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected workspace attach to claim droid, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected workspace attach to land unbound, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "workspace_attached" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if event.SelectionPrompt != nil && event.SelectionPrompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected workspace attach notice plus /use prompt, got %#v", events)
	}
}

func TestAttachWorkspaceSwitchClearsPinnedThread(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 5, 0, 0, time.UTC)
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修样式", CWD: "/data/dl/web"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-2" || surface.ClaimedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected workspace switch to move to web, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected workspace switch to clear pinned thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "workspace_switched" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if event.SelectionPrompt != nil && event.SelectionPrompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected workspace switch notice plus /use prompt, got %#v", events)
	}
}

func TestAttachWorkspaceSwitchBlockedByQueuedWork(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 10, 0, 0, time.UTC)
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.QueueItems["item-1"] = &state.QueueItemRecord{ID: "item-1", Status: state.QueueItemQueued}
	surface.QueuedQueueItemIDs = []string{"item-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
	})

	if surface.AttachedInstanceID != "inst-1" || surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected blocked switch to keep current workspace, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_switch_queued" {
		t.Fatalf("expected queued switch block notice, got %#v", events)
	}
}

func TestListWorkspacesMarksBusyClaimedWorkspaceDisabled(t *testing.T) {
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修样式", CWD: "/data/dl/web"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Kind != control.SelectionPromptAttachWorkspace || len(prompt.Options) != 2 {
		t.Fatalf("unexpected workspace prompt: %#v", prompt)
	}
	if prompt.Title != "工作区列表" || prompt.Layout != "grouped_attach_workspace" {
		t.Fatalf("expected workspace prompt title, got %#v", prompt)
	}
	for _, option := range prompt.Options {
		switch option.OptionID {
		case "/data/dl/droid":
			if !option.Disabled || option.ButtonLabel != "" || !strings.Contains(option.MetaText, "当前被其他飞书会话接管") {
				t.Fatalf("expected busy workspace to be disabled, got %#v", option)
			}
		case "/data/dl/web":
			if option.Disabled {
				t.Fatalf("expected free workspace to remain selectable, got %#v", option)
			}
		default:
			t.Fatalf("unexpected workspace option: %#v", option)
		}
	}
}

func TestListWorkspacesShowsCurrentSummaryAndSortsAttachableFirst(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-current": {ThreadID: "thread-current", Name: "当前工作", CWD: "/data/dl/droid", LastUsedAt: now.Add(-5 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-web",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-web",
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-ops",
		DisplayName:   "ops",
		WorkspaceRoot: "/data/dl/ops",
		WorkspaceKey:  "/data/dl/ops",
		ShortName:     "ops",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-ops": {ThreadID: "thread-ops", Name: "值班处理", CWD: "/data/dl/ops", LastUsedAt: now.Add(-1 * time.Hour)},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-current",
		ChatID:           "chat-current",
		ActorUserID:      "user-current",
		WorkspaceKey:     "/data/dl/droid",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-busy",
		ChatID:           "chat-busy",
		ActorUserID:      "user-busy",
		WorkspaceKey:     "/data/dl/ops",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-current",
		ChatID:           "chat-current",
		ActorUserID:      "user-current",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Layout != "grouped_attach_workspace" || prompt.ContextTitle != "当前工作区" {
		t.Fatalf("unexpected workspace prompt metadata: %#v", prompt)
	}
	if !strings.Contains(prompt.ContextText, "droid · 5分前") || !strings.Contains(prompt.ContextText, "/use 或 /new") {
		t.Fatalf("expected current workspace summary, got %#v", prompt.ContextText)
	}
	if len(prompt.Options) != 2 {
		t.Fatalf("expected current workspace to be summarized instead of listed, got %#v", prompt.Options)
	}
	if prompt.Options[0].OptionID != "/data/dl/web" || prompt.Options[0].Disabled || prompt.Options[0].ButtonLabel != "切换" || prompt.Options[0].MetaText != "2分前 · 有 VS Code 活动" {
		t.Fatalf("expected attachable workspace first with compact meta, got %#v", prompt.Options[0])
	}
	if prompt.Options[1].OptionID != "/data/dl/ops" || !prompt.Options[1].Disabled || prompt.Options[1].MetaText != "1小时前 · 当前被其他飞书会话接管" {
		t.Fatalf("expected busy workspace in unavailable section, got %#v", prompt.Options[1])
	}
}

func TestListWorkspacesUsesVisibleThreadCWDsForBroadHeadlessPool(t *testing.T) {
	now := time.Date(2026, 4, 9, 19, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 1; i <= 2; i++ {
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-headless-%d", i),
			DisplayName:   fmt.Sprintf("headless-%d", i),
			WorkspaceRoot: "/data/dl",
			WorkspaceKey:  "/data/dl",
			ShortName:     "dl",
			Source:        "headless",
			Managed:       true,
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-fs-%d", i): {ThreadID: fmt.Sprintf("thread-fs-%d", i), Name: "atlas", CWD: "/data/dl/atlas", Loaded: true},
				fmt.Sprintf("thread-sf-%d", i): {ThreadID: fmt.Sprintf("thread-sf-%d", i), Name: "harbor", CWD: "/data/dl/harbor", Loaded: true},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one workspace selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Kind != control.SelectionPromptAttachWorkspace || prompt.Title != "工作区列表" || prompt.Layout != "grouped_attach_workspace" {
		t.Fatalf("unexpected workspace prompt: %#v", prompt)
	}
	if len(prompt.Options) != 2 {
		t.Fatalf("expected two real workspaces instead of broad instance root, got %#v", prompt.Options)
	}
	got := map[string]bool{}
	for _, option := range prompt.Options {
		got[option.OptionID] = true
	}
	if !got["/data/dl/atlas"] || !got["/data/dl/harbor"] || got["/data/dl"] {
		t.Fatalf("expected thread cwd workspaces only, got %#v", prompt.Options)
	}
}

func TestListWorkspacesShowsPersistedOnlyWorkspaceAsRecoverable(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{
				ThreadID:   "thread-picdetect",
				Name:       "排查图片识别",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-3 * time.Minute),
			},
		},
		byID: map[string]state.ThreadRecord{
			"thread-picdetect": {
				ThreadID:   "thread-picdetect",
				Name:       "排查图片识别",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-3 * time.Minute),
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one workspace selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if len(prompt.Options) != 1 {
		t.Fatalf("expected one recoverable workspace option, got %#v", prompt.Options)
	}
	option := prompt.Options[0]
	if option.OptionID != "/data/dl/picdetect" || option.ActionKind != "show_workspace_threads" || option.ButtonLabel != "恢复" || option.Disabled {
		t.Fatalf("expected persisted-only workspace to route to workspace thread list, got %#v", option)
	}
	if option.MetaText != "3分前 · 后台可恢复" {
		t.Fatalf("expected recoverable workspace meta, got %#v", option.MetaText)
	}
}

func TestListWorkspacesShowsRecentFiveWithExpandAction(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: key,
			WorkspaceKey:  key,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        key,
					LastUsedAt: now.Add(-time.Duration(i) * time.Minute),
				},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one workspace selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Title != "工作区列表" {
		t.Fatalf("unexpected prompt title: %#v", prompt)
	}
	if len(prompt.Options) != 6 {
		t.Fatalf("expected 5 workspaces plus expand action, got %#v", prompt.Options)
	}
	for i := 0; i < 5; i++ {
		if prompt.Options[i].ActionKind == "show_all_workspaces" {
			t.Fatalf("expand action appeared before recent workspaces: %#v", prompt.Options)
		}
	}
	last := prompt.Options[5]
	if last.ActionKind != "show_all_workspaces" || last.ButtonLabel != "全部工作区" || last.MetaText != "还有 1 个工作区未显示" {
		t.Fatalf("unexpected expand option: %#v", last)
	}
}

func TestShowAllWorkspacesExpandsListAndOffersReturn(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("/data/dl/proj-%d", i)
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID:    fmt.Sprintf("inst-%d", i),
			DisplayName:   fmt.Sprintf("proj-%d", i),
			WorkspaceRoot: key,
			WorkspaceKey:  key,
			ShortName:     fmt.Sprintf("proj-%d", i),
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				fmt.Sprintf("thread-%d", i): {
					ThreadID:   fmt.Sprintf("thread-%d", i),
					Name:       fmt.Sprintf("会话-%d", i),
					CWD:        key,
					LastUsedAt: now.Add(-time.Duration(i) * time.Minute),
				},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllWorkspaces,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one workspace selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Title != "全部工作区" {
		t.Fatalf("unexpected prompt title: %#v", prompt)
	}
	if len(prompt.Options) != 7 {
		t.Fatalf("expected all workspaces plus return action, got %#v", prompt.Options)
	}
	last := prompt.Options[6]
	if last.ActionKind != "show_recent_workspaces" || last.ButtonLabel != "最近工作区" || last.MetaText != "回到最近 5 个工作区" {
		t.Fatalf("unexpected return option: %#v", last)
	}
}

func TestAttachWorkspaceUsesThreadWorkspaceFromBroadHeadlessPool(t *testing.T) {
	now := time.Date(2026, 4, 9, 19, 35, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless-1",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-fs": {ThreadID: "thread-fs", Name: "修复 relay", CWD: "/data/dl/atlas", Loaded: true},
			"thread-sf": {ThreadID: "thread-sf", Name: "整理 harbor", CWD: "/data/dl/harbor", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/atlas",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-headless-1" || surface.ClaimedWorkspaceKey != "/data/dl/atlas" {
		t.Fatalf("expected workspace attach to resolve via thread cwd, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected broad-pool workspace attach to remain unbound, got %#v", surface)
	}
	var threadPrompt *control.SelectionPrompt
	for _, event := range events {
		if event.SelectionPrompt != nil && event.SelectionPrompt.Kind == control.SelectionPromptUseThread {
			threadPrompt = event.SelectionPrompt
			break
		}
	}
	if threadPrompt == nil || len(threadPrompt.Options) != 1 || threadPrompt.Options[0].OptionID != "thread-fs" {
		t.Fatalf("expected /use prompt to be scoped to selected workspace, got %#v", events)
	}
}

func TestShowWorkspaceThreadsSupportsPersistedOnlyWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 10, 14, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{
				ThreadID:   "thread-picdetect",
				Name:       "最新排查",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-2 * time.Minute),
			},
			{
				ThreadID:   "thread-other",
				Name:       "其他工作区",
				Preview:    "other",
				CWD:        "/data/dl/other",
				Loaded:     true,
				LastUsedAt: now.Add(-1 * time.Minute),
			},
		},
		byID: map[string]state.ThreadRecord{
			"thread-picdetect": {
				ThreadID:   "thread-picdetect",
				Name:       "最新排查",
				Preview:    "sqlite only",
				CWD:        "/data/dl/picdetect",
				Loaded:     true,
				LastUsedAt: now.Add(-2 * time.Minute),
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowWorkspaceThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/picdetect",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected workspace thread selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Title != "picdetect 全部会话" || len(prompt.Options) != 2 {
		t.Fatalf("unexpected persisted-only workspace prompt: %#v", prompt)
	}
	if prompt.Options[0].OptionID != "thread-picdetect" || !prompt.Options[0].AllowCrossWorkspace {
		t.Fatalf("expected persisted-only thread option to remain recoverable, got %#v", prompt.Options[0])
	}
	if prompt.Options[1].ActionKind != "show_all_threads" {
		t.Fatalf("expected trailing return action, got %#v", prompt.Options[1])
	}
}

func TestUseBusyIdleThreadShowsKickPromptAndConfirmTransfersClaim(t *testing.T) {
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
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		ProductMode:        state.ProductModeVSCode,
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}
	svc.instanceClaims["inst-1"] = &instanceClaimRecord{InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	promptEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(promptEvents) != 1 || promptEvents[0].SelectionPrompt == nil || promptEvents[0].SelectionPrompt.Kind != control.SelectionPromptKickThread {
		t.Fatalf("expected kick confirmation prompt, got %#v", promptEvents)
	}

	confirm := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionConfirmKickThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})

	first := svc.root.Surfaces["surface-1"]
	second := svc.root.Surfaces["surface-2"]
	if first.SelectedThreadID != "" || first.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected victim surface to become unbound, got selected=%q route=%q", first.SelectedThreadID, first.RouteMode)
	}
	if second.SelectedThreadID != "thread-1" || second.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected requesting surface to claim thread-1, got selected=%q route=%q", second.SelectedThreadID, second.RouteMode)
	}
	var sawVictimNotice, sawWinnerNotice bool
	for _, event := range confirm {
		if event.SurfaceSessionID == "surface-1" && event.Notice != nil && event.Notice.Code == "thread_claim_lost" {
			sawVictimNotice = true
		}
		if event.SurfaceSessionID == "surface-2" && event.Notice != nil && event.Notice.Code == "thread_kicked" {
			sawWinnerNotice = true
		}
	}
	if !sawVictimNotice || !sawWinnerNotice {
		t.Fatalf("expected kick notices for both surfaces, got %#v", confirm)
	}
}

func TestUseBusyRunningThreadRejectsKick(t *testing.T) {
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
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		ProductMode:        state.ProductModeVSCode,
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_busy_running" {
		t.Fatalf("expected busy running thread to reject kick, got %#v", events)
	}
}

func TestNormalModeListWithoutOnlineWorkspacesReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one notice event, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "no_available_workspaces" {
		t.Fatalf("expected no_available_workspaces notice, got %#v", events[0])
	}
	if !strings.Contains(events[0].Notice.Text, "当前没有可接管的工作区") {
		t.Fatalf("expected workspace empty state notice, got %#v", events[0].Notice)
	}
}

func TestVSCodeModeListWithoutOnlineInstancesReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "no_online_instances" {
		t.Fatalf("expected no_online_instances notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "当前没有在线 VS Code 实例") {
		t.Fatalf("expected vscode-specific empty state notice, got %#v", events[0].Notice)
	}
}

func TestStatusMaterializesSurfaceWithDefaultNormalMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Snapshot == nil {
		t.Fatalf("expected one snapshot event, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected surface to be materialized")
	}
	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected default product mode normal, got %q", surface.ProductMode)
	}
	if events[0].Snapshot.ProductMode != "normal" {
		t.Fatalf("expected snapshot product mode normal, got %#v", events[0].Snapshot)
	}
}

func TestModeCommandSwitchesDetachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected product mode vscode, got %q", surface.ProductMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected detached unbound surface after mode switch, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected prompt override to be cleared, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
}

func TestModeCommandDetachesIdleAttachmentBeforeSwitching(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 10, 0, 0, time.UTC)
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected product mode vscode, got %q", surface.ProductMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected mode switch to detach attached surface, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
}

func TestModeCommandCancelsPendingHeadlessBeforeSwitching(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 11, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	start := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	if len(start) != 2 || start[1].DaemonCommand == nil || start[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected detached /use to start headless launch, got %#v", start)
	}
	pending := svc.root.Surfaces["surface-1"].PendingHeadless
	if pending == nil {
		t.Fatal("expected pending headless before mode switch")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeVSCode {
		t.Fatalf("expected product mode vscode, got %q", surface.ProductMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound || surface.PendingHeadless != nil {
		t.Fatalf("expected mode switch to fully clear pending headless state, got %#v", surface)
	}
	if len(events) != 2 || events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless || events[1].Notice == nil || events[1].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected kill + switched notice, got %#v", events)
	}
	if events[0].DaemonCommand.InstanceID != pending.InstanceID || events[0].DaemonCommand.ThreadID != pending.ThreadID {
		t.Fatalf("expected mode switch to kill pending headless launch, got %#v", events[0].DaemonCommand)
	}
}

func TestModeCommandRejectsSwitchWhileWorkIsRunning(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 15, 0, 0, time.UTC)
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode vscode",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected mode to remain normal while busy, got %q", surface.ProductMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_busy" {
		t.Fatalf("expected surface_mode_busy notice, got %#v", events)
	}
}

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

func TestVSCodeAttachCanSwitchInstanceWithoutDetach(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 13, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})
	svc.root.Surfaces["surface-1"].PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-2",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-2" || surface.SelectedThreadID != "thread-2" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected vscode attach switch to rebind follow-local on new instance, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected attach switch to clear prompt override, got %#v", surface.PromptOverride)
	}
	if len(surface.StagedImages) != 0 {
		t.Fatalf("expected attach switch to discard staged images, got %#v", surface.StagedImages)
	}
	if svc.instanceClaimSurface("inst-1") != nil || svc.instanceClaimSurface("inst-2") == nil {
		t.Fatalf("expected instance claim to move to switched target")
	}
	var sawSwitchNotice bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "已切换到") {
			sawSwitchNotice = true
		}
	}
	if !sawSwitchNotice {
		t.Fatalf("expected switch notice, got %#v", events)
	}
}

func TestShowAllThreadsDisablesWorkspaceClaimedThreadInNormalMode(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 15, 0, 0, time.UTC)
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
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	var found bool
	for _, option := range events[0].SelectionPrompt.Options {
		if option.OptionID != "thread-2" {
			continue
		}
		found = true
		if !option.Disabled || option.ButtonLabel != "droid · 整理日志" || !strings.Contains(option.Subtitle, "workspace 已被其他飞书会话接管") {
			t.Fatalf("expected thread in claimed workspace to be disabled, got %#v", option)
		}
	}
	if !found {
		t.Fatalf("expected claimed workspace thread to appear in prompt, got %#v", events[0].SelectionPrompt)
	}
}

func TestDetachReleasesWorkspaceClaim(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 20, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})

	surface := svc.root.Surfaces["surface-2"]
	if surface.AttachedInstanceID != "inst-2" || surface.ClaimedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected workspace claim to be released for second attach, got %#v", surface)
	}
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Code != "attached" {
		t.Fatalf("expected attach success after detach release, got %#v", events)
	}
}

func TestNormalModeListIncludesHeadlessWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
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

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one workspace selection prompt for headless-only runtime, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Kind != control.SelectionPromptAttachWorkspace || prompt.Title != "工作区列表" || prompt.Layout != "grouped_attach_workspace" {
		t.Fatalf("unexpected workspace prompt: %#v", prompt)
	}
	if len(prompt.Options) != 1 || prompt.Options[0].OptionID != "/data/dl/runtime/headless" {
		t.Fatalf("expected only headless workspace in list prompt, got %#v", prompt.Options)
	}
}

func TestVSCodeModeListHeadlessOnlyReturnsNoVSCodeNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
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
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "no_online_instances" {
		t.Fatalf("expected no_online_instances notice for headless-only runtime, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "当前没有在线 VS Code 实例") {
		t.Fatalf("expected vscode-specific empty state notice, got %#v", events[0].Notice)
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
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Title != "在线 VS Code 实例" || prompt.Layout != "grouped_attach_instance" {
		t.Fatalf("expected vscode attach prompt title, got %#v", prompt)
	}
	if len(prompt.Options) != 1 || prompt.Options[0].OptionID != "inst-vscode-1" {
		t.Fatalf("expected only vscode instance in list prompt, got %#v", prompt.Options)
	}
	if prompt.Options[0].MetaText != "等待 VS Code 焦点" {
		t.Fatalf("expected vscode instance prompt to show compact waiting meta, got %#v", prompt.Options[0])
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

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Layout != "grouped_attach_instance" || prompt.ContextTitle != "当前实例" {
		t.Fatalf("unexpected vscode instance prompt metadata: %#v", prompt)
	}
	if !strings.Contains(prompt.ContextText, "droid · 当前跟随中") || !strings.Contains(prompt.ContextText, "换实例才用 /list") {
		t.Fatalf("expected current instance summary, got %#v", prompt.ContextText)
	}
	if len(prompt.Options) != 3 {
		t.Fatalf("expected current instance to be summarized instead of listed, got %#v", prompt.Options)
	}
	if prompt.Options[0].OptionID != "inst-focus" || prompt.Options[0].ButtonLabel != "切换" || prompt.Options[0].MetaText != "2分前 · 当前焦点可跟随" {
		t.Fatalf("expected focused candidate first, got %#v", prompt.Options[0])
	}
	if prompt.Options[1].OptionID != "inst-wait" || prompt.Options[1].MetaText != "1小时前 · 等待 VS Code 焦点" {
		t.Fatalf("expected waiting candidate after focused one, got %#v", prompt.Options[1])
	}
	if prompt.Options[2].OptionID != "inst-busy" || !prompt.Options[2].Disabled || prompt.Options[2].MetaText != "30分前 · 当前被其他飞书会话接管" {
		t.Fatalf("expected busy instance in unavailable section, got %#v", prompt.Options[2])
	}
}
