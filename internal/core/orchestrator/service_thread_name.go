package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Project all loaded identities, including unselected old threads outside the
// recent-list window, without replacing runtime state or querying per thread.
func (s *Service) persistedThreadNames(backend agentproto.Backend, threads map[string]*state.ThreadRecord, selectedID string) map[string]string {
	ids := make([]string, 0, len(threads)+1)
	for id, thread := range threads {
		if thread != nil {
			ids = append(ids, id)
		}
	}
	if threads[selectedID] == nil && selectedID != "" {
		ids = append(ids, selectedID)
	}
	sort.Strings(ids)
	return s.catalog.explicitThreadNames(backend, ids)
}

func threadWithPersistedName(thread *state.ThreadRecord, names map[string]string) *state.ThreadRecord {
	if thread == nil || names[thread.ThreadID] == "" || names[thread.ThreadID] == thread.Name {
		return thread
	}
	projected := *thread
	projected.Name = names[thread.ThreadID]
	return &projected
}

// An explicit desktop rename does not share activity recency with other metadata.
func mergePersistedThreadName(current, persisted *state.ThreadRecord, backend agentproto.Backend) *state.ThreadRecord {
	merged := mergeThreadMetadata(current, persisted)
	if merged != nil && persisted != nil && agentproto.NormalizeBackend(backend) == agentproto.BackendCodex {
		if name := strings.TrimSpace(persisted.PersistedName); name != "" {
			merged.Name = name
		}
	}
	return merged
}
