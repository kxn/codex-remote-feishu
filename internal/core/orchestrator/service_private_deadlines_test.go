package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
	"time"
)

func TestPrivateExitedFreshLaunchAllowsNewSameWorkspace(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.MaterializeSurfaceResumeContract("surface", "gateway", "chat", "user", state.HeadlessCodexSurfaceBackendContract(""), "", "")
	surface := s.root.Surfaces["surface"]
	cwd := t.TempDir()
	events := s.startFreshWorkspaceHeadlessWithOptions(surface, cwd, true)
	if len(events) == 0 || surface.PendingHeadless == nil {
		t.Fatal("first launch missing")
	}
	old := surface.PendingHeadless.InstanceID
	if got := s.PrivateOwnerLaunchExited("surface", "different"); len(got) != 0 || surface.PendingHeadless == nil {
		t.Fatal("other instance cleared pending launch")
	}
	events = s.PrivateOwnerLaunchExited("surface", old)
	if len(events) == 0 || surface.PendingHeadless != nil {
		t.Fatal("exact failed launch not cleared")
	}
	events = s.startFreshWorkspaceHeadlessWithOptions(surface, cwd, true)
	if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID == old {
		t.Fatal("new same-workspace launch blocked")
	}
	found := false
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			found = true
		}
	}
	if !found {
		t.Fatal("replacement launch not scheduled")
	}
}
