package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

const workspaceDefaultsKeySeparator = "\x00"

func WorkspaceDefaultsStorageKey(workspaceKey string, backend agentproto.Backend) string {
	workspaceKey = ResolveWorkspaceKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	return string(agentproto.NormalizeBackend(backend)) + workspaceDefaultsKeySeparator + workspaceKey
}
