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
	// Loaded identities outside the recent window still receive fresh names.
	ids := make([]string, 0, len(viewsByID))
	for id, view := range viewsByID {
		if agentproto.NormalizeBackend(view.Backend) == agentproto.BackendCodex {
			ids = append(ids, id)
		}
	}
	names := s.catalog.explicitThreadNames(agentproto.BackendCodex, ids)
	for _, view := range viewsByID {
		if agentproto.NormalizeBackend(view.Backend) == agentproto.BackendCodex {
			view.Thread = threadWithPersistedName(view.Thread, names)
		}
	}
}

// Global native lists may label copied threads with the reporting instance's
// workspace. Resolve a newer saved cwd against explicit roots after merging.
func (s *Service) mergePersistedThreadMetadata(current, persisted *state.ThreadRecord, backend agentproto.Backend) *state.ThreadRecord {
	merged := mergePersistedThreadName(current, persisted, backend)
	if merged == nil || persisted == nil || strings.TrimSpace(persisted.CWD) == "" ||
		strings.TrimSpace(persisted.WorkspaceKey) != "" || !shouldPromoteMergedThreadMetadata(current, persisted) {
		return merged
	}
	cwd := normalizeWorkspaceClaimKey(persisted.CWD)
	workspaceKey := ""
	hasThreadWorkspace := false
	nativeWorkspace := func(thread *state.ThreadRecord, key string) bool {
		return thread != nil && (thread.Loaded || normalizeWorkspaceClaimKey(thread.CWD) == normalizeWorkspaceClaimKey(key))
	}
	consider := func(key string, threadWorkspace bool) {
		key = normalizeWorkspaceClaimKey(key)
		if key == "" || (cwd != key && !strings.HasPrefix(cwd, key+"/")) {
			return
		}
		hasThreadWorkspace = hasThreadWorkspace || threadWorkspace
		if len(key) > len(workspaceKey) {
			workspaceKey = key
		}
	}
	// Preserve a stable root for an ordinary cwd change within that workspace.
	consider(merged.WorkspaceKey, nativeWorkspace(current, merged.WorkspaceKey))
	// SQLite RecentWorkspaces contains raw CWD values, not explicit roots.
	for _, inst := range s.root.Instances {
		if inst != nil && state.EffectiveInstanceBackend(inst) == backend {
			key := instanceWorkspaceClaimKey(inst)
			thread := inst.Threads[persisted.ThreadID]
			consider(key, threadBelongsToInstanceWorkspaceForTarget(inst, thread) && nativeWorkspace(thread, key))
		}
	}
	for _, surface := range s.root.Surfaces {
		if surface != nil && s.surfaceUsesWorkspaceClaims(surface) && s.surfaceBackend(surface) == backend {
			consider(s.surfaceCurrentWorkspaceKey(surface), surface.SelectedThreadID == persisted.ThreadID || surfaceFeishuRoomID(surface) != "")
		}
	}
	// An unrelated parent instance or a globally copied, unloaded thread may
	// carry an inherited workspace label. Prefer the saved cwd without evidence
	// that this thread belongs to one of the containing workspace roots.
	if !hasThreadWorkspace {
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
