package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// evalSymlinkForTest resolves symlinks in a path, which is needed on macOS
// where /var is a symlink to /private/var. This ensures test assertions
// match the paths produced by filepath.Clean in the daemon code.
func evalSymlinkForTest(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return resolved
}

// tempDirSuffixForTest extracts the temp dir suffix (e.g. "003") from a path.
// Used for cross-platform comparison where Windows 8.3 short names
// (RUNNER~1) differ from long names (runneradmin).
func tempDirSuffixForTest(t *testing.T, path string) string {
	t.Helper()
	clean := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(clean, "/")
	if len(parts) == 0 {
		t.Fatalf("empty path")
	}
	return parts[len(parts)-1]
}

func authorizePendingHeadlessForTest(t *testing.T, app *App, command control.DaemonCommand) {
	t.Helper()
	surfaceID := strings.TrimSpace(command.SurfaceSessionID)
	if surfaceID == "" {
		t.Fatal("test helper requires a surface-bound headless command")
	}
	surface := app.service.Surface(surfaceID)
	if surface == nil {
		t.Fatalf("surface %q is not materialized", surfaceID)
	}
	now := time.Now().UTC()
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:                strings.TrimSpace(command.InstanceID),
		ThreadID:                  strings.TrimSpace(command.ThreadID),
		ThreadTitle:               strings.TrimSpace(command.ThreadTitle),
		WorkspaceKey:              strings.TrimSpace(command.WorkspaceKey),
		ThreadCWD:                 strings.TrimSpace(command.ThreadCWD),
		Backend:                   agentproto.NormalizeBackend(command.Backend),
		CodexProfileID:            state.NormalizeCodexProfileID(command.CodexProfileID),
		ClaudeProfileID:           state.NormalizeClaudeProfileID(command.ClaudeProfileID),
		ClaudeReasoningEffort:     strings.TrimSpace(command.ClaudeReasoningEffort),
		OpenCodeProfileID:         state.NormalizeOpenCodeProfileID(command.OpenCodeProfileID),
		OpenCodeAdmissionRef:      state.NormalizeOpenCodeAdmissionRef(command.OpenCodeAdmissionRef),
		OpenCodeRuntimeAccessMode: state.NormalizeOpenCodeRuntimeAccessMode(command.OpenCodeRuntimeAccessMode),
		RequestedAt:               now,
		ExpiresAt:                 now.Add(30 * time.Second),
		Status:                    state.HeadlessLaunchStarting,
		Purpose:                   state.HeadlessLaunchPurposeThreadRestore,
		AutoRestore:               command.AutoRestore,
	}
}
