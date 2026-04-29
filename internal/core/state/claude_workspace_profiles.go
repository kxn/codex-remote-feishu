package state

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	claudeWorkspaceProfileKeySeparator = "\x00"
	DefaultClaudeProfileID             = "default"
)

func NormalizeClaudeProfileID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultClaudeProfileID
	}
	return value
}

func ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey string, backend agentproto.Backend, profileID string) string {
	workspaceKey = ResolveWorkspaceKey(workspaceKey)
	if workspaceKey == "" {
		return ""
	}
	backend = agentproto.NormalizeBackend(backend)
	if backend == "" {
		return ""
	}
	return string(backend) + claudeWorkspaceProfileKeySeparator + NormalizeClaudeProfileID(profileID) + claudeWorkspaceProfileKeySeparator + workspaceKey
}

func NormalizeClaudeWorkspaceProfileSnapshotRecord(value ClaudeWorkspaceProfileSnapshotRecord) ClaudeWorkspaceProfileSnapshotRecord {
	value.ReasoningEffort = strings.TrimSpace(strings.ToLower(value.ReasoningEffort))
	value.AccessMode = agentproto.NormalizeAccessMode(value.AccessMode)
	value.PlanMode = NormalizePlanModeSetting(value.PlanMode)
	if ClaudeWorkspaceProfileSnapshotRecordEmpty(value) {
		return ClaudeWorkspaceProfileSnapshotRecord{}
	}
	return value
}

func ClaudeWorkspaceProfileSnapshotRecordEmpty(value ClaudeWorkspaceProfileSnapshotRecord) bool {
	return strings.TrimSpace(value.ReasoningEffort) == "" &&
		strings.TrimSpace(value.AccessMode) == "" &&
		NormalizePlanModeSetting(value.PlanMode) == PlanModeSettingOff
}
