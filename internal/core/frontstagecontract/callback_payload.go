package frontstagecontract

import "strings"

const (
	CardActionPayloadKeyKind                  = "kind"
	CardActionPayloadKeyInstanceID            = "instance_id"
	CardActionPayloadKeyWorkspaceKey          = "workspace_key"
	CardActionPayloadKeyThreadID              = "thread_id"
	CardActionPayloadKeyTurnID                = "turn_id"
	CardActionPayloadKeyViewMode              = "view_mode"
	CardActionPayloadKeyPage                  = "page"
	CardActionPayloadKeyReturnPage            = "return_page"
	CardActionPayloadKeyAllowCrossWorkspace   = "allow_cross_workspace"
	CardActionPayloadKeyPromptID              = "prompt_id"
	CardActionPayloadKeyOptionID              = "option_id"
	CardActionPayloadKeyRequestID             = "request_id"
	CardActionPayloadKeyRequestType           = "request_type"
	CardActionPayloadKeyRequestOptionID       = "request_option_id"
	CardActionPayloadKeyRequestAnswers        = "request_answers"
	CardActionPayloadKeyRequestRevision       = "request_revision"
	CardActionPayloadKeyRequestControl        = "request_control"
	CardActionPayloadKeyQuestionID            = "question_id"
	CardActionPayloadKeyCommandID             = "command_id"
	CardActionPayloadKeyActionKind            = "action_kind"
	CardActionPayloadKeyActionArg             = "action_arg"
	CardActionPayloadKeyActionArgPrefix       = "action_arg_prefix"
	CardActionPayloadKeyFieldName             = "field_name"
	CardActionPayloadKeyCursor                = "cursor"
	CardActionPayloadKeyPickerID              = "picker_id"
	CardActionPayloadKeyEntryName             = "entry_name"
	CardActionPayloadKeyTargetValue           = "target_value"
	CardActionPayloadKeyDaemonLifecycleID     = "daemon_lifecycle_id"
	CardPathPickerDirectorySelectFieldName    = "path_picker_directory"
	CardPathPickerFileSelectFieldName         = "path_picker_file"
	CardTargetPickerModeFieldName             = "target_picker_mode"
	CardTargetPickerWorkspaceFieldName        = "target_picker_workspace"
	CardTargetPickerSessionFieldName          = "target_picker_session"
	CardTargetPickerSourceFieldName           = "target_picker_source"
	CardSelectionThreadFieldName              = "selection_thread"
	CardThreadHistoryTurnFieldName            = "thread_history_turn"
	CardActionPayloadDefaultCommandFieldName  = "command_args"
	CardActionKindAttachInstance              = "attach_instance"
	CardActionKindAttachWorkspace             = "attach_workspace"
	CardActionKindUseThread                   = "use_thread"
	CardActionKindShowScopedThreads           = "show_scoped_threads"
	CardActionKindShowThreads                 = "show_threads"
	CardActionKindShowAllThreads              = "show_all_threads"
	CardActionKindShowAllThreadWorkspaces     = "show_all_thread_workspaces"
	CardActionKindShowRecentThreadWorkspaces  = "show_recent_thread_workspaces"
	CardActionKindShowWorkspaceThreads        = "show_workspace_threads"
	CardActionKindShowAllWorkspaces           = "show_all_workspaces"
	CardActionKindShowRecentWorkspaces        = "show_recent_workspaces"
	CardActionKindKickThreadConfirm           = "kick_thread_confirm"
	CardActionKindKickThreadCancel            = "kick_thread_cancel"
	CardActionKindRequestRespond              = "request_respond"
	CardActionKindRequestControl              = "request_control"
	CardActionKindPageAction                  = "page_action"
	CardActionKindUpgradeOwnerFlow            = "upgrade_owner_flow"
	CardActionKindVSCodeMigrateOwnerFlow      = "vscode_migrate_owner_flow"
	CardActionKindPlanProposal                = "plan_proposal"
	CardActionKindPageSubmit                  = "page_submit"
	CardActionKindSubmitRequestForm           = "submit_request_form"
	CardActionKindPathPickerEnter             = "path_picker_enter"
	CardActionKindPathPickerUp                = "path_picker_up"
	CardActionKindPathPickerSelect            = "path_picker_select"
	CardActionKindPathPickerPage              = "path_picker_page"
	CardActionKindPathPickerConfirm           = "path_picker_confirm"
	CardActionKindPathPickerCancel            = "path_picker_cancel"
	CardActionKindTargetPickerSelectMode      = "target_picker_select_mode"
	CardActionKindTargetPickerSelectSource    = "target_picker_select_source"
	CardActionKindTargetPickerSelectWorkspace = "target_picker_select_workspace"
	CardActionKindTargetPickerSelectSession   = "target_picker_select_session"
	CardActionKindTargetPickerPage            = "target_picker_page"
	CardActionKindTargetPickerOpenPathPicker  = "target_picker_open_path_picker"
	CardActionKindTargetPickerBack            = "target_picker_back"
	CardActionKindTargetPickerCancel          = "target_picker_cancel"
	CardActionKindTargetPickerConfirm         = "target_picker_confirm"
	CardActionKindHistoryPage                 = "history_page"
	CardActionKindHistoryDetail               = "history_detail"
)

func ActionPayloadKind(value map[string]any) string {
	return strings.TrimSpace(stringMapValue(value, CardActionPayloadKeyKind))
}

func ActionPayloadWithLifecycle(value map[string]any, daemonLifecycleID string) map[string]any {
	if len(value) == 0 {
		return value
	}
	if strings.TrimSpace(daemonLifecycleID) == "" {
		return value
	}
	value[CardActionPayloadKeyDaemonLifecycleID] = strings.TrimSpace(daemonLifecycleID)
	return value
}

func ActionPayloadNavigation(kind string) map[string]any {
	return map[string]any{CardActionPayloadKeyKind: kind}
}

func ActionPayloadNavigationPage(kind string, page int) map[string]any {
	payload := ActionPayloadNavigation(kind)
	if page > 0 {
		payload[CardActionPayloadKeyPage] = page
	}
	return payload
}

func ActionPayloadThreadNavigation(kind, viewMode string, page int) map[string]any {
	payload := ActionPayloadNavigationPage(kind, page)
	if strings.TrimSpace(viewMode) != "" {
		payload[CardActionPayloadKeyViewMode] = strings.TrimSpace(viewMode)
	}
	return payload
}

func ActionPayloadWorkspaceThreads(workspaceKey string, page, returnPage int) map[string]any {
	payload := map[string]any{
		CardActionPayloadKeyKind:         CardActionKindShowWorkspaceThreads,
		CardActionPayloadKeyWorkspaceKey: strings.TrimSpace(workspaceKey),
	}
	if page > 0 {
		payload[CardActionPayloadKeyPage] = page
	}
	if returnPage > 0 {
		payload[CardActionPayloadKeyReturnPage] = returnPage
	}
	return payload
}

func ActionPayloadAttachInstance(instanceID string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:       CardActionKindAttachInstance,
		CardActionPayloadKeyInstanceID: strings.TrimSpace(instanceID),
	}
}

func ActionPayloadAttachWorkspace(workspaceKey string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:         CardActionKindAttachWorkspace,
		CardActionPayloadKeyWorkspaceKey: strings.TrimSpace(workspaceKey),
	}
}

func ActionPayloadUseThread(threadID string, allowCrossWorkspace bool) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:                CardActionKindUseThread,
		CardActionPayloadKeyThreadID:            strings.TrimSpace(threadID),
		CardActionPayloadKeyAllowCrossWorkspace: allowCrossWorkspace,
	}
}

func ActionPayloadUseThreadField(fieldName string, allowCrossWorkspace bool) map[string]any {
	payload := ActionPayloadUseThread("", allowCrossWorkspace)
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = CardSelectionThreadFieldName
	}
	payload[CardActionPayloadKeyFieldName] = fieldName
	return payload
}

func ActionPayloadKickThreadConfirm(threadID string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:     CardActionKindKickThreadConfirm,
		CardActionPayloadKeyThreadID: strings.TrimSpace(threadID),
	}
}

func ActionPayloadPageAction(actionKind, actionArg string) map[string]any {
	payload := map[string]any{
		CardActionPayloadKeyKind:       CardActionKindPageAction,
		CardActionPayloadKeyActionKind: strings.TrimSpace(actionKind),
	}
	if strings.TrimSpace(actionArg) != "" {
		payload[CardActionPayloadKeyActionArg] = strings.TrimSpace(actionArg)
	}
	return payload
}

func ActionPayloadUpgradeOwnerFlow(flowID, optionID string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:     CardActionKindUpgradeOwnerFlow,
		CardActionPayloadKeyPickerID: strings.TrimSpace(flowID),
		CardActionPayloadKeyOptionID: strings.TrimSpace(optionID),
	}
}

func ActionPayloadVSCodeMigrateOwnerFlow(flowID, optionID string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:     CardActionKindVSCodeMigrateOwnerFlow,
		CardActionPayloadKeyPickerID: strings.TrimSpace(flowID),
		CardActionPayloadKeyOptionID: strings.TrimSpace(optionID),
	}
}

func ActionPayloadPlanProposal(flowID, optionID string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:     CardActionKindPlanProposal,
		CardActionPayloadKeyPickerID: strings.TrimSpace(flowID),
		CardActionPayloadKeyOptionID: strings.TrimSpace(optionID),
	}
}

func ActionPayloadPageSubmit(actionKind, actionArgPrefix, fieldName string) map[string]any {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = CardActionPayloadDefaultCommandFieldName
	}
	payload := map[string]any{
		CardActionPayloadKeyKind:       CardActionKindPageSubmit,
		CardActionPayloadKeyActionKind: strings.TrimSpace(actionKind),
		CardActionPayloadKeyFieldName:  fieldName,
	}
	if strings.TrimSpace(actionArgPrefix) != "" {
		payload[CardActionPayloadKeyActionArgPrefix] = strings.TrimSpace(actionArgPrefix)
	}
	return payload
}

func ActionPayloadRequestRespond(requestID, requestType, requestOptionID string, requestAnswers map[string]any, requestRevision int) map[string]any {
	payload := map[string]any{
		CardActionPayloadKeyKind:            CardActionKindRequestRespond,
		CardActionPayloadKeyRequestID:       strings.TrimSpace(requestID),
		CardActionPayloadKeyRequestType:     strings.TrimSpace(requestType),
		CardActionPayloadKeyRequestOptionID: strings.TrimSpace(requestOptionID),
	}
	if len(requestAnswers) != 0 {
		payload[CardActionPayloadKeyRequestAnswers] = requestAnswers
	}
	if requestRevision > 0 {
		payload[CardActionPayloadKeyRequestRevision] = requestRevision
	}
	return payload
}

func ActionPayloadRequestControl(requestID, requestType, requestControl, questionID string, requestRevision int) map[string]any {
	payload := map[string]any{
		CardActionPayloadKeyKind:           CardActionKindRequestControl,
		CardActionPayloadKeyRequestID:      strings.TrimSpace(requestID),
		CardActionPayloadKeyRequestType:    strings.TrimSpace(requestType),
		CardActionPayloadKeyRequestControl: strings.TrimSpace(requestControl),
	}
	if strings.TrimSpace(questionID) != "" {
		payload[CardActionPayloadKeyQuestionID] = strings.TrimSpace(questionID)
	}
	if requestRevision > 0 {
		payload[CardActionPayloadKeyRequestRevision] = requestRevision
	}
	return payload
}

func ActionPayloadSubmitRequestForm(requestID, requestType string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:        CardActionKindSubmitRequestForm,
		CardActionPayloadKeyRequestID:   strings.TrimSpace(requestID),
		CardActionPayloadKeyRequestType: strings.TrimSpace(requestType),
	}
}

func ActionPayloadPathPicker(kind, pickerID, entryName string) map[string]any {
	payload := map[string]any{
		CardActionPayloadKeyKind:     strings.TrimSpace(kind),
		CardActionPayloadKeyPickerID: strings.TrimSpace(pickerID),
	}
	if strings.TrimSpace(entryName) != "" {
		payload[CardActionPayloadKeyEntryName] = strings.TrimSpace(entryName)
	}
	return payload
}

func ActionPayloadPathPickerCursor(pickerID, fieldName string, cursor int) map[string]any {
	payload := ActionPayloadPathPicker(CardActionKindPathPickerPage, pickerID, "")
	if strings.TrimSpace(fieldName) != "" {
		payload[CardActionPayloadKeyFieldName] = strings.TrimSpace(fieldName)
	}
	if cursor > 0 {
		payload[CardActionPayloadKeyCursor] = cursor
	}
	return payload
}

func ActionPayloadTargetPicker(kind, pickerID string) map[string]any {
	return map[string]any{
		CardActionPayloadKeyKind:     strings.TrimSpace(kind),
		CardActionPayloadKeyPickerID: strings.TrimSpace(pickerID),
	}
}

func ActionPayloadTargetPickerValue(kind, pickerID, targetValue string) map[string]any {
	payload := ActionPayloadTargetPicker(kind, pickerID)
	if strings.TrimSpace(targetValue) != "" {
		payload[CardActionPayloadKeyTargetValue] = strings.TrimSpace(targetValue)
	}
	return payload
}

func ActionPayloadTargetPickerCursor(pickerID, fieldName string, cursor int) map[string]any {
	payload := ActionPayloadTargetPicker(CardActionKindTargetPickerPage, pickerID)
	if strings.TrimSpace(fieldName) != "" {
		payload[CardActionPayloadKeyFieldName] = strings.TrimSpace(fieldName)
	}
	if cursor > 0 {
		payload[CardActionPayloadKeyCursor] = cursor
	}
	return payload
}

func ActionPayloadThreadHistory(kind, pickerID, turnID string, page int) map[string]any {
	payload := map[string]any{
		CardActionPayloadKeyKind:     strings.TrimSpace(kind),
		CardActionPayloadKeyPickerID: strings.TrimSpace(pickerID),
	}
	if page > 0 {
		payload[CardActionPayloadKeyPage] = page
	}
	if strings.TrimSpace(turnID) != "" {
		payload[CardActionPayloadKeyTurnID] = strings.TrimSpace(turnID)
	}
	return payload
}

func stringMapValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	text, _ := values[key].(string)
	return text
}
