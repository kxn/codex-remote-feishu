package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestModeCommandSwitchesDetachedSurfaceToClaude(t *testing.T) {
	now := time.Date(2026, 4, 28, 6, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode claude",
	})

	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected product mode normal, got %q", surface.ProductMode)
	}
	if surface.Backend != agentproto.BackendClaude {
		t.Fatalf("expected claude backend after switch, got %q", surface.Backend)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected detached unbound surface after claude switch, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected prompt override to be cleared, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "claude") {
		t.Fatalf("expected claude switch notice, got %#v", events[0].Notice)
	}
}

func TestModeCommandNormalAliasReturnsSurfaceToCodex(t *testing.T) {
	now := time.Date(2026, 4, 28, 6, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode claude",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode normal",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected product mode normal, got %q", surface.ProductMode)
	}
	if surface.Backend != agentproto.BackendCodex {
		t.Fatalf("expected normal alias to restore codex backend, got %q", surface.Backend)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "codex") {
		t.Fatalf("expected codex switch notice, got %#v", events[0].Notice)
	}
}

func TestModeCommandSwitchesCurrentWorkspaceToClaudeAndPreparesHeadless(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-codex",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendCodex,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaudeProfileID = "devseek"
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})

	if surface.ProductMode != state.ProductModeNormal || surface.Backend != agentproto.BackendClaude {
		t.Fatalf("expected normal claude surface after switch, got %#v", surface)
	}
	if surface.AttachedInstanceID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected codex attachment cleared before claude prep, got %#v", surface)
	}
	if surface.PendingHeadless == nil || !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") {
		t.Fatalf("expected claude mode switch to prepare workspace headless, got %#v", surface.PendingHeadless)
	}
	if !surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected claude mode switch to preserve new-thread-ready intent, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.ClaudeProfileID != "devseek" {
		t.Fatalf("expected pending headless to keep current claude profile, got %#v", surface.PendingHeadless)
	}
	if !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected workspace claim to be preserved, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected prompt override to be cleared, got %#v", surface.PromptOverride)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + workspace prep notice + daemon command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice first, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "workspace_create_starting" {
		t.Fatalf("expected workspace_create_starting notice second, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command third, got %#v", events)
	}
	if events[2].DaemonCommand.ClaudeProfileID != "devseek" {
		t.Fatalf("expected start headless daemon command to carry current claude profile, got %#v", events[2].DaemonCommand)
	}
}

func TestModeCommandSwitchesCurrentWorkspaceToClaudeExistingWorkspaceAndPreparesNewThreadReady(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-codex",
		DisplayName:             "repo-codex",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-codex",
		Backend:                 agentproto.BackendCodex,
		Online:                  true,
		ObservedFocusedThreadID: "thread-codex",
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-claude",
		DisplayName:             "repo-claude",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-claude",
		Backend:                 agentproto.BackendClaude,
		Online:                  true,
		ObservedFocusedThreadID: "thread-claude",
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaudeProfileID = "devseek"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})

	if surface.ProductMode != state.ProductModeNormal || surface.Backend != agentproto.BackendClaude {
		t.Fatalf("expected normal claude surface after switch, got %#v", surface)
	}
	if surface.AttachedInstanceID != "" || surface.PendingHeadless == nil {
		t.Fatalf("expected profile-mismatched claude workspace to start matching headless, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected backend switch fresh-start path to stay unbound until launch completes, got %#v", surface)
	}
	if !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") || !surface.PendingHeadless.PrepareNewThread || !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected backend switch to preserve workspace/new-thread intent in pending headless, got %#v", surface)
	}
	if surface.PreparedFromThreadID != "" || surface.PreparedThreadCWD != "" {
		t.Fatalf("expected fresh-start path not to pre-bind prepared thread route before launch, got %#v", surface)
	}

	var sawSwitchNotice, sawWorkspaceStarting bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "surface_mode_switched" {
			sawSwitchNotice = true
		}
		if event.Notice != nil && event.Notice.Code == "workspace_create_starting" {
			sawWorkspaceStarting = true
		}
	}
	if !sawSwitchNotice || !sawWorkspaceStarting {
		t.Fatalf("expected switch notice + fresh-start notice, got %#v", events)
	}
}

func TestModeCommandSwitchesClaudeWorkspaceBackToCodexAndPreparesNewThreadReady(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-claude",
		DisplayName:             "repo-claude",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-claude",
		Backend:                 agentproto.BackendClaude,
		Online:                  true,
		ObservedFocusedThreadID: "thread-claude",
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-codex",
		DisplayName:             "repo-codex",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-codex",
		Backend:                 agentproto.BackendCodex,
		Online:                  true,
		ObservedFocusedThreadID: "thread-codex",
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-claude",
	})
	surface := svc.root.Surfaces["surface-1"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode normal",
	})

	if surface.ProductMode != state.ProductModeNormal || surface.Backend != agentproto.BackendCodex {
		t.Fatalf("expected normal codex surface after switch, got %#v", surface)
	}
	if surface.AttachedInstanceID != "inst-codex" || surface.PendingHeadless != nil {
		t.Fatalf("expected compatible codex workspace to attach directly, got %#v", surface)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeNewThreadReady {
		t.Fatalf("expected compatible codex workspace switch to land in new_thread_ready, got %#v", surface)
	}
	if !strings.EqualFold(surface.PreparedThreadCWD, "/data/dl/repo") || !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected prepared/claimed workspace to stay on repo, got %#v", surface)
	}
	if surface.PreparedFromThreadID != "" {
		t.Fatalf("expected backend switch to drop cross-backend session binding, got %#v", surface)
	}

	var sawSwitchNotice, sawPreparedSelection, sawNewThreadReady bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "surface_mode_switched" {
			sawSwitchNotice = true
		}
		if event.ThreadSelection != nil && event.ThreadSelection.RouteMode == string(state.RouteModeNewThreadReady) && event.ThreadSelection.Title == preparedNewThreadSelectionTitle() {
			sawPreparedSelection = true
		}
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawNewThreadReady = true
		}
	}
	if !sawSwitchNotice || !sawPreparedSelection || !sawNewThreadReady {
		t.Fatalf("expected switch notice + prepared selection + new_thread_ready, got %#v", events)
	}
}

func TestClaudeModeFirstTurnBindsCreatedThreadForFollowupText(t *testing.T) {
	now := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-codex",
		DisplayName:             "repo-codex",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-codex",
		Backend:                 agentproto.BackendCodex,
		Online:                  true,
		ObservedFocusedThreadID: "thread-codex",
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-claude",
		DisplayName:             "repo-claude",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-claude",
		Backend:                 agentproto.BackendClaude,
		Online:                  true,
		ObservedFocusedThreadID: "thread-claude",
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-claude" || surface.RouteMode != state.RouteModeNewThreadReady {
		t.Fatalf("expected claude mode switch to attach compatible visible workspace in new-thread-ready state, got %#v", surface)
	}

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	commandID := "cmd-1"
	sawPrompt := false
	for _, event := range first {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawPrompt = true
		}
	}
	if !sawPrompt {
		t.Fatalf("expected first text to dispatch a prompt, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", commandID)

	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		CommandID: commandID,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:      agentproto.EventItemStarted,
		CommandID: commandID,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		ItemID:    "item-1",
		ItemKind:  "agent_message",
	})
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		CommandID: commandID,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		ItemID:    "item-1",
		ItemKind:  "agent_message",
		Status:    "completed",
		Metadata:  map[string]any{"text": "你好"},
	})
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            commandID,
		ThreadID:             "thread-created",
		TurnID:               "turn-1",
		Status:               "completed",
		Initiator:            agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
	})

	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-created" {
		t.Fatalf("expected completed first claude turn to bind created thread, got %#v", surface)
	}

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "继续",
	})
	sawResume := false
	for _, event := range second {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend &&
			event.Command.Target.ThreadID == "thread-created" &&
			event.Command.Target.ExecutionMode == agentproto.PromptExecutionModeResumeExisting &&
			!event.Command.Target.CreateThreadIfMissing {
			sawResume = true
		}
		if event.Notice != nil && event.Notice.Code == "thread_not_ready" {
			t.Fatalf("expected second text to stay on created thread, got %#v", second)
		}
	}
	if !sawResume {
		t.Fatalf("expected second text to resume created thread, got %#v", second)
	}
}

func TestClaudeModeFirstTurnBindingSurvivesLauncherMenuActions(t *testing.T) {
	now := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-codex",
		DisplayName:             "repo-codex",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-codex",
		Backend:                 agentproto.BackendCodex,
		Online:                  true,
		ObservedFocusedThreadID: "thread-codex",
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-claude",
		DisplayName:             "repo-claude",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo-claude",
		Backend:                 agentproto.BackendClaude,
		Online:                  true,
		ObservedFocusedThreadID: "thread-claude",
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})
	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	commandID := "cmd-1"
	sawPrompt := false
	for _, event := range first {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawPrompt = true
		}
	}
	if !sawPrompt {
		t.Fatalf("expected first text to dispatch a prompt, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", commandID)
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		CommandID: commandID,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:      agentproto.EventItemStarted,
		CommandID: commandID,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		ItemID:    "item-1",
		ItemKind:  "agent_message",
	})
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		CommandID: commandID,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		ItemID:    "item-1",
		ItemKind:  "agent_message",
		Status:    "completed",
		Metadata:  map[string]any{"text": "你好"},
	})
	svc.ApplyAgentEvent("inst-claude", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            commandID,
		ThreadID:             "thread-created",
		TurnID:               "turn-1",
		Status:               "completed",
		Initiator:            agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		MessageID:        "menu-1",
		Text:             "/menu send_settings",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		MessageID:        "menu-2",
		Text:             "/verbose",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-created" {
		t.Fatalf("expected launcher menu actions not to clear created thread binding, got %#v", surface)
	}

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "继续",
	})
	for _, event := range second {
		if event.Notice != nil && event.Notice.Code == "thread_not_ready" {
			t.Fatalf("expected second text to stay sendable after launcher menu actions, got %#v", second)
		}
	}
}
