package daemon

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/claudestate"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type backendAwarePersistedThreadCatalog interface {
	RecentThreadsForBackend(agentproto.Backend, int) ([]state.ThreadRecord, error)
	RecentWorkspacesForBackend(agentproto.Backend, int) (map[string]time.Time, error)
	ThreadByIDForBackend(agentproto.Backend, string) (*state.ThreadRecord, error)
}

type daemonPersistedThreadCatalog struct {
	codex  *codexstate.SQLiteThreadCatalog
	claude *claudestate.SessionCatalog
}

func newDaemonPersistedThreadCatalog(logf func(string, ...any)) (*daemonPersistedThreadCatalog, error) {
	codexCatalog, err := codexstate.NewDefaultSQLiteThreadCatalog(codexstate.SQLiteThreadCatalogOptions{Logf: logf})
	if err != nil {
		return nil, err
	}
	return &daemonPersistedThreadCatalog{
		codex:  codexCatalog,
		claude: claudestate.NewSessionCatalog(claudestate.SessionCatalogOptions{Logf: logf}),
	}, nil
}

func (c *daemonPersistedThreadCatalog) RecentThreads(limit int) ([]state.ThreadRecord, error) {
	return c.RecentThreadsForBackend(agentproto.BackendCodex, limit)
}

func (c *daemonPersistedThreadCatalog) RecentWorkspaces(limit int) (map[string]time.Time, error) {
	return c.RecentWorkspacesForBackend(agentproto.BackendCodex, limit)
}

func (c *daemonPersistedThreadCatalog) ThreadByID(threadID string) (*state.ThreadRecord, error) {
	return c.ThreadByIDForBackend(agentproto.BackendCodex, threadID)
}

func (c *daemonPersistedThreadCatalog) RecentThreadsForBackend(backend agentproto.Backend, limit int) ([]state.ThreadRecord, error) {
	switch agentproto.NormalizeBackend(backend) {
	case agentproto.BackendClaude:
		if c == nil || c.claude == nil {
			return nil, nil
		}
		return c.claude.RecentThreads(limit)
	default:
		if c == nil || c.codex == nil {
			return nil, nil
		}
		return c.codex.RecentThreads(limit)
	}
}

func (c *daemonPersistedThreadCatalog) RecentWorkspacesForBackend(backend agentproto.Backend, limit int) (map[string]time.Time, error) {
	switch agentproto.NormalizeBackend(backend) {
	case agentproto.BackendClaude:
		if c == nil || c.claude == nil {
			return nil, nil
		}
		return c.claude.RecentWorkspaces(limit)
	default:
		if c == nil || c.codex == nil {
			return nil, nil
		}
		return c.codex.RecentWorkspaces(limit)
	}
}

func (c *daemonPersistedThreadCatalog) ThreadByIDForBackend(backend agentproto.Backend, threadID string) (*state.ThreadRecord, error) {
	switch agentproto.NormalizeBackend(backend) {
	case agentproto.BackendClaude:
		if c == nil || c.claude == nil {
			return nil, nil
		}
		return c.claude.ThreadByID(threadID)
	default:
		if c == nil || c.codex == nil {
			return nil, nil
		}
		return c.codex.ThreadByID(threadID)
	}
}
