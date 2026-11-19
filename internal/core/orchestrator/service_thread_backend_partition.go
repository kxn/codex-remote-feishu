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
			viewsByID[thread.ThreadID] = &mergedThreadView{
				ThreadID: thread.ThreadID,
				Backend:  backend,
				Inst:     syntheticPersistedThreadInstance(&thread, backend),
				Thread:   cloneThreadRecord(&thread),
			}
			continue
		}
		view.Thread = mergePersistedThreadName(view.Thread, &thread, view.Backend)
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

func (s *Service) normalModeThreadBackend(surface *state.SurfaceConsoleRecord) (agentproto.Backend, bool) {
	if !s.surfaceIsHeadless(surface) {
		return "", false
	}
	return s.surfaceBackend(surface), true
}
