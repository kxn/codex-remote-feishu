package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) applyFeishuUIIntent(surface *state.SurfaceConsoleRecord, action control.Action, intent control.FeishuUIIntent) []eventcontract.Event {
	if flow, ok := control.FeishuConfigFlowDefinitionByIntentKind(intent.Kind); ok {
		if support, ok := control.ResolveFeishuCommandSupport(s.buildCatalogContext(surface), flow.CommandID); ok && !support.DispatchAllowed {
			return s.commandSupportNotice(surface, support)
		}
		return s.openConfigCommandPageForAction(surface, action)
	}
	switch intent.Kind {
	case control.FeishuUIIntentShowWorkspaceRoot:
		if !s.surfaceIsHeadless(surface) {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先切到 headless 模式（`/mode codex` 或 `/mode claude`）。")
		}
		return []eventcontract.Event{s.workspacePageEvent(surface, control.FeishuCommandWorkspace, s.workspacePageTriggeredFromMenu(surface, intent.SourceMessageID), intent.SourceMessageID)}
	case control.FeishuUIIntentShowWorkspaceNew:
		if !s.surfaceIsHeadless(surface) {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先切到 headless 模式（`/mode codex` 或 `/mode claude`）。")
		}
		return []eventcontract.Event{s.workspacePageEvent(surface, control.FeishuCommandWorkspaceNew, s.workspacePageTriggeredFromMenu(surface, intent.SourceMessageID), intent.SourceMessageID)}
	case control.FeishuUIIntentShowWorkspaceList:
		if !s.surfaceIsHeadless(surface) {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先切到 headless 模式（`/mode codex` 或 `/mode claude`）。")
		}
		return s.openTargetPickerForAction(surface, action, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowWorkspaceNewDir:
		if !s.surfaceIsHeadless(surface) {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先切到 headless 模式（`/mode codex` 或 `/mode claude`）。")
		}
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceDir, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowWorkspaceNewGit:
		if !s.surfaceIsHeadless(surface) {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先切到 headless 模式（`/mode codex` 或 `/mode claude`）。")
		}
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceGit, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowWorkspaceNewWorktree:
		if !s.surfaceIsHeadless(surface) {
			return notice(surface, "workspace_normal_only", "当前处于 vscode 模式，请先切到 headless 模式（`/mode codex` 或 `/mode claude`）。")
		}
		return s.openTargetPicker(surface, control.TargetPickerRequestSourceWorktree, "", s.workspacePageParentCommand(surface, intent.SourceMessageID), intent.SourceMessageID, true)
	case control.FeishuUIIntentShowCommandMenu:
		return []eventcontract.Event{s.menuPageEvent(surface, intent.RawText, intent.SourceMessageID)}
	case control.FeishuUIIntentShowHistory:
		return s.openThreadHistory(surface, intent.SourceMessageID, intent.Inline)
	case control.FeishuUIIntentShowReviewRoot:
		return []eventcontract.Event{s.reviewRootPageEvent(surface, s.reviewRootPageTriggeredFromMenu(surface, intent.SourceMessageID))}
	case control.FeishuUIIntentShowList:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceList, action, "", "", intent.SourceMessageID, true)
		}
		return s.presentInstanceSelectionWithAction(surface, action, true)
	case control.FeishuUIIntentOpenSendFilePicker:
		return s.openSendFilePickerWithInline(surface, intent.SourceMessageID, true)
	case control.FeishuUIIntentShowRecentWorkspaces:
		return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceList, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
	case control.FeishuUIIntentShowAllWorkspaces:
		return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceList, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
	case control.FeishuUIIntentShowThreads:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceUse, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionModeAtCursorWithAction(surface, action, threadSelectionDisplayRecent, intent.Page, 0)
	case control.FeishuUIIntentShowAllThreads:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceUseAll, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionModeAtCursorWithAction(surface, action, threadSelectionDisplayAll, intent.Page, 0)
	case control.FeishuUIIntentShowScopedThreads:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceUse, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		mode := threadSelectionDisplayScopedAll
		if intent.ViewMode == string(control.FeishuThreadSelectionVSCodeAll) || intent.ViewMode == string(control.FeishuThreadSelectionVSCodeScopedAll) {
			mode = threadSelectionDisplayScopedAll
		}
		return s.presentThreadSelectionModeAtCursorWithAction(surface, action, mode, intent.Page, 0)
	case control.FeishuUIIntentShowWorkspaceThreads:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceWorkspace, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentWorkspaceThreadSelectionPageWithAction(surface, action, intent.WorkspaceKey, intent.Page, intent.ReturnPage)
	case control.FeishuUIIntentShowAllThreadWorkspaces:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceUseAll, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionModeAtCursorWithAction(surface, action, threadSelectionDisplayAllExpanded, intent.Page, 0)
	case control.FeishuUIIntentShowRecentThreadWorkspaces:
		if s.surfaceIsHeadless(surface) {
			return s.openTargetPickerWithSourceForAction(surface, control.TargetPickerRequestSourceUseAll, action, intent.WorkspaceKey, "", intent.SourceMessageID, true)
		}
		return s.presentThreadSelectionModeAtCursorWithAction(surface, action, threadSelectionDisplayAllExpanded, intent.Page, 0)
	case control.FeishuUIIntentThreadSelectionPage:
		return s.handleThreadSelectionPageWithAction(surface, action, intent.ViewMode, intent.Cursor)
	case control.FeishuUIIntentPathPickerEnter:
		return s.handlePathPickerEnter(surface, intent.PickerID, intent.PickerEntry, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerUp:
		return s.handlePathPickerUp(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerSelect:
		return s.handlePathPickerSelect(surface, intent.PickerID, intent.PickerEntry, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerPage:
		return s.handlePathPickerPage(surface, intent.PickerID, intent.FieldName, intent.Cursor, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerConfirm:
		return s.handlePathPickerConfirm(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentPathPickerCancel:
		return s.handlePathPickerCancel(surface, intent.PickerID, intent.ActorUserID)
	case control.FeishuUIIntentTargetPickerSelectWorkspace:
		return s.handleTargetPickerSelectWorkspace(surface, intent.PickerID, intent.WorkspaceKey, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerSelectSession:
		return s.handleTargetPickerSelectSession(surface, intent.PickerID, intent.TargetValue, intent.ActorUserID, intent.RequestAnswers)
	case control.FeishuUIIntentTargetPickerPage:
		return s.handleTargetPickerPage(surface, intent.PickerID, intent.FieldName, intent.Cursor, intent.ActorUserID, intent.RequestAnswers)
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
