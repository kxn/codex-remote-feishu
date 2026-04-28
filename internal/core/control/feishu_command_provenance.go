package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type ResolvedCommand struct {
	FamilyID  string
	VariantID string
	Backend   agentproto.Backend
	Action    Action
}

func NormalizeResolvedCommand(resolved ResolvedCommand) ResolvedCommand {
	familyID := strings.TrimSpace(resolved.FamilyID)
	variantID := strings.TrimSpace(resolved.VariantID)
	if variantID == "" && familyID != "" {
		variantID = defaultFeishuCommandDisplayVariantID(familyID)
	}
	backend := agentproto.NormalizeBackend(resolved.Backend)
	action := ApplyCatalogProvenanceToAction(resolved.Action, familyID, variantID, backend)
	return ResolvedCommand{
		FamilyID:  familyID,
		VariantID: variantID,
		Backend:   backend,
		Action:    action,
	}
}

func ApplyCatalogProvenanceToAction(action Action, familyID, variantID string, backend agentproto.Backend) Action {
	action.CatalogFamilyID = strings.TrimSpace(familyID)
	action.CatalogVariantID = strings.TrimSpace(variantID)
	action.CatalogBackend = agentproto.NormalizeBackend(backend)
	if action.CatalogVariantID == "" && action.CatalogFamilyID != "" {
		action.CatalogVariantID = defaultFeishuCommandDisplayVariantID(action.CatalogFamilyID)
	}
	return action
}

func ResolveFeishuActionCatalog(ctx CatalogContext, action Action) (ResolvedCommand, bool) {
	ctx = NormalizeCatalogContext(ctx)
	if strings.TrimSpace(action.CatalogFamilyID) != "" || strings.TrimSpace(action.CatalogVariantID) != "" {
		backend := firstNonZeroBackend(action.CatalogBackend, ctx.Backend)
		variantID := strings.TrimSpace(action.CatalogVariantID)
		if familyID := strings.TrimSpace(action.CatalogFamilyID); familyID != "" &&
			(variantID == "" || variantID == defaultFeishuCommandDisplayVariantID(familyID)) {
			variantID = feishuCommandVariantIDForContext(action.CatalogFamilyID, CatalogContext{
				Backend:      backend,
				ProductMode:  ctx.ProductMode,
				MenuStage:    ctx.MenuStage,
				AttachedKind: ctx.AttachedKind,
				WorkspaceKey: ctx.WorkspaceKey,
				InstanceID:   ctx.InstanceID,
				Capabilities: ctx.Capabilities,
			})
		}
		return NormalizeResolvedCommand(ResolvedCommand{
			FamilyID:  action.CatalogFamilyID,
			VariantID: variantID,
			Backend:   backend,
			Action:    action,
		}), true
	}
	if commandID := strings.TrimSpace(action.CommandID); commandID != "" {
		return resolvedCommandFromCommandID(ctx, commandID, action)
	}
	if text := strings.TrimSpace(action.Text); text != "" {
		if resolved, ok := ResolveFeishuTextCommand(ctx, text); ok {
			resolved.Action = mergeResolvedAction(action, resolved)
			return NormalizeResolvedCommand(resolved), true
		}
	}
	if commandID, ok := FeishuCommandIDForActionKind(action.Kind); ok {
		return resolvedCommandFromCommandID(ctx, commandID, action)
	}
	return ResolvedCommand{}, false
}

func resolvedCommandFromCommandID(ctx CatalogContext, commandID string, action Action) (ResolvedCommand, bool) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return ResolvedCommand{}, false
	}
	spec, ok := feishuCommandSpecByID(commandID)
	if !ok {
		return ResolvedCommand{}, false
	}
	action = ApplyCatalogProvenanceToAction(action, commandID, feishuCommandVariantIDForContext(commandID, ctx), ctx.Backend)
	return resolvedFeishuCommandFromSpec(ctx, spec, action), true
}

func mergeResolvedAction(base Action, resolved ResolvedCommand) Action {
	base = ApplyCatalogProvenanceToAction(base, resolved.FamilyID, resolved.VariantID, resolved.Backend)
	if strings.TrimSpace(base.Text) == "" {
		base.Text = resolved.Action.Text
	}
	if base.Kind == "" {
		base.Kind = resolved.Action.Kind
	}
	if strings.TrimSpace(base.CommandID) == "" {
		base.CommandID = strings.TrimSpace(resolved.FamilyID)
	}
	return base
}

func FeishuCommandIDForActionKind(kind ActionKind) (string, bool) {
	commandID, _, ok := feishuCommandActionRouteByKind(kind)
	return commandID, ok
}

func firstNonZeroBackend(values ...agentproto.Backend) agentproto.Backend {
	for _, value := range values {
		if normalized := agentproto.NormalizeBackend(value); normalized != "" {
			return normalized
		}
	}
	return agentproto.BackendCodex
}
