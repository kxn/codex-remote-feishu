package claude

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	claudePermissionModeDefault           = "default"
	claudePermissionModeAcceptEdits       = "acceptEdits"
	claudePermissionModePlan              = "plan"
	claudePermissionModeBypassPermissions = "bypassPermissions"
)

type claudePermissionSelection struct {
	NativeMode string
	AccessMode string
	PlanMode   string
}

func claudePermissionSelectionFromOverrides(accessMode, planMode string) claudePermissionSelection {
	normalizedPlan := state.NormalizePlanModeSetting(state.PlanModeSetting(planMode))
	if normalizedPlan == state.PlanModeSettingOn {
		return claudePermissionSelection{
			NativeMode: claudePermissionModePlan,
			PlanMode:   string(normalizedPlan),
		}
	}
	switch agentproto.NormalizeAccessMode(accessMode) {
	case agentproto.AccessModeAcceptEdits:
		return claudePermissionSelection{
			NativeMode: claudePermissionModeAcceptEdits,
			AccessMode: agentproto.AccessModeAcceptEdits,
			PlanMode:   string(state.PlanModeSettingOff),
		}
	case agentproto.AccessModeFullAccess:
		return claudePermissionSelection{
			NativeMode: claudePermissionModeBypassPermissions,
			AccessMode: agentproto.AccessModeFullAccess,
			PlanMode:   string(state.PlanModeSettingOff),
		}
	default:
		return claudePermissionSelection{
			NativeMode: claudePermissionModeDefault,
			AccessMode: agentproto.AccessModeConfirm,
			PlanMode:   string(state.PlanModeSettingOff),
		}
	}
}

func claudePermissionSelectionFromNative(mode string) claudePermissionSelection {
	switch strings.TrimSpace(mode) {
	case claudePermissionModeAcceptEdits:
		return claudePermissionSelection{
			NativeMode: claudePermissionModeAcceptEdits,
			AccessMode: agentproto.AccessModeAcceptEdits,
			PlanMode:   string(state.PlanModeSettingOff),
		}
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
