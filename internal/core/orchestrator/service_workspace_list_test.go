package orchestrator

import (
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestListWorkspacesUsesStableThreadWorkspaceKeysForBroadHeadlessPool(t *testing.T) {
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
				fmt.Sprintf("thread-fs-%d", i): {ThreadID: fmt.Sprintf("thread-fs-%d", i), Name: "atlas", WorkspaceKey: "/data/dl/atlas", CWD: "/data/dl/atlas/server", Loaded: true},
				fmt.Sprintf("thread-sf-%d", i): {ThreadID: fmt.Sprintf("thread-sf-%d", i), Name: "harbor", WorkspaceKey: "/data/dl/harbor", CWD: "/data/dl/harbor/web", Loaded: true},
			},
		})
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.Source != control.TargetPickerRequestSourceList || view.Title != "切换工作区与会话" {
		t.Fatalf("unexpected target picker view: %#v", view)
	}
	if len(view.WorkspaceOptions) != 2 {
		t.Fatalf("expected two real workspaces instead of broad instance root, got %#v", view.WorkspaceOptions)
	}
	got := map[string]bool{}
	for _, option := range view.WorkspaceOptions {
		if option.Synthetic {
			continue
		}
		got[option.Value] = true
	}
	if !got[testutil.WorkspacePath("data", "dl", "atlas")] || !got[testutil.WorkspacePath("data", "dl", "harbor")] || got[testutil.WorkspacePath("data", "dl")] {
		t.Fatalf("expected stable thread workspace roots only, got %#v", view.WorkspaceOptions)
	}
}

func TestListWorkspacesCollapsesMonorepoChildCWDsUnderStableWorkspaceRoot(t *testing.T) {
	now := time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "claw-port",
		WorkspaceRoot: "/data/dl/claw-port",
		WorkspaceKey:  "/data/dl/claw-port",
		ShortName:     "claw-port",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {
				ThreadID:      "thread-web",
				Name:          "web",
				WorkspaceKey:  "/data/dl/claw-port",
				CWD:           "/data/dl/claw-port/web",
				Loaded:        true,
				LastUsedAt:    now.Add(-time.Minute),
				RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle},
			},
			"thread-client": {
				ThreadID:      "thread-client",
				Name:          "client",
				WorkspaceKey:  "/data/dl/claw-port",
				CWD:           "/data/dl/claw-port/client",
				Loaded:        true,
				LastUsedAt:    now.Add(-2 * time.Minute),
				RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	got := map[string]bool{}
	for _, option := range view.WorkspaceOptions {
		if option.Synthetic {
			continue
		}
		got[option.Value] = true
	}
	if len(got) != 1 || !got["/data/dl/claw-port"] || got["/data/dl/claw-port/web"] || got["/data/dl/claw-port/client"] {
		t.Fatalf("expected monorepo child cwd values to collapse under stable root, got %#v", view.WorkspaceOptions)
	}
}
