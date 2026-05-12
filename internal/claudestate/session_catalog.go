package claudestate

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/claudesessionstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type SessionCatalogOptions struct {
	Logf func(string, ...any)
}

type SessionCatalog struct {
	logf func(string, ...any)
}

func NewSessionCatalog(opts SessionCatalogOptions) *SessionCatalog {
	return &SessionCatalog{logf: opts.Logf}
}

func (c *SessionCatalog) RecentThreads(limit int) ([]state.ThreadRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	metas, err := claudesessionstore.ListSessionMeta("", true)
	if err != nil {
		return nil, err
	}
	if len(metas) > limit {
		metas = metas[:limit]
	}
	threads := make([]state.ThreadRecord, 0, len(metas))
	for _, meta := range metas {
		if thread := sessionMetaToThreadRecord(meta); thread != nil {
			threads = append(threads, *thread)
		}
	}
	return threads, nil
}

func (c *SessionCatalog) RecentWorkspaces(limit int) (map[string]time.Time, error) {
	if limit <= 0 {
		limit = 200
	}
	metas, err := claudesessionstore.ListSessionMeta("", true)
	if err != nil {
		return nil, err
	}
	workspaces := map[string]time.Time{}
	for _, meta := range metas {
		workspaceKey := state.ResolveWorkspaceKey(meta.WorkspaceKey, meta.CWD)
		if workspaceKey == "" {
			continue
		}
		if current, ok := workspaces[workspaceKey]; !ok || meta.UpdatedAt.After(current) {
			workspaces[workspaceKey] = meta.UpdatedAt
		}
		if len(workspaces) >= limit {
			break
		}
	}
	if len(workspaces) == 0 {
		return nil, nil
	}
	return workspaces, nil
}

func (c *SessionCatalog) ThreadByID(threadID string) (*state.ThreadRecord, error) {
	meta, err := claudesessionstore.FindSessionMeta(strings.TrimSpace(threadID))
	if err != nil || meta == nil {
		return nil, err
	}
	return sessionMetaToThreadRecord(*meta), nil
}

func sessionMetaToThreadRecord(meta claudesessionstore.SessionMeta) *state.ThreadRecord {
	threadID := strings.TrimSpace(meta.ID)
	workspaceKey := state.ResolveWorkspaceKey(meta.WorkspaceKey, meta.CWD)
	cwd := state.ResolveWorkspaceKey(meta.CWD, workspaceKey)
	if threadID == "" || workspaceKey == "" {
		return nil
	}
	return &state.ThreadRecord{
		ThreadID:      threadID,
		Name:          strings.TrimSpace(meta.Title),
		Preview:       strings.TrimSpace(meta.Preview),
		WorkspaceKey:  workspaceKey,
		CWD:           cwd,
		State:         string(agentproto.ThreadRuntimeStatusTypeNotLoaded),
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeNotLoaded},
		ExplicitModel: strings.TrimSpace(meta.Model),
		ObservedAccessMode: agentproto.NormalizeAccessMode(
			strings.TrimSpace(meta.AccessMode),
		),
		ObservedPlanMode: state.NormalizePlanModeSetting(
			state.PlanModeSetting(strings.TrimSpace(meta.PlanMode)),
		),
		Loaded:     false,
		Archived:   false,
		LastUsedAt: meta.UpdatedAt.UTC(),
	}
}
