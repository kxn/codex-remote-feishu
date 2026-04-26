package control

import "strings"

type FeishuWorkspaceSessionFlow struct {
	FamilyID         string
	DefaultVariantID string
	ActionKind       ActionKind
	IntentKind       FeishuUIIntentKind
	TargetPicker     TargetPickerRequestSource
}

func ResolveFeishuWorkspaceSessionFlowFromAction(action Action) (FeishuWorkspaceSessionFlow, bool) {
	if flow, ok := ResolveFeishuWorkspaceSessionFlowForCatalog(action.CatalogFamilyID, action.CatalogVariantID); ok {
		return flow, true
	}
	switch action.Kind {
	case ActionListInstances:
		return resolveFeishuWorkspaceSessionFlow(FeishuCommandList)
	case ActionShowThreads:
		return resolveFeishuWorkspaceSessionFlow(FeishuCommandUse)
	case ActionShowAllThreads:
		return resolveFeishuWorkspaceSessionFlow(FeishuCommandUseAll)
	case ActionNewThread:
		return resolveFeishuWorkspaceSessionFlow(FeishuCommandNew)
	case ActionFollowLocal:
		return resolveFeishuWorkspaceSessionFlow(FeishuCommandFollow)
	default:
		return FeishuWorkspaceSessionFlow{}, false
	}
}

func ResolveFeishuWorkspaceSessionFlowForCatalog(familyID, variantID string) (FeishuWorkspaceSessionFlow, bool) {
	_ = strings.TrimSpace(variantID)
	return resolveFeishuWorkspaceSessionFlow(strings.TrimSpace(familyID))
}

func resolveFeishuWorkspaceSessionFlow(familyID string) (FeishuWorkspaceSessionFlow, bool) {
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return FeishuWorkspaceSessionFlow{}, false
	}
	flow := FeishuWorkspaceSessionFlow{
		FamilyID:         familyID,
		DefaultVariantID: defaultFeishuCommandDisplayVariantID(familyID),
	}
	switch familyID {
	case FeishuCommandList:
		flow.ActionKind = ActionListInstances
		flow.IntentKind = FeishuUIIntentShowList
		flow.TargetPicker = TargetPickerRequestSourceList
	case FeishuCommandUse:
		flow.ActionKind = ActionShowThreads
		flow.IntentKind = FeishuUIIntentShowThreads
		flow.TargetPicker = TargetPickerRequestSourceUse
	case FeishuCommandUseAll:
		flow.ActionKind = ActionShowAllThreads
		flow.IntentKind = FeishuUIIntentShowAllThreads
		flow.TargetPicker = TargetPickerRequestSourceUseAll
	case FeishuCommandNew:
		flow.ActionKind = ActionNewThread
	case FeishuCommandFollow:
		flow.ActionKind = ActionFollowLocal
	default:
		return FeishuWorkspaceSessionFlow{}, false
	}
	return flow, true
}
