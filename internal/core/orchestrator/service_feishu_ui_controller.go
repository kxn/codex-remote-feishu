package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) ApplyFeishuUIIntent(action control.Action, intent control.FeishuUIIntent) []control.UIEvent {
	surface := s.ensureSurface(action)
	return s.filterEventsForSurfaceVisibility(s.applyFeishuUIIntent(surface, intent))
}

func (s *Service) applyFeishuUIIntent(surface *state.SurfaceConsoleRecord, intent control.FeishuUIIntent) []control.UIEvent {
	switch intent.Kind {
	case control.FeishuUIIntentShowWorkspaceRoot:
		if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先 `/mode normal`。")
		}
		return []control.UIEvent{s.workspacePageEvent(surface, control.FeishuCommandWorkspace, s.workspacePageTriggeredFromMenu(surface, intent.SourceMessageID))}
	case control.FeishuUIIntentShowWorkspaceNew:
		if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先 `/mode normal`。")
		}
		return []control.UIEvent{s.workspacePageEvent(surface, control.FeishuCommandWorkspaceNew, s.workspacePageTriggeredFromMenu(surface, intent.SourceMessageID))}
	case control.FeishuUIIntentShowWorkspaceList:
		if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先 `/mode normal`。")
		}
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceList, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowWorkspaceNewDir:
		if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先 `/mode normal`。")
		}
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceDir, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowWorkspaceNewGit:
		if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先 `/mode normal`。")
		}
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceGit, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowCommandMenu:
		return []control.UIEvent{s.menuPageEvent(surface, intent.RawText)}
	case control.FeishuUIIntentShowHistory:
		return s.openThreadHistory(surface, intent.SourceMessageID, intent.Inline)
	case control.FeishuUIIntentShowModeCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildModeCommandView(surface))}
	case control.FeishuUIIntentShowAutoContinueCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildAutoContinueCommandView(surface))}
	case control.FeishuUIIntentShowReasoningCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildReasoningCommandView(surface))}
	case control.FeishuUIIntentShowAccessCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildAccessCommandView(surface))}
	case control.FeishuUIIntentShowPlanCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildPlanCommandView(surface))}
	case control.FeishuUIIntentShowModelCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildModelCommandView(surface))}
	case control.FeishuUIIntentShowVerboseCatalog:
		return []control.UIEvent{s.configPageEventFromCatalogView(surface, s.buildVerboseCommandView(surface))}
	case control.FeishuUIIntentShowList:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceList, "", "", intent.SourceMessageID, true)
		}
		return s.presentInstanceSelectionWithInline(surface, true)
	case control.FeishuUIIntentOpenSendFilePicker:
		return s.openSendFilePickerWithInline(surface, intent.SourceMessageID, true)
	case control.FeishuUIIntentShowRecentWorkspaces:
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceList, intent.WorkspaceKey, "", intent.SourceMessageID, true)
	case control.FeishuUIIntentShowAllWorkspaces:
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceList, intent.WorkspaceKey, "", intent.SourceMessageID, true)
	case control.FeishuUIIntentShowThreads:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceUse, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayRecent, intent.Page)
	case control.FeishuUIIntentShowAllThreads:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceUseAll, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayAll, intent.Page)
	case control.FeishuUIIntentShowScopedThreads:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceUse, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		mode := threadSelectionDisplayScopedAll
		if intent.ViewMode == string(control.FeishuThreadSelectionVSCodeAll) || intent.ViewMode == string(control.FeishuThreadSelectionVSCodeScopedAll) {
			mode = threadSelectionDisplayScopedAll
		}
		return s.presentThreadSelectionMode(surface, mode, intent.Page)
	case control.FeishuUIIntentShowWorkspaceThreads:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceWorkspace, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentWorkspaceThreadSelectionPage(surface, intent.WorkspaceKey, intent.Page, intent.ReturnPage)
	case control.FeishuUIIntentShowAllThreadWorkspaces:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceUseAll, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayAllExpanded, intent.Page)
	case control.FeishuUIIntentShowRecentThreadWorkspaces:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return s.openTargetPicker(surface, control.TargetPickerRequestSourceUseAll, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayAllExpanded, intent.Page)
	case control.FeishuUIIntentPathPickerEnter:
		return s.handlePathPickerEnter(surface, intent.PickerID, intent.PickerEntry, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerUp:
		return s.handlePathPickerUp(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerSelect:
		return s.handlePathPickerSelect(surface, intent.PickerID, intent.PickerEntry, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerConfirm:
		return s.handlePathPickerConfirm(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerCancel:
		return s.handlePathPickerCancel(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentTargetPickerSelectMode:
		return s.handleTargetPickerSelectMode(surface, intent.PickerID, intent.TargetValue, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerSelectSource:
		return s.handleTargetPickerSelectSource(surface, intent.PickerID, intent.TargetValue, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerSelectWorkspace:
		return s.handleTargetPickerSelectWorkspace(surface, intent.PickerID, intent.WorkspaceKey, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerSelectSession:
		return s.handleTargetPickerSelectSession(surface, intent.PickerID, intent.TargetValue, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerOpenPathPicker:
		return s.handleTargetPickerOpenPathPicker(surface, intent.PickerID, intent.TargetValue, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerBack:
		return s.handleTargetPickerBack(surface, intent.PickerID, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerCancel:
		return s.handleTargetPickerCancel(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentHistoryPage:
		return s.handleThreadHistoryPage(surface, intent.PickerID, intent.Page, intent.ActorUserID, intent.SourceMessageID, intent.Inline)
	case control.FeishuUIIntentHistoryDetail:
		return s.handleThreadHistoryDetail(surface, intent.PickerID, intent.TurnID, intent.ActorUserID, intent.SourceMessageID, intent.Inline)
	default:
		return nil
	}
}
