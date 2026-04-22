package feishu

import "strings"

const (
	cardActionPayloadKeyKind                  = "kind"
	cardActionPayloadKeyInstanceID            = "instance_id"
	cardActionPayloadKeyWorkspaceKey          = "workspace_key"
	cardActionPayloadKeyThreadID              = "thread_id"
	cardActionPayloadKeyTurnID                = "turn_id"
	cardActionPayloadKeyViewMode              = "view_mode"
	cardActionPayloadKeyPage                  = "page"
	cardActionPayloadKeyReturnPage            = "return_page"
	cardActionPayloadKeyAllowCrossWorkspace   = "allow_cross_workspace"
	cardActionPayloadKeyPromptID              = "prompt_id"
	cardActionPayloadKeyOptionID              = "option_id"
	cardActionPayloadKeyRequestID             = "request_id"
	cardActionPayloadKeyRequestType           = "request_type"
	cardActionPayloadKeyRequestOptionID       = "request_option_id"
	cardActionPayloadKeyRequestAnswers        = "request_answers"
	cardActionPayloadKeyRequestRevision       = "request_revision"
	cardActionPayloadKeyCommandID             = "command_id"
	cardActionPayloadKeyActionKind            = "action_kind"
	cardActionPayloadKeyActionArg             = "action_arg"
	cardActionPayloadKeyActionArgPrefix       = "action_arg_prefix"
	cardActionPayloadKeyFieldName             = "field_name"
	cardActionPayloadKeyPickerID              = "picker_id"
	cardActionPayloadKeyEntryName             = "entry_name"
	cardActionPayloadKeyTargetValue           = "target_value"
	cardActionPayloadKeyDaemonLifecycleID     = "daemon_lifecycle_id"
	cardPathPickerDirectorySelectFieldName    = "path_picker_directory"
	cardPathPickerFileSelectFieldName         = "path_picker_file"
	cardTargetPickerModeFieldName             = "target_picker_mode"
	cardTargetPickerWorkspaceFieldName        = "target_picker_workspace"
	cardTargetPickerSessionFieldName          = "target_picker_session"
	cardTargetPickerSourceFieldName           = "target_picker_source"
	cardSelectionThreadFieldName              = "selection_thread"
	cardThreadHistoryTurnFieldName            = "thread_history_turn"
	cardActionPayloadDefaultCommandFieldName  = "command_args"
	cardActionKindAttachInstance              = "attach_instance"
	cardActionKindAttachWorkspace             = "attach_workspace"
	cardActionKindUseThread                   = "use_thread"
	cardActionKindShowScopedThreads           = "show_scoped_threads"
	cardActionKindShowThreads                 = "show_threads"
	cardActionKindShowAllThreads              = "show_all_threads"
	cardActionKindShowAllThreadWorkspaces     = "show_all_thread_workspaces"
	cardActionKindShowRecentThreadWorkspaces  = "show_recent_thread_workspaces"
	cardActionKindShowWorkspaceThreads        = "show_workspace_threads"
	cardActionKindShowAllWorkspaces           = "show_all_workspaces"
	cardActionKindShowRecentWorkspaces        = "show_recent_workspaces"
	cardActionKindKickThreadConfirm           = "kick_thread_confirm"
	cardActionKindKickThreadCancel            = "kick_thread_cancel"
	cardActionKindRequestRespond              = "request_respond"
	cardActionKindPageAction                  = "page_action"
	cardActionKindUpgradeOwnerFlow            = "upgrade_owner_flow"
	cardActionKindVSCodeMigrateOwnerFlow      = "vscode_migrate_owner_flow"
	cardActionKindPlanProposal                = "plan_proposal"
	cardActionKindPageSubmit                  = "page_submit"
	cardActionKindSubmitRequestForm           = "submit_request_form"
	cardActionKindPathPickerEnter             = "path_picker_enter"
	cardActionKindPathPickerUp                = "path_picker_up"
	cardActionKindPathPickerSelect            = "path_picker_select"
	cardActionKindPathPickerConfirm           = "path_picker_confirm"
	cardActionKindPathPickerCancel            = "path_picker_cancel"
	cardActionKindTargetPickerSelectMode      = "target_picker_select_mode"
	cardActionKindTargetPickerSelectSource    = "target_picker_select_source"
	cardActionKindTargetPickerSelectWorkspace = "target_picker_select_workspace"
	cardActionKindTargetPickerSelectSession   = "target_picker_select_session"
	cardActionKindTargetPickerOpenPathPicker  = "target_picker_open_path_picker"
	cardActionKindTargetPickerBack            = "target_picker_back"
	cardActionKindTargetPickerCancel          = "target_picker_cancel"
	cardActionKindTargetPickerConfirm         = "target_picker_confirm"
	cardActionKindHistoryPage                 = "history_page"
	cardActionKindHistoryDetail               = "history_detail"
)

func actionPayloadKind(value map[string]any) string {
	return strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyKind))
}

func actionPayloadWithLifecycle(value map[string]any, daemonLifecycleID string) map[string]any {
	if len(value) == 0 {
		return value
	}
	if strings.TrimSpace(daemonLifecycleID) == "" {
		return value
	}
	value[cardActionPayloadKeyDaemonLifecycleID] = strings.TrimSpace(daemonLifecycleID)
	return value
}

func actionPayloadNavigation(kind string) map[string]any {
	return map[string]any{cardActionPayloadKeyKind: kind}
}

func actionPayloadNavigationPage(kind string, page int) map[string]any {
	payload := actionPayloadNavigation(kind)
	if page > 0 {
		payload[cardActionPayloadKeyPage] = page
	}
	return payload
}

func actionPayloadThreadNavigation(kind, viewMode string, page int) map[string]any {
	payload := actionPayloadNavigationPage(kind, page)
	if strings.TrimSpace(viewMode) != "" {
		payload[cardActionPayloadKeyViewMode] = strings.TrimSpace(viewMode)
	}
	return payload
}

func actionPayloadWorkspaceThreads(workspaceKey string, page, returnPage int) map[string]any {
	payload := map[string]any{
		cardActionPayloadKeyKind:         cardActionKindShowWorkspaceThreads,
		cardActionPayloadKeyWorkspaceKey: strings.TrimSpace(workspaceKey),
	}
	if page > 0 {
		payload[cardActionPayloadKeyPage] = page
	}
	if returnPage > 0 {
		payload[cardActionPayloadKeyReturnPage] = returnPage
	}
	return payload
}

func actionPayloadAttachInstance(instanceID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:       cardActionKindAttachInstance,
		cardActionPayloadKeyInstanceID: strings.TrimSpace(instanceID),
	}
}

func actionPayloadAttachWorkspace(workspaceKey string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:         cardActionKindAttachWorkspace,
		cardActionPayloadKeyWorkspaceKey: strings.TrimSpace(workspaceKey),
	}
}

func actionPayloadUseThread(threadID string, allowCrossWorkspace bool) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:                cardActionKindUseThread,
		cardActionPayloadKeyThreadID:            strings.TrimSpace(threadID),
		cardActionPayloadKeyAllowCrossWorkspace: allowCrossWorkspace,
	}
}

func actionPayloadUseThreadField(fieldName string, allowCrossWorkspace bool) map[string]any {
	payload := actionPayloadUseThread("", allowCrossWorkspace)
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = cardSelectionThreadFieldName
	}
	payload[cardActionPayloadKeyFieldName] = fieldName
	return payload
}

func actionPayloadKickThreadConfirm(threadID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:     cardActionKindKickThreadConfirm,
		cardActionPayloadKeyThreadID: strings.TrimSpace(threadID),
	}
}

func actionPayloadPageAction(actionKind, actionArg string) map[string]any {
	payload := map[string]any{
		cardActionPayloadKeyKind:       cardActionKindPageAction,
		cardActionPayloadKeyActionKind: strings.TrimSpace(actionKind),
	}
	if strings.TrimSpace(actionArg) != "" {
		payload[cardActionPayloadKeyActionArg] = strings.TrimSpace(actionArg)
	}
	return payload
}

func actionPayloadUpgradeOwnerFlow(flowID, optionID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:     cardActionKindUpgradeOwnerFlow,
		cardActionPayloadKeyPickerID: strings.TrimSpace(flowID),
		cardActionPayloadKeyOptionID: strings.TrimSpace(optionID),
	}
}

func actionPayloadVSCodeMigrateOwnerFlow(flowID, optionID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:     cardActionKindVSCodeMigrateOwnerFlow,
		cardActionPayloadKeyPickerID: strings.TrimSpace(flowID),
		cardActionPayloadKeyOptionID: strings.TrimSpace(optionID),
	}
}

func actionPayloadPageSubmit(actionKind, actionArgPrefix, fieldName string) map[string]any {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = cardActionPayloadDefaultCommandFieldName
	}
	payload := map[string]any{
		cardActionPayloadKeyKind:       cardActionKindPageSubmit,
		cardActionPayloadKeyActionKind: strings.TrimSpace(actionKind),
		cardActionPayloadKeyFieldName:  fieldName,
	}
	if strings.TrimSpace(actionArgPrefix) != "" {
		payload[cardActionPayloadKeyActionArgPrefix] = strings.TrimSpace(actionArgPrefix)
	}
	return payload
}

func actionPayloadRequestRespond(requestID, requestType, requestOptionID string, requestAnswers map[string]any) map[string]any {
	payload := map[string]any{
		cardActionPayloadKeyKind:            cardActionKindRequestRespond,
		cardActionPayloadKeyRequestID:       strings.TrimSpace(requestID),
		cardActionPayloadKeyRequestType:     strings.TrimSpace(requestType),
		cardActionPayloadKeyRequestOptionID: strings.TrimSpace(requestOptionID),
	}
	if len(requestAnswers) != 0 {
		payload[cardActionPayloadKeyRequestAnswers] = requestAnswers
	}
	return payload
}

func actionPayloadSubmitRequestForm(requestID, requestType string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:        cardActionKindSubmitRequestForm,
		cardActionPayloadKeyRequestID:   strings.TrimSpace(requestID),
		cardActionPayloadKeyRequestType: strings.TrimSpace(requestType),
	}
}

func actionPayloadPathPicker(kind, pickerID, entryName string) map[string]any {
	payload := map[string]any{
		cardActionPayloadKeyKind:     strings.TrimSpace(kind),
		cardActionPayloadKeyPickerID: strings.TrimSpace(pickerID),
	}
	if strings.TrimSpace(entryName) != "" {
		payload[cardActionPayloadKeyEntryName] = strings.TrimSpace(entryName)
	}
	return payload
}

func actionPayloadTargetPicker(kind, pickerID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:     strings.TrimSpace(kind),
		cardActionPayloadKeyPickerID: strings.TrimSpace(pickerID),
	}
}

func actionPayloadTargetPickerValue(kind, pickerID, targetValue string) map[string]any {
	payload := actionPayloadTargetPicker(kind, pickerID)
	if strings.TrimSpace(targetValue) != "" {
		payload[cardActionPayloadKeyTargetValue] = strings.TrimSpace(targetValue)
	}
	return payload
}

func actionPayloadThreadHistory(kind, pickerID, turnID string, page int) map[string]any {
	payload := map[string]any{
		cardActionPayloadKeyKind:     strings.TrimSpace(kind),
		cardActionPayloadKeyPickerID: strings.TrimSpace(pickerID),
	}
	if page > 0 {
		payload[cardActionPayloadKeyPage] = page
	}
	if strings.TrimSpace(turnID) != "" {
		payload[cardActionPayloadKeyTurnID] = strings.TrimSpace(turnID)
	}
	return payload
}
