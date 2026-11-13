package threadcatalogcontract

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type PersistedThreadCatalog interface {
	RecentThreads(limit int) ([]state.ThreadRecord, error)
	RecentWorkspaces(limit int) (map[string]time.Time, error)
	ThreadByID(threadID string) (*state.ThreadRecord, error)
}

type BackendAwarePersistedThreadCatalog interface {
	RecentThreadsForBackend(agentproto.Backend, int) ([]state.ThreadRecord, error)
	RecentWorkspacesForBackend(agentproto.Backend, int) (map[string]time.Time, error)
	ThreadByIDForBackend(agentproto.Backend, string) (*state.ThreadRecord, error)
}

// Optional read-only capability for current explicit Codex names. Consumers
// must gate it to BackendCodex; absent/blank names are not returned.
type ExplicitThreadNameCatalog interface {
	ExplicitThreadNames(threadIDs []string) (map[string]string, error)
}
