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
	case control.FeishuUIIntentShowCommandMenu:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildCommandMenuView(surface, intent.RawText))}
	case control.FeishuUIIntentShowModeCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModeCommandView(surface))}
	case control.FeishuUIIntentShowAutoContinueCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildAutoContinueCommandView(surface))}
	case control.FeishuUIIntentShowReasoningCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildReasoningCommandView(surface))}
	case control.FeishuUIIntentShowAccessCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildAccessCommandView(surface))}
	case control.FeishuUIIntentShowModelCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModelCommandView(surface))}
	case control.FeishuUIIntentShowVerboseCatalog:
		return []control.UIEvent{s.commandViewEvent(surface, s.buildVerboseCommandView(surface))}
	case control.FeishuUIIntentShowRecentWorkspaces:
		return s.presentWorkspaceSelectionPage(surface, intent.Page)
	case control.FeishuUIIntentShowAllWorkspaces:
		return s.presentWorkspaceSelectionPage(surface, intent.Page)
	case control.FeishuUIIntentShowThreads:
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayRecent, intent.Page)
	case control.FeishuUIIntentShowAllThreads:
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayAll, intent.Page)
	case control.FeishuUIIntentShowScopedThreads:
		mode := threadSelectionDisplayScopedAll
		if intent.ViewMode == string(control.FeishuThreadSelectionVSCodeAll) || intent.ViewMode == string(control.FeishuThreadSelectionVSCodeScopedAll) {
			mode = threadSelectionDisplayScopedAll
		}
		return s.presentThreadSelectionMode(surface, mode, intent.Page)
	case control.FeishuUIIntentShowWorkspaceThreads:
		return s.presentWorkspaceThreadSelectionPage(surface, intent.WorkspaceKey, intent.Page, intent.ReturnPage)
	case control.FeishuUIIntentShowAllThreadWorkspaces:
		return s.presentThreadSelectionMode(surface, threadSelectionDisplayAllExpanded, intent.Page)
	case control.FeishuUIIntentShowRecentThreadWorkspaces:
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
	default:
		return nil
	}
}
