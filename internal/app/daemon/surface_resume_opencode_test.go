package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSurfaceResumeStatePersistsOpenCodeProfileID(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	app.service.MaterializeSurfaceResumeContract(
		"surface-1",
		"app-1",
		"chat-1",
		"user-1",
		state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)

	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.Backend != string(agentproto.BackendOpenCode) || entry.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected persisted opencode profile id, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	if got := restarted.service.SurfaceOpenCodeProfileID("surface-1"); got != "op_team" {
		t.Fatalf("expected opencode profile id restored after restart, got %q", got)
	}
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "normal" || snapshot.Backend != agentproto.BackendOpenCode {
		t.Fatalf("expected latent opencode surface after restart, got %#v", snapshot)
	}
}
