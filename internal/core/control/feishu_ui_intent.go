package control

import "strings"

type FeishuUIIntentKind string

const (
	FeishuUIIntentShowCommandMenu            FeishuUIIntentKind = "show_command_menu"
	FeishuUIIntentShowModeCatalog            FeishuUIIntentKind = "show_mode_catalog"
	FeishuUIIntentShowAutoContinueCatalog    FeishuUIIntentKind = "show_auto_continue_catalog"
	FeishuUIIntentShowReasoningCatalog       FeishuUIIntentKind = "show_reasoning_catalog"
	FeishuUIIntentShowAccessCatalog          FeishuUIIntentKind = "show_access_catalog"
	FeishuUIIntentShowModelCatalog           FeishuUIIntentKind = "show_model_catalog"
	FeishuUIIntentShowVerboseCatalog         FeishuUIIntentKind = "show_verbose_catalog"
	FeishuUIIntentShowRecentWorkspaces       FeishuUIIntentKind = "show_recent_workspaces"
	FeishuUIIntentShowAllWorkspaces          FeishuUIIntentKind = "show_all_workspaces"
	FeishuUIIntentShowThreads                FeishuUIIntentKind = "show_threads"
	FeishuUIIntentShowAllThreads             FeishuUIIntentKind = "show_all_threads"
	FeishuUIIntentShowScopedThreads          FeishuUIIntentKind = "show_scoped_threads"
	FeishuUIIntentShowWorkspaceThreads       FeishuUIIntentKind = "show_workspace_threads"
	FeishuUIIntentShowAllThreadWorkspaces    FeishuUIIntentKind = "show_all_thread_workspaces"
	FeishuUIIntentShowRecentThreadWorkspaces FeishuUIIntentKind = "show_recent_thread_workspaces"
	FeishuUIIntentPathPickerEnter            FeishuUIIntentKind = "path_picker_enter"
	FeishuUIIntentPathPickerUp               FeishuUIIntentKind = "path_picker_up"
	FeishuUIIntentPathPickerSelect           FeishuUIIntentKind = "path_picker_select"
	FeishuUIIntentPathPickerConfirm          FeishuUIIntentKind = "path_picker_confirm"
	FeishuUIIntentPathPickerCancel           FeishuUIIntentKind = "path_picker_cancel"
)

// FeishuUIIntent classifies same-context Feishu navigation handled by the
// Feishu UI controller instead of the main product reducer.
type FeishuUIIntent struct {
	Kind         FeishuUIIntentKind
	RawText      string
	ViewMode     string
	WorkspaceKey string
	Page         int
	ReturnPage   int
	PickerID     string
	PickerEntry  string
	ActorUserID  string
}

func FeishuUIIntentFromAction(action Action) (*FeishuUIIntent, bool) {
	switch action.Kind {
	case ActionShowCommandMenu:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowCommandMenu, RawText: action.Text}, true
	case ActionModeCommand:
		if isBareInlineCommand(action.Text, "/mode") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowModeCatalog, RawText: action.Text}, true
		}
	case ActionAutoContinueCommand:
		if isBareInlineCommand(action.Text, "/autowhip") || isBareInlineCommand(action.Text, "/autocontinue") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowAutoContinueCatalog, RawText: action.Text}, true
		}
	case ActionReasoningCommand:
		if isBareInlineCommand(action.Text, "/reasoning") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowReasoningCatalog, RawText: action.Text}, true
		}
	case ActionAccessCommand:
		if isBareInlineCommand(action.Text, "/access") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowAccessCatalog, RawText: action.Text}, true
		}
	case ActionModelCommand:
		if isBareInlineCommand(action.Text, "/model") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowModelCatalog, RawText: action.Text}, true
		}
	case ActionVerboseCommand:
		if isBareInlineCommand(action.Text, "/verbose") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowVerboseCatalog, RawText: action.Text}, true
		}
	case ActionShowRecentWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowRecentWorkspaces, Page: action.Page}, true
	case ActionShowAllWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllWorkspaces, Page: action.Page}, true
	case ActionShowThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowThreads, ViewMode: action.ViewMode, Page: action.Page}, true
	case ActionShowAllThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreads, ViewMode: action.ViewMode, Page: action.Page}, true
	case ActionShowScopedThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowScopedThreads, ViewMode: action.ViewMode, Page: action.Page}, true
	case ActionShowWorkspaceThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowWorkspaceThreads, WorkspaceKey: action.WorkspaceKey, Page: action.Page, ReturnPage: action.ReturnPage}, true
	case ActionShowAllThreadWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreadWorkspaces, Page: action.Page}, true
	case ActionShowRecentThreadWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowRecentThreadWorkspaces, Page: action.Page}, true
	case ActionPathPickerEnter:
		return &FeishuUIIntent{Kind: FeishuUIIntentPathPickerEnter, PickerID: action.PickerID, PickerEntry: action.PickerEntry, ActorUserID: action.ActorUserID}, true
	case ActionPathPickerUp:
		return &FeishuUIIntent{Kind: FeishuUIIntentPathPickerUp, PickerID: action.PickerID, ActorUserID: action.ActorUserID}, true
	case ActionPathPickerSelect:
		return &FeishuUIIntent{Kind: FeishuUIIntentPathPickerSelect, PickerID: action.PickerID, PickerEntry: action.PickerEntry, ActorUserID: action.ActorUserID}, true
	case ActionPathPickerConfirm:
		return &FeishuUIIntent{Kind: FeishuUIIntentPathPickerConfirm, PickerID: action.PickerID, ActorUserID: action.ActorUserID}, true
	case ActionPathPickerCancel:
		return &FeishuUIIntent{Kind: FeishuUIIntentPathPickerCancel, PickerID: action.PickerID, ActorUserID: action.ActorUserID}, true
	}
	return nil, false
}

func isBareInlineCommand(text, command string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	return len(fields) == 1 && strings.EqualFold(fields[0], strings.TrimSpace(command))
}
