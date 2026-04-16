package orchestrator

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const persistedRecentWorkspaceLimit = 500

func (s *Service) recentPersistedThreads(limit int) []state.ThreadRecord {
	if s == nil || s.persistedThreads == nil {
		return nil
	}
	threads, err := s.persistedThreads.RecentThreads(limit)
	if err != nil {
		if len(s.persistedThreadsLast) == 0 {
			return nil
		}
		return clonePersistedThreads(s.persistedThreadsLast)
	}
	s.persistedThreadsLast = clonePersistedThreads(threads)
	return clonePersistedThreads(threads)
}

func (s *Service) recentPersistedWorkspaces(limit int) map[string]time.Time {
	if s == nil || s.persistedThreads == nil {
		return nil
	}
	workspaces, err := s.persistedThreads.RecentWorkspaces(limit)
	if err == nil {
		normalized := normalizePersistedWorkspaceRecency(workspaces)
		s.persistedWorkspaces = clonePersistedWorkspaceRecency(normalized)
		return normalized
	}
	if len(s.persistedWorkspaces) > 0 {
		return clonePersistedWorkspaceRecency(s.persistedWorkspaces)
	}
	return workspaceRecencyFromThreads(s.recentPersistedThreads(persistedRecentThreadLimit))
}

func (s *Service) RecentPersistedWorkspaces(limit int) map[string]time.Time {
	return clonePersistedWorkspaceRecency(s.recentPersistedWorkspaces(limit))
}

func workspaceRecencyFromThreads(threads []state.ThreadRecord) map[string]time.Time {
	if len(threads) == 0 {
		return nil
	}
	workspaces := map[string]time.Time{}
	for i := range threads {
		thread := threads[i]
		workspaceKey := normalizeWorkspaceClaimKey(thread.CWD)
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
