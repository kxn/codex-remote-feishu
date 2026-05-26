package agentproto

import "strings"

const (
	AccessModeFullAccess  = "full_access"
	AccessModeConfirm     = "confirm"
	AccessModeAcceptEdits = "accept_edits"
)

func NormalizeAccessMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "full", "full access", "fullaccess", "full_access", "full-access", "never",
		"danger-full-access", "danger_full_access", "dangerfullaccess":
		return AccessModeFullAccess
	case "accept edits", "acceptedits", "accept_edits", "accept-edits":
		return AccessModeAcceptEdits
	case "confirm", "approval", "approve", "ask", "on-request", "on_request",
		"workspace-write", "workspace_write", "workspacewrite":
		return AccessModeConfirm
	default:
		return ""
	}
}

func EffectiveAccessMode(value string) string {
	if normalized := NormalizeAccessMode(value); normalized != "" {
		return normalized
	}
	return AccessModeFullAccess
}

func ApprovalPolicyForAccessMode(value string) string {
	switch EffectiveAccessMode(value) {
	case AccessModeConfirm, AccessModeAcceptEdits:
		return "on-request"
	default:
		return "never"
	}
}

func ThreadSandboxForAccessMode(value string) string {
	switch EffectiveAccessMode(value) {
	case AccessModeConfirm, AccessModeAcceptEdits:
		return "workspace-write"
	default:
		return "danger-full-access"
	}
}

func TurnSandboxPolicyForAccessMode(value string) map[string]any {
	switch EffectiveAccessMode(value) {
	case AccessModeConfirm, AccessModeAcceptEdits:
		return map[string]any{"type": "workspaceWrite"}
	default:
		return map[string]any{"type": "dangerFullAccess"}
	}
}

func DisplayAccessMode(value string) string {
	switch EffectiveAccessMode(value) {
	case AccessModeAcceptEdits:
		return "accept edits"
	case AccessModeConfirm:
		return "confirm"
	default:
		return "full access"
	}
}

func DisplayAccessModeShort(value string) string {
	switch EffectiveAccessMode(value) {
	case AccessModeAcceptEdits:
		return "accept-edits"
	case AccessModeConfirm:
		return "confirm"
	default:
		return "full"
	}
}
