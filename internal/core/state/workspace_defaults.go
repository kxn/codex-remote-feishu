package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

const workspaceDefaultsKeySeparator = "\x00"

// WorkspaceDefaultsStorageKey partitions headless workspace defaults by
// backend + workspace key while keeping the stored ProductMode token stable.
func WorkspaceDefaultsStorageKey(workspaceKey string, backend agentproto.Backend) string {
	workspaceKey = ResolveWorkspaceKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	return string(NormalizeHeadlessBackend(backend)) + workspaceDefaultsKeySeparator + workspaceKey
}
