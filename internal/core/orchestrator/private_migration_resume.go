package orchestrator

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PreparePrivateMigrationResume reuses the ordinary headless state transition;
// the host must supply only facts from the verified migration source plan and
// commit the resulting checkpoint and effects together.
func (s *Service) PreparePrivateMigrationResume(surfaceID, threadID, cwd string, backend agentproto.Backend, profileID string) ([]eventcontract.Event, error) {
	if !s.config.PrivateApplication || strings.TrimSpace(threadID) == "" || !filepath.IsAbs(cwd) || (backend != agentproto.BackendCodex && backend != agentproto.BackendClaude) {
		return nil, fmt.Errorf("invalid migration resume selection")
	}
	v := s.root.Surfaces[surfaceID]
	if v == nil || !s.privateMigrationReleased[surfaceID] || len(v.QueuedQueueItemIDs) == 0 || v.ActiveQueueItemID != "" || v.AttachedInstanceID != "" || v.PendingHeadless != nil {
		return nil, fmt.Errorf("migration surface is not ready")
	}
	if _, held := s.privateRecoveryHolds[surfaceID]; held {
		return nil, fmt.Errorf("migration surface remains held")
	}
	if v.Backend != backend || (backend == agentproto.BackendCodex && v.CodexProfileID != profileID) || (backend == agentproto.BackendClaude && v.ClaudeProfileID != profileID) {
		return nil, fmt.Errorf("migration profile changed")
	}
	view := &mergedThreadView{ThreadID: threadID, Backend: backend, Thread: &state.ThreadRecord{ThreadID: threadID, CWD: cwd, WorkspaceKey: cwd}}
	events := s.startHeadlessForResolvedThreadWithProfilePolicy(v, view, startHeadlessModeHeadlessRestore, surfaceOverlayRouteCleanupOptions{}, false)
	if v.PendingHeadless == nil || v.PendingHeadless.ThreadID != threadID || v.PendingHeadless.ThreadCWD != cwd || v.PendingHeadless.Backend != backend {
		return nil, fmt.Errorf("migration headless continuation unavailable")
	}
	return events, nil
}

// RestorePrivateMigrationHeadlessProjection reinstates only the exact frozen
// pending launch matched to a committed migration effect; it starts no process.
func (s *Service) RestorePrivateMigrationHeadlessProjection(surfaceID, instanceID, threadID, cwd string, backend agentproto.Backend) error {
	v := s.root.Surfaces[surfaceID]
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		if v != nil && v.PendingHeadless != nil && v.PendingHeadless.InstanceID == instanceID {
			return nil
		}
		return fmt.Errorf("migration headless evidence absent")
	}
	if v == nil || !s.privateMigrationReleased[surfaceID] || len(held.Holds) != 1 || held.Holds[0].Kind != "headless_launch" || v.PendingHeadless != nil {
		return fmt.Errorf("migration headless remains held")
	}
	var launch state.HeadlessLaunchRecord
	if json.Unmarshal(held.Holds[0].Frozen, &launch) != nil || launch.InstanceID != instanceID || launch.ThreadID != threadID || launch.ThreadCWD != cwd || launch.Backend != backend {
		return fmt.Errorf("migration headless evidence mismatch")
	}
	v.PendingHeadless = &launch
	delete(s.privateRecoveryHolds, surfaceID)
	v.DispatchMode = state.DispatchModeNormal
	return nil
}
