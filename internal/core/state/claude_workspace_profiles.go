package state

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	claudeWorkspaceProfileKeySeparator = "\x00"
	DefaultClaudeProfileID             = "default"
	DefaultClaudeProfileName           = "默认"
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
	backend = NormalizeHeadlessBackend(backend)
	if backend == "" {
		return ""
	}
	return string(backend) + claudeWorkspaceProfileKeySeparator + NormalizeClaudeProfileID(profileID) + claudeWorkspaceProfileKeySeparator + workspaceKey
}

type ClaudeProfileRecord struct {
	ID              string
	Name            string
	ReasoningEffort string
	BuiltIn         bool
}

func NormalizeClaudeProfileRecord(value ClaudeProfileRecord) ClaudeProfileRecord {
	value.ID = NormalizeClaudeProfileID(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	value.ReasoningEffort = NormalizeClaudeReasoningEffort(value.ReasoningEffort)
	if value.Name == "" {
		if value.ID == DefaultClaudeProfileID {
			value.Name = DefaultClaudeProfileName
		} else {
			value.Name = value.ID
		}
	}
	if value.ID == DefaultClaudeProfileID {
		value.BuiltIn = true
	}
	return value
}

func NormalizeClaudeWorkspaceProfileSnapshotRecord(value ClaudeWorkspaceProfileSnapshotRecord) ClaudeWorkspaceProfileSnapshotRecord {
	value.ReasoningEffort = NormalizeReasoningEffort(value.ReasoningEffort)
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
