package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateWorkspaceNewDiscardOutcomeAndInvalidSettings(t *testing.T) {
	s, req := privateUpgradeResumeFixture(t, false)
	v := s.Surface("s")
	before := s.PrivateInputDiscardVersion("s")
	events := s.enterTargetPickerNewThread(v, req.CWD)
	if len(events) == 0 || s.PrivateInputDiscardVersion("s") == before {
		t.Fatalf("successful new target did not report discard: %+v", events)
	}
	s, req = privateUpgradeResumeFixture(t, false)
	v = s.Surface("s")
	s.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("g")] = state.BotCapabilitySettingsRecord{}
	before = s.PrivateInputDiscardVersion("s")
	queued := len(v.QueuedQueueItemIDs)
	images := len(v.StagedImages)
	events = s.startWorkspaceHeadlessLaunchWithOverlayCleanup(v, req.CWD, workspaceHeadlessLaunchOptions{PrepareNewThread: true})
	if len(events) == 0 || s.PrivateInputDiscardVersion("s") != before || len(v.QueuedQueueItemIDs) != queued || len(v.StagedImages) != images {
		t.Fatalf("rejected settings discarded inputs: %+v", events)
	}
}
