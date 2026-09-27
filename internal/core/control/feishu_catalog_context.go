package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type CatalogAttachedKind string

const (
	CatalogAttachedKindDetached  CatalogAttachedKind = "detached"
	CatalogAttachedKindWorkspace CatalogAttachedKind = "workspace"
	CatalogAttachedKindInstance  CatalogAttachedKind = "instance"
)

type CatalogSurfaceScopeKind string

const (
	CatalogSurfaceScopeKindUser CatalogSurfaceScopeKind = "user"
	CatalogSurfaceScopeKindChat CatalogSurfaceScopeKind = "chat"
)

type CatalogPrimaryBotState string

const (
	CatalogPrimaryBotStateUnknown CatalogPrimaryBotState = "unknown"
	CatalogPrimaryBotStateNone    CatalogPrimaryBotState = "none"
	CatalogPrimaryBotStateCurrent CatalogPrimaryBotState = "current"
	CatalogPrimaryBotStateOther   CatalogPrimaryBotState = "other"
)

type CatalogPrimaryPermissionState string

const (
	CatalogPrimaryPermissionStateUnknown CatalogPrimaryPermissionState = "unknown"
	CatalogPrimaryPermissionStateGranted CatalogPrimaryPermissionState = "granted"
	CatalogPrimaryPermissionStateMissing CatalogPrimaryPermissionState = "missing"
	CatalogPrimaryPermissionStateError   CatalogPrimaryPermissionState = "error"
	CatalogPrimaryPermissionStateStale   CatalogPrimaryPermissionState = "stale"
)

type CatalogContext struct {
	Backend                           agentproto.Backend
	ProductMode                       string
	MenuStage                         string
	AttachedKind                      string
	WorkspaceKey                      string
	InstanceID                        string
	SurfaceScopeKind                  string
	PrimaryBotState                   string
	PrimaryPermissionState            string
	Capabilities                      agentproto.Capabilities
	CapabilitiesDeclared              bool
	BotCapabilitySettingsReadOnly     bool
	CodexConversationSettingsWritable bool
}

func NormalizeCatalogAttachedKind(value string) CatalogAttachedKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(CatalogAttachedKindWorkspace):
		return CatalogAttachedKindWorkspace
	case string(CatalogAttachedKindInstance):
		return CatalogAttachedKindInstance
	default:
		return CatalogAttachedKindDetached
	}
}

func NormalizeCatalogSurfaceScopeKind(value string) CatalogSurfaceScopeKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(CatalogSurfaceScopeKindChat):
		return CatalogSurfaceScopeKindChat
	default:
		return CatalogSurfaceScopeKindUser
	}
}

func NormalizeCatalogPrimaryBotState(value string) CatalogPrimaryBotState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(CatalogPrimaryBotStateNone):
		return CatalogPrimaryBotStateNone
	case string(CatalogPrimaryBotStateCurrent):
		return CatalogPrimaryBotStateCurrent
	case string(CatalogPrimaryBotStateOther):
		return CatalogPrimaryBotStateOther
	default:
		return CatalogPrimaryBotStateUnknown
	}
}

func NormalizeCatalogPrimaryPermissionState(value string) CatalogPrimaryPermissionState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(CatalogPrimaryPermissionStateGranted):
		return CatalogPrimaryPermissionStateGranted
	case string(CatalogPrimaryPermissionStateMissing):
		return CatalogPrimaryPermissionStateMissing
	case string(CatalogPrimaryPermissionStateError):
		return CatalogPrimaryPermissionStateError
	case string(CatalogPrimaryPermissionStateStale):
		return CatalogPrimaryPermissionStateStale
	default:
		return CatalogPrimaryPermissionStateUnknown
	}
}

func NormalizeCatalogContext(ctx CatalogContext) CatalogContext {
	backend := agentproto.NormalizeBackend(ctx.Backend)
	productMode := normalizeFeishuCommandProductMode(ctx.ProductMode)
	instanceID := strings.TrimSpace(ctx.InstanceID)
	workspaceKey := strings.TrimSpace(ctx.WorkspaceKey)
	attachedKind := NormalizeCatalogAttachedKind(ctx.AttachedKind)
	if strings.TrimSpace(ctx.AttachedKind) == "" {
		switch {
		case instanceID == "":
			attachedKind = CatalogAttachedKindDetached
		case productMode == "vscode":
			attachedKind = CatalogAttachedKindInstance
		default:
			attachedKind = CatalogAttachedKindWorkspace
		}
	}
	menuStage := NormalizeFeishuCommandMenuStage(ctx.MenuStage)
	if strings.TrimSpace(ctx.MenuStage) == "" {
		switch attachedKind {
		case CatalogAttachedKindDetached:
			menuStage = FeishuCommandMenuStageDetached
		default:
			if productMode == "vscode" {
				menuStage = FeishuCommandMenuStageVSCodeWorking
			} else {
				menuStage = FeishuCommandMenuStageNormalWorking
			}
		}
	}
	caps := agentproto.EffectiveCapabilitiesForBackend(backend, ctx.Capabilities)
	if ctx.CapabilitiesDeclared {
		caps = ctx.Capabilities
	}
	return CatalogContext{
		Backend:                           backend,
		ProductMode:                       productMode,
		MenuStage:                         string(menuStage),
		AttachedKind:                      string(attachedKind),
		WorkspaceKey:                      workspaceKey,
		InstanceID:                        instanceID,
		SurfaceScopeKind:                  string(NormalizeCatalogSurfaceScopeKind(ctx.SurfaceScopeKind)),
		PrimaryBotState:                   string(NormalizeCatalogPrimaryBotState(ctx.PrimaryBotState)),
		PrimaryPermissionState:            string(NormalizeCatalogPrimaryPermissionState(ctx.PrimaryPermissionState)),
		Capabilities:                      caps,
		CapabilitiesDeclared:              ctx.CapabilitiesDeclared,
		BotCapabilitySettingsReadOnly:     ctx.BotCapabilitySettingsReadOnly,
		CodexConversationSettingsWritable: ctx.CodexConversationSettingsWritable,
	}
}

func VisibleModeForCatalogContext(ctx CatalogContext) string {
	normalized := NormalizeCatalogContext(ctx)
	return state.SurfaceModeAlias(state.ProductMode(normalized.ProductMode), normalized.Backend)
}
