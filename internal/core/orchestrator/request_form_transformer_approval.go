package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func transformApprovalForm(form *RequestFormState, record *state.RequestPromptRecord, option string, answers map[string][]string) (map[string]any, bool, *RequestFeedbackCapture, error) {
	if record.SemanticKind == control.RequestSemanticPlanConfirmation {
		if record.StructuredForm != nil && option == "" {
			response, complete, invalid := buildPlanConfirmationPermissionSelectionResponse(record, answers)
			if invalid != "" {
				return nil, false, nil, formInvalid(invalid)
			}
			return response, complete, nil, nil
		}
		if option == "acceptForSession" {
			if !requestHasOption(record, option) {
				return nil, false, nil, formInvalid("本会话授权选项当前不可用。")
			}
			record.StructuredForm = buildPlanConfirmationStructuredForm(record, form.WorkspaceRoot)
			form.Options = nil
			for _, o := range planConfirmationPanelOptions() {
				form.Options = append(form.Options, state.RequestPromptOptionRecord{OptionID: o.OptionID, Label: o.Label, Style: o.Style})
			}
			return nil, false, nil, nil
		}
	}
	if !requestHasOption(record, option) {
		return nil, false, nil, formInvalid("确认选项无效或当前不可用。")
	}
	if len(answers) > 0 {
		return nil, false, nil, formInvalid("此确认按钮不接受额外表单答案。")
	}
	if option == "captureFeedback" || option == "revise" {
		mode := requestCaptureModeDeclineWithFeedback
		hint := "请发送对当前确认请求的处理意见。"
		if record.SemanticKind == control.RequestSemanticApprovalCanUseTool {
			mode = requestCaptureModeSameRequestDecline
		}
		if option == "revise" {
			mode = requestCaptureModePlanReviseFeedback
			hint = "请发送对当前计划的修改意见。"
		}
		form.FeedbackMode = mode
		return nil, false, &RequestFeedbackCapture{Mode: mode, HintText: hint}, nil
	}
	decision := decisionForRequestOption(option)
	if decision == "" {
		return nil, false, nil, formInvalid("当前确认选项没有受支持的响应映射。")
	}
	return map[string]any{"type": "approval", "decision": decision}, true, nil, nil
}
func transformRequestFeedback(form *RequestFormState, record *state.RequestPromptRecord, text string) (RequestFormTransition, error) {
	text = strings.TrimSpace(text)
	if text == "" || form.FeedbackMode == "" {
		return RequestFormTransition{}, formInvalid("当前没有等待文字反馈的请求。")
	}
	response := map[string]any{"type": "approval", "decision": "decline"}
	followup := ""
	switch form.FeedbackMode {
	case requestCaptureModePlanReviseFeedback:
		response["decision"] = "revise"
		response["message"] = text
	case requestCaptureModeSameRequestDecline:
		response["message"] = text
	case requestCaptureModeDeclineWithFeedback:
		followup = text
	default:
		return RequestFormTransition{}, formInvalid("当前反馈模式无效。")
	}
	form.FeedbackMode = ""
	form.update(record)
	form.Submitted = true
	return RequestFormTransition{NextForm: *form, Response: response, Complete: true, FollowupHumanInput: followup}, nil
}
