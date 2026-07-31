package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

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
		InstanceID:            strings.TrimSpace(command.InstanceID),
		ThreadID:              strings.TrimSpace(command.ThreadID),
		ThreadTitle:           strings.TrimSpace(command.ThreadTitle),
		WorkspaceKey:          strings.TrimSpace(command.WorkspaceKey),
		ThreadCWD:             strings.TrimSpace(command.ThreadCWD),
		Backend:               agentproto.NormalizeBackend(command.Backend),
		CodexProviderID:       state.NormalizeCodexProviderID(command.CodexProviderID),
		ClaudeProfileID:       state.NormalizeClaudeProfileID(command.ClaudeProfileID),
		ClaudeReasoningEffort: strings.TrimSpace(command.ClaudeReasoningEffort),
		RequestedAt:           now,
		ExpiresAt:             now.Add(30 * time.Second),
		Status:                state.HeadlessLaunchStarting,
		Purpose:               state.HeadlessLaunchPurposeThreadRestore,
		AutoRestore:           command.AutoRestore,
	}
}
