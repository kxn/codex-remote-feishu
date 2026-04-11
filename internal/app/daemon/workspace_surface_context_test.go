package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestWorkspaceSurfaceContextWrittenAndRemovedForNormalMode(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, ".git", "info"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git/info): %v", err)
	}
	app := New("127.0.0.1:0", "127.0.0.1:0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "vscode",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	contextPath := workspaceSurfaceContextPath(workspaceRoot)
	payload, err := readWorkspaceSurfaceContext(contextPath)
	if err != nil {
		t.Fatalf("readWorkspaceSurfaceContext() error = %v", err)
	}
	if payload.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected surface context payload: %#v", payload)
	}
	excludeRaw, err := os.ReadFile(filepath.Join(workspaceRoot, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(excludeRaw), "/.codex-remote/") {
		t.Fatalf("expected local git exclude entry, got %q", string(excludeRaw))
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if _, err := os.Stat(contextPath); !os.IsNotExist(err) {
		t.Fatalf("expected context file removed after detach, stat err=%v", err)
	}
}

func TestWorkspaceSurfaceContextNotWrittenForVSCodeMode(t *testing.T) {
	workspaceRoot := t.TempDir()
	app := New("127.0.0.1:0", "127.0.0.1:0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "vscode",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	if _, err := os.Stat(workspaceSurfaceContextPath(workspaceRoot)); !os.IsNotExist(err) {
		t.Fatalf("expected no workspace context file in vscode mode, stat err=%v", err)
	}
}
