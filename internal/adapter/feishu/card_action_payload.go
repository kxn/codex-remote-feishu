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
	cardActionPayloadKeyCommandText           = "command_text"
	cardActionPayloadKeyCommandLegacy         = "command"
	cardActionPayloadKeyFieldName             = "field_name"
	cardActionPayloadKeyApproved              = "approved"
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
	cardThreadHistoryTurnFieldName            = "thread_history_turn"
	cardActionPayloadDefaultCommandFieldName  = "command_args"
	cardActionKindAttachInstance              = "attach_instance"
	cardActionKindAttachWorkspace             = "attach_workspace"
	cardActionKindCreateWorkspace             = "create_workspace"
	cardActionKindUseThread                   = "use_thread"
	cardActionKindShowScopedThreads           = "show_scoped_threads"
	cardActionKindShowThreads                 = "show_threads"
	cardActionKindShowAllThreads              = "show_all_threads"
	cardActionKindShowAllThreadWorkspaces     = "show_all_thread_workspaces"
	cardActionKindShowRecentThreadWorkspaces  = "show_recent_thread_workspaces"
	cardActionKindShowWorkspaceThreads        = "show_workspace_threads"
	cardActionKindShowAllWorkspaces           = "show_all_workspaces"
	cardActionKindShowRecentWorkspaces        = "show_recent_workspaces"
	cardActionKindResumeHeadlessThread        = "resume_headless_thread"
	cardActionKindKickThreadConfirm           = "kick_thread_confirm"
	cardActionKindKickThreadCancel            = "kick_thread_cancel"
	cardActionKindPromptSelect                = "prompt_select"
	cardActionKindRequestRespond              = "request_respond"
	cardActionKindRunCommand                  = "run_command"
	cardActionKindStartCommandCapture         = "start_command_capture"
	cardActionKindCancelCommandCapture        = "cancel_command_capture"
	cardActionKindSubmitCommandForm           = "submit_command_form"
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

func actionPayloadCreateWorkspace() map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind: cardActionKindCreateWorkspace,
	}
}

func actionPayloadUseThread(threadID string, allowCrossWorkspace bool) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:                cardActionKindUseThread,
		cardActionPayloadKeyThreadID:            strings.TrimSpace(threadID),
		cardActionPayloadKeyAllowCrossWorkspace: allowCrossWorkspace,
	}
}

func actionPayloadKickThreadConfirm(threadID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:     cardActionKindKickThreadConfirm,
		cardActionPayloadKeyThreadID: strings.TrimSpace(threadID),
	}
}

func actionPayloadPromptSelect(promptID, optionID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:     cardActionKindPromptSelect,
		cardActionPayloadKeyPromptID: strings.TrimSpace(promptID),
		cardActionPayloadKeyOptionID: strings.TrimSpace(optionID),
	}
}

func actionPayloadRunCommand(commandText string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:        cardActionKindRunCommand,
		cardActionPayloadKeyCommandText: strings.TrimSpace(commandText),
	}
}

func actionPayloadStartCommandCapture(commandID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:      cardActionKindStartCommandCapture,
		cardActionPayloadKeyCommandID: strings.TrimSpace(commandID),
	}
}

func actionPayloadCancelCommandCapture(commandID string) map[string]any {
	return map[string]any{
		cardActionPayloadKeyKind:      cardActionKindCancelCommandCapture,
		cardActionPayloadKeyCommandID: strings.TrimSpace(commandID),
	}
}

func actionPayloadSubmitCommandForm(commandID, commandText, fieldName string) map[string]any {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		fieldName = cardActionPayloadDefaultCommandFieldName
	}
	return map[string]any{
		cardActionPayloadKeyKind:          cardActionKindSubmitCommandForm,
		cardActionPayloadKeyCommandID:     strings.TrimSpace(commandID),
		cardActionPayloadKeyCommandLegacy: strings.TrimSpace(commandText),
		cardActionPayloadKeyFieldName:     fieldName,
	}
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
