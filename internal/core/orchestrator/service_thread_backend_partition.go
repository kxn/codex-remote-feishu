package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) mergePersistedRecentThreadsForBackend(viewsByID map[string]*mergedThreadView, backend agentproto.Backend, filterByBackend bool) {
	if s == nil || s.catalog.persistedThreads == nil {
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
				Inst:     syntheticPersistedThreadInstance(&thread),
				Thread:   cloneThreadRecord(&thread),
			}
			continue
		}
		view.Thread = mergeThreadMetadata(view.Thread, &thread)
	}
}

func (s *Service) normalModeThreadBackend(surface *state.SurfaceConsoleRecord) (agentproto.Backend, bool) {
	if surface == nil || s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return "", false
	}
	return s.surfaceBackend(surface), true
}
