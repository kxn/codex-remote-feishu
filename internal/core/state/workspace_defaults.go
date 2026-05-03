package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

const workspaceDefaultsKeySeparator = "\x00"

// WorkspaceDefaultsStorageKey partitions headless workspace defaults by the
// backend identity that makes a default meaningful for that workspace.
func WorkspaceDefaultsStorageKey(workspaceKey string, contract InstanceBackendContract) string {
	workspaceKey = ResolveWorkspaceKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	contract = NormalizeObservedInstanceBackendContract(contract)
	identity := WorkspaceDefaultsIdentity(contract)
	if identity == "" {
		return ""
	}
	return string(contract.Backend) + workspaceDefaultsKeySeparator + identity + workspaceDefaultsKeySeparator + workspaceKey
}

// LegacyWorkspaceDefaultsStorageKey returns the pre-provider/profile key used
// before workspace defaults were scoped by backend identity.
func LegacyWorkspaceDefaultsStorageKey(workspaceKey string, backend agentproto.Backend) string {
	workspaceKey = ResolveWorkspaceKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	return string(NormalizeHeadlessBackend(backend)) + workspaceDefaultsKeySeparator + workspaceKey
}

func WorkspaceDefaultsIdentity(contract InstanceBackendContract) string {
	contract = NormalizeObservedInstanceBackendContract(contract)
	switch contract.Backend {
	case agentproto.BackendClaude:
		return NormalizeClaudeProfileID(contract.ClaudeProfileID)
	default:
		return NormalizeCodexProviderID(contract.CodexProviderID)
	}
}
