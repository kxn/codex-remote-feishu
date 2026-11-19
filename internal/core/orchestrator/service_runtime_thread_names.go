package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/threadcatalogcontract"
)

func (r *serviceCatalogRuntime) explicitThreadNames(backend agentproto.Backend, threadIDs []string) map[string]string {
	backend = agentproto.NormalizeBackend(backend)
	if backend != agentproto.BackendCodex || !r.hasPersistedThreads() || len(threadIDs) == 0 {
		return nil
	}
	if catalog, ok := r.persistedThreads.(threadcatalogcontract.ExplicitThreadNameCatalog); ok {
		names, err := catalog.ExplicitThreadNames(threadIDs)
		if err != nil {
			return nil // never promote cached names after a failed fresh read
		}
		return names
	}
	// Older/custom catalog implementations can still resolve visible identities
	// exactly. The installed SQLite catalog uses the bounded batch path above.
	names := map[string]string{}
	for _, id := range threadIDs {
		thread, err := r.persistedThreadByIDForBackend(backend, id)
		if err == nil && thread != nil {
			if name := strings.TrimSpace(thread.PersistedName); name != "" {
				names[id] = name
			}
		}
	}
	return names
}
