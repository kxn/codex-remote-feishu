package control

import "strings"

type FeishuUIIntentKind string

const (
	FeishuUIIntentShowCommandMenu             FeishuUIIntentKind = "show_command_menu"
	FeishuUIIntentShowHistory                 FeishuUIIntentKind = "show_history"
	FeishuUIIntentShowModeCatalog             FeishuUIIntentKind = "show_mode_catalog"
	FeishuUIIntentShowAutoContinueCatalog     FeishuUIIntentKind = "show_auto_continue_catalog"
	FeishuUIIntentShowReasoningCatalog        FeishuUIIntentKind = "show_reasoning_catalog"
	FeishuUIIntentShowAccessCatalog           FeishuUIIntentKind = "show_access_catalog"
	FeishuUIIntentShowModelCatalog            FeishuUIIntentKind = "show_model_catalog"
	FeishuUIIntentShowVerboseCatalog          FeishuUIIntentKind = "show_verbose_catalog"
	FeishuUIIntentShowRecentWorkspaces        FeishuUIIntentKind = "show_recent_workspaces"
	FeishuUIIntentShowAllWorkspaces           FeishuUIIntentKind = "show_all_workspaces"
	FeishuUIIntentShowThreads                 FeishuUIIntentKind = "show_threads"
	FeishuUIIntentShowAllThreads              FeishuUIIntentKind = "show_all_threads"
	FeishuUIIntentShowScopedThreads           FeishuUIIntentKind = "show_scoped_threads"
	FeishuUIIntentShowWorkspaceThreads        FeishuUIIntentKind = "show_workspace_threads"
	FeishuUIIntentShowAllThreadWorkspaces     FeishuUIIntentKind = "show_all_thread_workspaces"
	FeishuUIIntentShowRecentThreadWorkspaces  FeishuUIIntentKind = "show_recent_thread_workspaces"
	FeishuUIIntentPathPickerEnter             FeishuUIIntentKind = "path_picker_enter"
	FeishuUIIntentPathPickerUp                FeishuUIIntentKind = "path_picker_up"
	FeishuUIIntentPathPickerSelect            FeishuUIIntentKind = "path_picker_select"
	FeishuUIIntentPathPickerConfirm           FeishuUIIntentKind = "path_picker_confirm"
	FeishuUIIntentPathPickerCancel            FeishuUIIntentKind = "path_picker_cancel"
	FeishuUIIntentTargetPickerSelectMode      FeishuUIIntentKind = "target_picker_select_mode"
	FeishuUIIntentTargetPickerSelectSource    FeishuUIIntentKind = "target_picker_select_source"
	FeishuUIIntentTargetPickerSelectWorkspace FeishuUIIntentKind = "target_picker_select_workspace"
	FeishuUIIntentTargetPickerSelectSession   FeishuUIIntentKind = "target_picker_select_session"
	FeishuUIIntentTargetPickerOpenPathPicker  FeishuUIIntentKind = "target_picker_open_path_picker"
	FeishuUIIntentTargetPickerCancel          FeishuUIIntentKind = "target_picker_cancel"
	FeishuUIIntentHistoryPage                 FeishuUIIntentKind = "history_page"
	FeishuUIIntentHistoryDetail               FeishuUIIntentKind = "history_detail"
)

// FeishuUIIntent classifies same-context Feishu navigation handled by the
// Feishu UI controller instead of the main product reducer.
type FeishuUIIntent struct {
	Kind            FeishuUIIntentKind
	RawText         string
	ViewMode        string
	WorkspaceKey    string
	Page            int
	ReturnPage      int
	PickerID        string
	PickerEntry     string
	TargetValue     string
	ActorUserID     string
	TurnID          string
	SourceMessageID string
	Inline          bool
	RequestAnswers  map[string][]string
}

func FeishuUIIntentFromAction(action Action) (*FeishuUIIntent, bool) {
	switch action.Kind {
	case ActionShowCommandMenu:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowCommandMenu, RawText: action.Text, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowHistory:
		if isBareInlineCommand(action.Text, "/history") {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowHistory, RawText: action.Text, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
		}
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
		return &FeishuUIIntent{Kind: FeishuUIIntentShowRecentWorkspaces, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowAllWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllWorkspaces, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowThreads, ViewMode: action.ViewMode, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowAllThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreads, ViewMode: action.ViewMode, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowScopedThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowScopedThreads, ViewMode: action.ViewMode, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowWorkspaceThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowWorkspaceThreads, WorkspaceKey: action.WorkspaceKey, Page: action.Page, ReturnPage: action.ReturnPage, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowAllThreadWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreadWorkspaces, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionShowRecentThreadWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowRecentThreadWorkspaces, Page: action.Page, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
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
	case ActionTargetPickerSelectMode:
		return &FeishuUIIntent{Kind: FeishuUIIntentTargetPickerSelectMode, PickerID: action.PickerID, TargetValue: action.TargetPickerValue, ActorUserID: action.ActorUserID, RequestAnswers: action.RequestAnswers}, true
	case ActionTargetPickerSelectSource:
		return &FeishuUIIntent{Kind: FeishuUIIntentTargetPickerSelectSource, PickerID: action.PickerID, TargetValue: action.TargetPickerValue, ActorUserID: action.ActorUserID, RequestAnswers: action.RequestAnswers}, true
	case ActionTargetPickerSelectWorkspace:
		return &FeishuUIIntent{Kind: FeishuUIIntentTargetPickerSelectWorkspace, PickerID: action.PickerID, WorkspaceKey: action.WorkspaceKey, ActorUserID: action.ActorUserID, RequestAnswers: action.RequestAnswers}, true
	case ActionTargetPickerSelectSession:
		return &FeishuUIIntent{Kind: FeishuUIIntentTargetPickerSelectSession, PickerID: action.PickerID, TargetValue: action.TargetPickerValue, ActorUserID: action.ActorUserID, RequestAnswers: action.RequestAnswers}, true
	case ActionTargetPickerOpenPathPicker:
		return &FeishuUIIntent{Kind: FeishuUIIntentTargetPickerOpenPathPicker, PickerID: action.PickerID, TargetValue: action.TargetPickerValue, ActorUserID: action.ActorUserID, RequestAnswers: action.RequestAnswers}, true
	case ActionTargetPickerCancel:
		return &FeishuUIIntent{Kind: FeishuUIIntentTargetPickerCancel, PickerID: action.PickerID, ActorUserID: action.ActorUserID, RequestAnswers: action.RequestAnswers}, true
	case ActionHistoryPage:
		return &FeishuUIIntent{Kind: FeishuUIIntentHistoryPage, PickerID: action.PickerID, Page: action.Page, ActorUserID: action.ActorUserID, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	case ActionHistoryDetail:
		return &FeishuUIIntent{Kind: FeishuUIIntentHistoryDetail, PickerID: action.PickerID, TurnID: action.TurnID, ActorUserID: action.ActorUserID, SourceMessageID: action.MessageID, Inline: action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""}, true
	}
	return nil, false
}

func isBareInlineCommand(text, command string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	return len(fields) == 1 && strings.EqualFold(fields[0], strings.TrimSpace(command))
}
