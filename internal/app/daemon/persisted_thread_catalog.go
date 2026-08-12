package daemon

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/claudestate"
	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/threadcatalogcontract"
	"github.com/kxn/codex-remote-feishu/internal/opencodestate"
)

type daemonPersistedThreadCatalog struct {
	codex    *codexstate.SQLiteThreadCatalog
	claude   *claudestate.SessionCatalog
	opencode *opencodestate.SQLiteThreadCatalog
}

var _ threadcatalogcontract.BackendAwarePersistedThreadCatalog = (*daemonPersistedThreadCatalog)(nil)

func newDaemonPersistedThreadCatalog(logf func(string, ...any)) (*daemonPersistedThreadCatalog, error) {
	codexCatalog, err := codexstate.NewDefaultSQLiteThreadCatalog(codexstate.SQLiteThreadCatalogOptions{Logf: logf})
	if err != nil {
		return nil, err
	}
	opencodeCatalog, err := opencodestate.NewDefaultSQLiteThreadCatalog(opencodestate.SQLiteThreadCatalogOptions{Logf: logf})
	if err != nil {
		return nil, err
	}
	return &daemonPersistedThreadCatalog{
		codex:    codexCatalog,
		claude:   claudestate.NewSessionCatalog(claudestate.SessionCatalogOptions{Logf: logf}),
		opencode: opencodeCatalog,
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
	case agentproto.BackendOpenCode:
		if c == nil || c.opencode == nil {
			return nil, nil
		}
		return c.opencode.RecentThreads(limit)
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
	case agentproto.BackendOpenCode:
		if c == nil || c.opencode == nil {
			return nil, nil
		}
		return c.opencode.RecentWorkspaces(limit)
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
	case agentproto.BackendOpenCode:
		if c == nil || c.opencode == nil {
			return nil, nil
		}
		return c.opencode.ThreadByID(threadID)
	default:
		if c == nil || c.codex == nil {
			return nil, nil
		}
		return c.codex.ThreadByID(threadID)
	}
}
