package claudesessionstore

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	claudePermissionModeDefault           = "default"
	claudePermissionModePlan              = "plan"
	claudePermissionModeBypassPermissions = "bypassPermissions"
)

type claudePermissionSelection struct {
	NativeMode string
	AccessMode string
	PlanMode   string
}

func claudePermissionSelectionFromNative(mode string) claudePermissionSelection {
	switch strings.TrimSpace(mode) {
	case claudePermissionModePlan:
		return claudePermissionSelection{
			NativeMode: claudePermissionModePlan,
			PlanMode:   string(state.PlanModeSettingOn),
		}
	case claudePermissionModeBypassPermissions:
		return claudePermissionSelection{
			NativeMode: claudePermissionModeBypassPermissions,
			AccessMode: agentproto.AccessModeFullAccess,
			PlanMode:   string(state.PlanModeSettingOff),
		}
	default:
		return claudePermissionSelection{
			NativeMode: firstNonEmptyString(strings.TrimSpace(mode), claudePermissionModeDefault),
			AccessMode: agentproto.AccessModeConfirm,
			PlanMode:   string(state.PlanModeSettingOff),
		}
	}
}
