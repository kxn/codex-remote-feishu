package claudesessionstore

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

func CompileObservedPermissionStateFromClaudeNative(mode string) *agentproto.ObservedPermissionState {
	nativeMode := firstNonEmptyString(strings.TrimSpace(mode), claudePermissionModeDefault)
	switch nativeMode {
	case claudePermissionModeDefault:
		return &agentproto.ObservedPermissionState{
			NativeMode:          nativeMode,
			ProjectedAccessMode: agentproto.AccessModeConfirm,
			ProjectedPlanMode:   string(state.PlanModeSettingOff),
			ProjectionKind:      agentproto.ObservedPermissionProjectionKindExact,
		}
	case claudePermissionModeAcceptEdits:
		return &agentproto.ObservedPermissionState{
			NativeMode:          nativeMode,
			ProjectedAccessMode: agentproto.AccessModeAcceptEdits,
			ProjectedPlanMode:   string(state.PlanModeSettingOff),
			ProjectionKind:      agentproto.ObservedPermissionProjectionKindExact,
		}
	case claudePermissionModePlan:
		return &agentproto.ObservedPermissionState{
			NativeMode:        nativeMode,
			ProjectedPlanMode: string(state.PlanModeSettingOn),
			ProjectionKind:    agentproto.ObservedPermissionProjectionKindExact,
		}
	case claudePermissionModeBypassPermissions:
		return &agentproto.ObservedPermissionState{
			NativeMode:          nativeMode,
			ProjectedAccessMode: agentproto.AccessModeFullAccess,
			ProjectedPlanMode:   string(state.PlanModeSettingOff),
			ProjectionKind:      agentproto.ObservedPermissionProjectionKindExact,
		}
	default:
		return &agentproto.ObservedPermissionState{
			NativeMode:     nativeMode,
			ProjectionKind: agentproto.ObservedPermissionProjectionKindUnmapped,
		}
	}
}

func claudePermissionSelectionFromObservedPermission(observed *agentproto.ObservedPermissionState) claudePermissionSelection {
	if observed == nil {
		return claudePermissionSelection{}
	}
	return claudePermissionSelection{
		NativeMode: strings.TrimSpace(observed.NativeMode),
		AccessMode: agentproto.NormalizeAccessMode(observed.ProjectedAccessMode),
		PlanMode:   strings.TrimSpace(observed.ProjectedPlanMode),
	}
}

func claudePermissionSelectionFromNative(mode string) claudePermissionSelection {
	return claudePermissionSelectionFromObservedPermission(CompileObservedPermissionStateFromClaudeNative(mode))
}
