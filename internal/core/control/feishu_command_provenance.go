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
		return NormalizeResolvedCommand(ResolvedCommand{
			FamilyID:  action.CatalogFamilyID,
			VariantID: action.CatalogVariantID,
			Backend:   firstNonZeroBackend(action.CatalogBackend, ctx.Backend),
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
	if _, ok := FeishuCommandDefinitionByID(commandID); !ok {
		return ResolvedCommand{}, false
	}
	return NormalizeResolvedCommand(ResolvedCommand{
		FamilyID:  commandID,
		VariantID: defaultFeishuCommandDisplayVariantID(commandID),
		Backend:   ctx.Backend,
		Action:    ApplyCatalogProvenanceToAction(action, commandID, defaultFeishuCommandDisplayVariantID(commandID), ctx.Backend),
	}), true
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
	if flow, ok := FeishuConfigFlowDefinitionByActionKind(kind); ok {
		return flow.CommandID, true
	}
	switch kind {
	case ActionShowCommandMenu:
		return FeishuCommandMenu, true
	case ActionShowCommandHelp:
		return FeishuCommandHelp, true
	case ActionShowHistory:
		return FeishuCommandHistory, true
	case ActionCronCommand:
		return FeishuCommandCron, true
	case ActionUpgradeCommand:
		return FeishuCommandUpgrade, true
	case ActionDebugCommand:
		return FeishuCommandDebug, true
	case ActionVSCodeMigrateCommand:
		return FeishuCommandVSCodeMigrate, true
	case ActionWorkspaceRoot:
		return FeishuCommandWorkspace, true
	case ActionWorkspaceList:
		return FeishuCommandWorkspaceList, true
	case ActionWorkspaceNew:
		return FeishuCommandWorkspaceNew, true
	case ActionWorkspaceNewDir:
		return FeishuCommandWorkspaceNewDir, true
	case ActionWorkspaceNewGit:
		return FeishuCommandWorkspaceNewGit, true
	case ActionWorkspaceDetach:
		return FeishuCommandWorkspaceDetach, true
	case ActionListInstances:
		return FeishuCommandList, true
	case ActionShowThreads:
		return FeishuCommandUse, true
	case ActionShowAllThreads:
		return FeishuCommandUseAll, true
	case ActionSendFile:
		return FeishuCommandSendFile, true
	case ActionStatus:
		return FeishuCommandStatus, true
	case ActionStop:
		return FeishuCommandStop, true
	case ActionCompact:
		return FeishuCommandCompact, true
	case ActionSteerAll:
		return FeishuCommandSteerAll, true
	case ActionNewThread:
		return FeishuCommandNew, true
	case ActionDetach:
		return FeishuCommandDetach, true
	case ActionFollowLocal:
		return FeishuCommandFollow, true
	default:
		return "", false
	}
}

func firstNonZeroBackend(values ...agentproto.Backend) agentproto.Backend {
	for _, value := range values {
		if normalized := agentproto.NormalizeBackend(value); normalized != "" {
			return normalized
		}
	}
	return agentproto.BackendCodex
}
