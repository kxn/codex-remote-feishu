package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestResolveWorkspaceAttachInstanceForCodexProviderFiltersMismatchedInstance(t *testing.T) {
	now := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-default",
		DisplayName:     "repo-default",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: state.DefaultCodexProviderID,
		Online:          true,
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-proxy",
		DisplayName:     "repo-proxy",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "team-proxy",
		Online:          true,
	})

	inst := svc.resolveWorkspaceAttachInstanceForBackend(svc.root.Surfaces["surface-1"], "/data/dl/repo", agentproto.BackendCodex)
	if inst == nil || inst.InstanceID != "inst-proxy" {
		t.Fatalf("expected provider-matched instance, got %#v", inst)
	}
}
