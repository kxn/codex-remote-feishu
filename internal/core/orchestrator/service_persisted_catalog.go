package orchestrator

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const persistedRecentWorkspaceLimit = 500

func (s *Service) RecentPersistedWorkspaces(limit int) map[string]time.Time {
	if s == nil || s.catalog == nil {
		return nil
	}
	return clonePersistedWorkspaceRecency(s.catalog.recentPersistedWorkspaces(limit))
}

func workspaceRecencyFromThreads(threads []state.ThreadRecord) map[string]time.Time {
	if len(threads) == 0 {
		return nil
	}
	workspaces := map[string]time.Time{}
	for i := range threads {
		thread := threads[i]
		workspaceKey := threadWorkspaceKeyFromRecord(&thread)
		if workspaceKey == "" || workspaceSelectionInternalProbeWorkspace(workspaceKey) {
			continue
		}
		if current, ok := workspaces[workspaceKey]; !ok || thread.LastUsedAt.After(current) {
			workspaces[workspaceKey] = thread.LastUsedAt
		}
	}
	return workspaces
}

func normalizePersistedWorkspaceRecency(raw map[string]time.Time) map[string]time.Time {
	if len(raw) == 0 {
		return nil
	}
	normalized := map[string]time.Time{}
	for workspaceKey, usedAt := range raw {
		workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
		if workspaceKey == "" || workspaceSelectionInternalProbeWorkspace(workspaceKey) {
			continue
		}
		if current, ok := normalized[workspaceKey]; !ok || usedAt.After(current) {
			normalized[workspaceKey] = usedAt
		}
	}
	return normalized
}

func clonePersistedThreads(threads []state.ThreadRecord) []state.ThreadRecord {
	if len(threads) == 0 {
		return nil
	}
	copied := make([]state.ThreadRecord, len(threads))
	copy(copied, threads)
	return copied
}

func clonePersistedWorkspaceRecency(workspaces map[string]time.Time) map[string]time.Time {
	if len(workspaces) == 0 {
		return nil
	}
	copied := make(map[string]time.Time, len(workspaces))
	for workspaceKey, usedAt := range workspaces {
		copied[workspaceKey] = usedAt
	}
	return copied
}
