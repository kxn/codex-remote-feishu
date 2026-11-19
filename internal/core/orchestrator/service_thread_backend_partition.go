package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) mergePersistedRecentThreadsForBackend(viewsByID map[string]*mergedThreadView, backend agentproto.Backend, filterByBackend bool) {
	if s == nil || !s.catalog.hasPersistedThreads() {
		return
	}
	threads := s.catalog.recentPersistedThreads(persistedRecentThreadLimit)
	if filterByBackend {
		threads = s.catalog.recentPersistedThreadsForBackend(backend, persistedRecentThreadLimit)
	}
	for i := range threads {
		thread := threads[i]
		if strings.TrimSpace(thread.ThreadID) == "" || !ordinaryThreadVisible(&thread) {
			continue
		}
		view := viewsByID[thread.ThreadID]
		if view == nil {
			merged := s.mergePersistedThreadMetadata(nil, &thread, agentproto.NormalizeBackend(backend))
			viewsByID[thread.ThreadID] = &mergedThreadView{
				ThreadID: thread.ThreadID,
				Backend:  backend,
				Inst:     syntheticPersistedThreadInstance(merged, backend),
				Thread:   merged,
			}
			continue
		}
		view.Thread = s.mergePersistedThreadMetadata(view.Thread, &thread, view.Backend)
	}
}

// Global native lists may label copied threads with the reporting instance's
// workspace. Resolve a newer saved cwd against explicit roots after merging.
func (s *Service) mergePersistedThreadMetadata(current, persisted *state.ThreadRecord, backend agentproto.Backend) *state.ThreadRecord {
	merged := mergeThreadMetadata(current, persisted)
	if merged == nil || persisted == nil || strings.TrimSpace(persisted.CWD) == "" ||
		strings.TrimSpace(persisted.WorkspaceKey) != "" || !shouldPromoteMergedThreadMetadata(current, persisted) {
		return merged
	}
	cwd := normalizeWorkspaceClaimKey(persisted.CWD)
	workspaceKey := ""
	consider := func(key string) {
		key = normalizeWorkspaceClaimKey(key)
		if key == "" || (cwd != key && !strings.HasPrefix(cwd, key+"/")) {
			return
		}
		if len(key) > len(workspaceKey) {
			workspaceKey = key
		}
	}
	consider(merged.WorkspaceKey)
	// SQLite RecentWorkspaces contains raw CWD values, not explicit roots.
	for _, inst := range s.root.Instances {
		if inst != nil && state.EffectiveInstanceBackend(inst) == backend {
			consider(instanceWorkspaceClaimKey(inst))
		}
	}
	for _, surface := range s.root.Surfaces {
		if surface != nil && s.surfaceUsesWorkspaceClaims(surface) && s.surfaceBackend(surface) == backend {
			consider(s.surfaceCurrentWorkspaceKey(surface))
		}
	}
	if workspaceKey == "" {
		workspaceKey = cwd
	}
	merged.WorkspaceKey = workspaceKey
	return merged
}

func (s *Service) normalModeThreadBackend(surface *state.SurfaceConsoleRecord) (agentproto.Backend, bool) {
	if !s.surfaceIsHeadless(surface) {
		return "", false
	}
	return s.surfaceBackend(surface), true
}
