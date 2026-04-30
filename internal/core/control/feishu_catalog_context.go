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

type CatalogContext struct {
	Backend              agentproto.Backend
	ProductMode          string
	MenuStage            string
	AttachedKind         string
	WorkspaceKey         string
	InstanceID           string
	Capabilities         agentproto.Capabilities
	CapabilitiesDeclared bool
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
		Backend:              backend,
		ProductMode:          productMode,
		MenuStage:            string(menuStage),
		AttachedKind:         string(attachedKind),
		WorkspaceKey:         workspaceKey,
		InstanceID:           instanceID,
		Capabilities:         caps,
		CapabilitiesDeclared: ctx.CapabilitiesDeclared,
	}
}

func VisibleModeForCatalogContext(ctx CatalogContext) string {
	normalized := NormalizeCatalogContext(ctx)
	return state.SurfaceModeAlias(state.ProductMode(normalized.ProductMode), normalized.Backend)
}
