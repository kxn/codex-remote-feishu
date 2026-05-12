package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestAttachWorkspaceReusesCompatibleManagedHeadless(t *testing.T) {
	now := time.Date(2026, 5, 1, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessCodexSurfaceBackendContract("default"), "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-headless-1",
		DisplayName:     "repo",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/repo",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-headless-1" || !testutil.SamePath(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected attach workspace to reuse compatible managed headless, got %#v", surface)
	}
	if surface.PendingHeadless != nil || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected managed headless reuse to attach immediately and stay unbound, got %#v", surface)
	}
	var sawAttached bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "workspace_attached" {
			sawAttached = true
			break
		}
	}
	if !sawAttached {
		t.Fatalf("expected reused managed headless to report workspace attached, got %#v", events)
	}
}

func TestModeCommandSwitchesCurrentWorkspaceToClaudeAndRestartsManagedMismatch(t *testing.T) {
	now := time.Date(2026, 5, 1, 15, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-codex",
		DisplayName:     "repo-codex",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo-codex",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude-old",
		DisplayName:     "repo-claude-old",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo-claude-old",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-b",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		Threads:         map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaudeProfileID = "profile-a"

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
		t.Fatalf("expected managed mismatch to restart into pending headless, got %#v", surface)
	}
	if !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") || !surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected restart path to preserve workspace/new-thread intent, got %#v", surface.PendingHeadless)
	}
	var sawKillOld, sawWorkspaceStarting, sawStartNew bool
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless && event.DaemonCommand.InstanceID == "inst-claude-old" {
			sawKillOld = true
		}
		if event.Notice != nil && event.Notice.Code == "workspace_create_starting" {
			sawWorkspaceStarting = true
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			sawStartNew = true
		}
	}
	if !sawKillOld || !sawWorkspaceStarting || !sawStartNew {
		t.Fatalf("expected backend switch to kill mismatched managed headless and start replacement, got %#v", events)
	}
}
