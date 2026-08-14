package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeSteerAllReachesCommandHandler(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/steerall",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected single noop notice, got %#v", events)
	}
	if events[0].Notice.Code != "steer_all_noop" || strings.Contains(events[0].Notice.Text, "same-turn steer") {
		t.Fatalf("unexpected steerall notice: %#v", events[0].Notice)
	}
}

func TestBareCodexProfileIntentRejectedInClaudeBeforeOpeningCatalog(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected single rejection notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_rejected" || !strings.Contains(events[0].Notice.Text, "/mode codex") {
		t.Fatalf("unexpected rejection notice: %#v", events[0].Notice)
	}
}

func TestBareReviewIntentOpensClaudeReviewPage(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/review",
	})
	if len(events) != 1 || events[0].PageView == nil {
		t.Fatalf("expected Claude review page, got %#v", events)
	}
	if events[0].PageView.CommandID != control.FeishuCommandReview || events[0].PageView.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("unexpected Claude review page: %#v", events[0].PageView)
	}
}

func TestBareCodexProfileIntentRejectedInOpenCodeBeforeOpeningCatalog(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendOpenCode, "", "", "")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected single rejection notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_rejected" || !strings.Contains(events[0].Notice.Text, "/mode codex") {
		t.Fatalf("unexpected rejection notice: %#v", events[0].Notice)
	}
}

func TestUnknownSlashRejectedInOpenCodeBeforePromptDispatch(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", "normal", agentproto.BackendOpenCode, "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-opencode",
		DisplayName:             "repo",
		WorkspaceRoot:           "/data/dl/repo",
		WorkspaceKey:            "/data/dl/repo",
		ShortName:               "repo",
		Backend:                 agentproto.BackendOpenCode,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "OpenCode session", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-opencode"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	surface.ClaimedWorkspaceKey = "/data/dl/repo"
	svc.bindThreadClaim(surface, "inst-opencode", "thread-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-unknown-slash",
		Text:             "/init",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected unknown slash to be rejected before prompt dispatch, got %#v", events)
	}
	if events[0].Notice.Code != "command_rejected" || !strings.Contains(events[0].Notice.Text, "/init") || !strings.Contains(events[0].Notice.Text, "/help") {
		t.Fatalf("unexpected unknown slash notice: %#v", events[0].Notice)
	}
	for _, event := range events {
		if event.DaemonCommand != nil {
			t.Fatalf("unknown slash must not dispatch to OpenCode backend: %#v", events)
		}
	}
}
