package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleFileActionMaterializesIntoWorkspaceAndUpdatesGitExclude(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, ".git", "info"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git/info): %v", err)
	}
	incomingDir := t.TempDir()
	incomingPath := filepath.Join(incomingDir, "notes.txt")
	if err := os.WriteFile(incomingPath, []byte("hello file"), 0o600); err != nil {
		t.Fatalf("WriteFile(incoming): %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "repo",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "repo",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	app.handleAction(context.Background(), control.Action{
		Kind:             control.ActionFileMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-file-1",
		LocalPath:        incomingPath,
		FileName:         "notes.txt",
	})

	surface := app.service.Surface("surface-1")
	if surface == nil || len(surface.StagedFiles) != 1 {
		t.Fatalf("expected one staged file, got %#v", surface)
	}
	var staged *state.StagedFileRecord
	for _, file := range surface.StagedFiles {
		staged = file
	}
	if staged == nil {
		t.Fatal("expected staged file record")
	}
	wantPath := filepath.Join(workspaceRoot, ".codex-remote", "inbox", "feishu-files", "msg-file-1", "notes.txt")
	if staged.LocalPath != wantPath {
		t.Fatalf("unexpected staged file path: got %q want %q", staged.LocalPath, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected materialized file to exist: %v", err)
	}
	if _, err := os.Stat(incomingPath); !os.IsNotExist(err) {
		t.Fatalf("expected original temp file to be moved away, got err=%v", err)
	}
	rawExclude, err := os.ReadFile(filepath.Join(workspaceRoot, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("ReadFile(exclude): %v", err)
	}
	if !strings.Contains(string(rawExclude), "/.codex-remote/") {
		t.Fatalf("expected git exclude to contain .codex-remote rule, got %q", string(rawExclude))
	}
}

func TestHandleFileActionWithoutAttachmentCleansTempFile(t *testing.T) {
	incomingDir := t.TempDir()
	incomingPath := filepath.Join(incomingDir, "spec.pdf")
	if err := os.WriteFile(incomingPath, []byte("pdf"), 0o600); err != nil {
		t.Fatalf("WriteFile(incoming): %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{StartedAt: serverIdentityForTest().StartedAt})
	app.handleAction(context.Background(), control.Action{
		Kind:             control.ActionFileMessage,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-file-2",
		LocalPath:        incomingPath,
		FileName:         "spec.pdf",
	})

	if _, err := os.Stat(incomingPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be cleaned, got err=%v", err)
	}
	surface := app.service.Surface("surface-1")
	if surface == nil {
		t.Fatal("expected surface to be materialized for notice routing")
	}
	if len(surface.StagedFiles) != 0 {
		t.Fatalf("expected no staged files without attachment, got %#v", surface.StagedFiles)
	}
}
