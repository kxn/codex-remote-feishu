package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleGatewayActionReplacesWorkspaceNavigationCardsInline(t *testing.T) {
	tests := []struct {
		name          string
		kind          control.ActionKind
		text          string
		expectedTitle string
		requiresGit   bool
	}{
		{
			name:          "workspace root page",
			kind:          control.ActionWorkspaceRoot,
			text:          "/workspace",
			expectedTitle: "工作区与会话",
		},
		{
			name:          "workspace new page",
			kind:          control.ActionWorkspaceNew,
			text:          "/workspace new",
			expectedTitle: "新建工作区",
		},
		{
			name:          "workspace list business card",
			kind:          control.ActionWorkspaceList,
			text:          "/workspace list",
			expectedTitle: "切换工作区与会话",
		},
		{
			name:          "workspace new dir business card",
			kind:          control.ActionWorkspaceNewDir,
			text:          "/workspace new dir",
			expectedTitle: "从目录新建工作区",
		},
		{
			name:          "workspace new git business card",
			kind:          control.ActionWorkspaceNewGit,
			text:          "/workspace new git",
			expectedTitle: "从 GIT URL 新建工作区",
			requiresGit:   true,
		},
		{
			name:          "workspace new worktree business card",
			kind:          control.ActionWorkspaceNewWorktree,
			text:          "/workspace new worktree",
			expectedTitle: "从 Worktree 新建工作区",
			requiresGit:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.requiresGit && !gitExecutableAvailable() {
				t.Skip("git unavailable in test environment")
			}

			gateway := &recordingGateway{}
			app := New(":0", ":0", gateway, agentproto.ServerIdentity{
				PID:       42,
				StartedAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
			})
			app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
			app.service.UpsertInstance(&state.InstanceRecord{
				InstanceID:    "inst-1",
				DisplayName:   "proj",
				WorkspaceRoot: "/data/dl/proj",
				WorkspaceKey:  "/data/dl/proj",
				ShortName:     "proj",
				Online:        true,
				Threads: map[string]*state.ThreadRecord{
					"thread-1": {
						ThreadID:   "thread-1",
						Name:       "会话-1",
						CWD:        "/data/dl/proj",
						LastUsedAt: time.Date(2026, 4, 22, 10, 1, 0, 0, time.UTC),
					},
				},
			})

			result := handleGatewayActionForTest(context.Background(), app, control.Action{
				Kind:             tt.kind,
				Text:             tt.text,
				GatewayID:        "app-1",
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
				MessageID:        "om-workspace-nav-1",
				Inbound: &control.ActionInboundMeta{
					CardDaemonLifecycleID: app.daemonLifecycleID,
				},
			})

			if result == nil || result.ReplaceCurrentCard == nil {
				t.Fatalf("expected inline replacement result, got %#v", result)
			}
			if len(gateway.operations) != 0 {
				t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
			}
			if result.ReplaceCurrentCard.CardTitle != tt.expectedTitle {
				t.Fatalf("unexpected replacement card title: got %q want %q", result.ReplaceCurrentCard.CardTitle, tt.expectedTitle)
			}
		})
	}
}
