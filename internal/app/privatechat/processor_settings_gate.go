package privatechat

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// Independent read-only UI and stop remain available while the shared config
// owner commits a change. Route/input actions remain durably pending.
func safeDuringBotSettings(action control.Action) bool {
	switch action.Kind {
	case control.ActionShowCommandMenu, control.ActionShowCommandHelp, control.ActionStatus, control.ActionShowHistory, control.ActionHistoryPage, control.ActionHistoryDetail, control.ActionStop, control.ActionShowThreads, control.ActionShowAllThreads, control.ActionShowScopedThreads, control.ActionShowWorkspaceThreads, control.ActionShowAllWorkspaces, control.ActionShowRecentWorkspaces, control.ActionShowAllThreadWorkspaces, control.ActionShowRecentThreadWorkspaces, control.ActionWorkspaceRoot, control.ActionWorkspaceList, control.ActionListInstances:
		return true
	case control.ActionPathPickerEnter, control.ActionPathPickerUp, control.ActionPathPickerSelect, control.ActionPathPickerPage, control.ActionPathPickerCancel, control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage, control.ActionTargetPickerOpenPathPicker, control.ActionTargetPickerCancel, control.ActionTargetPickerBack:
		return true
	case control.ActionModeCommand, control.ActionModelCommand, control.ActionReasoningCommand, control.ActionAccessCommand, control.ActionPlanCommand, control.ActionVerboseCommand, control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand:
		return len(strings.Fields(action.Text)) <= 1 && action.OptionID == ""
	default:
		return false
	}
}
