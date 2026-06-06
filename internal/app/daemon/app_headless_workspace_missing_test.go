package daemon

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestExplainMissingWorkspaceLaunchErrorReplacesOpaqueError(t *testing.T) {
	t.Parallel()

	missingDir := filepath.Join(t.TempDir(), "deleted-workspace")
	rawErr := errors.New(`fork/exec D:\Research\codex-remote-feishu\codex-remote.exe: The directory name is invalid.`)

	got := explainMissingWorkspaceLaunchError(control.DaemonCommand{
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	}, missingDir, rawErr)

	var info agentproto.ErrorInfo
	if !errors.As(got, &info) {
		t.Fatalf("expected ErrorInfo, got %T: %v", got, got)
	}
	if info.Code != "headless_workspace_dir_missing" {
		t.Fatalf("unexpected code: %q", info.Code)
	}
	if !strings.Contains(info.Message, missingDir) || !strings.Contains(info.Message, "工作区目录已不存在") {
		t.Fatalf("message does not explain the missing directory: %q", info.Message)
	}
	if !strings.Contains(info.Details, "directory name is invalid") {
		t.Fatalf("expected raw error preserved in details, got %q", info.Details)
	}
}

func TestExplainMissingWorkspaceLaunchErrorKeepsErrorWhenDirExists(t *testing.T) {
	t.Parallel()

	existingDir := t.TempDir()
	rawErr := errors.New("some other launch failure")

	got := explainMissingWorkspaceLaunchError(control.DaemonCommand{
		SurfaceSessionID: "surface-1",
	}, existingDir, rawErr)

	if got != rawErr {
		t.Fatalf("expected original error to be preserved when workspace exists, got %v", got)
	}

	// An empty workdir is not a missing-directory case either.
	if got := explainMissingWorkspaceLaunchError(control.DaemonCommand{}, "", rawErr); got != rawErr {
		t.Fatalf("expected original error for empty workDir, got %v", got)
	}
}
