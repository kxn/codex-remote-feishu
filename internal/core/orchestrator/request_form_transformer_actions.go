package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// TransformRequestForm is pure: the input snapshot is deeply cloned, invalid
// actions cannot mutate it, and no surface/session/time/native authority exists.
func TransformRequestForm(input RequestFormState, action HumanRequestFormAction) (RequestFormTransition, error) {
	form, err := cloneFormValue(input)
	if err != nil {
		return RequestFormTransition{}, err
	}
	original := RequestFormTransition{NextForm: input}
	if form.Revision < 1 {
		return original, formInvalid("请求表单版本无效。")
	}
	if form.Submitted {
		return original, formInvalid("这份表单已经提交，请等待原请求处理。")
	}
	record := form.record()
	option := control.NormalizeRequestOptionID(strings.TrimSpace(action.OptionID))
	if action.Feedback != "" {
		if option != "" || len(action.Answers) > 0 || action.SkipQuestionID != "" {
			return original, formInvalid("反馈不能混用其他表单动作。")
		}
		transition, err := transformRequestFeedback(&form, record, action.Feedback)
		if err != nil {
			return original, err
		}
		return transition, nil
	}
	if form.FeedbackMode != "" {
		return original, formInvalid("当前正在等待反馈文字。")
	}
	if err = validateFormAnswerFields(record, action.Answers); err != nil {
		return original, err
	}
	if action.SkipQuestionID != "" {
		if option != "" || len(action.Answers) > 0 {
			return original, formInvalid("跳过问题不能混用答案或其他动作。")
		}
		question, _, ok := requestPromptCurrentQuestionRecord(record)
		if !ok || !question.Optional || strings.TrimSpace(question.ID) != strings.TrimSpace(action.SkipQuestionID) {
			return original, formInvalid("当前问题不可跳过，或问题版本已经变化。")
		}
		markRequestQuestionSkipped(record, question.ID)
	}
	if requestPromptStepPrevious(option) || requestPromptStepNext(option) {
		if len(action.Answers) > 0 {
			return original, formInvalid("翻页动作不能同时提交答案。")
		}
		if record.StructuredForm != nil && record.SemanticKind == control.RequestSemanticPlanConfirmation {
			if !requestPromptStepPrevious(option) {
				return original, formInvalid("当前授权面板没有下一页。")
			}
			record.StructuredForm = nil
			definition, invalid := buildRequestPromptPresentationDefinition(form.Backend, form.Prompt, form.Metadata)
			if invalid != "" {
				return original, formInvalid(invalid)
			}
			form.Options = definition.Options
		} else if len(record.Questions) == 0 {
			return original, formInvalid("当前请求没有可翻页的问题。")
		} else {
			delta := 1
			if requestPromptStepPrevious(option) {
				delta = -1
			}
			moveRequestPromptCurrentQuestion(record, delta)
		}
		form.update(record)
		return RequestFormTransition{NextForm: form, NeedsRefresh: true}, nil
	}
	var response map[string]any
	complete := false
	invalid := ""
	switch record.RequestType {
	case "approval":
		var capture *RequestFeedbackCapture
		response, complete, capture, err = transformApprovalForm(&form, record, option, action.Answers)
		if err != nil {
			return original, err
		}
		if capture != nil {
			form.update(record)
			return RequestFormTransition{NextForm: form, Capture: capture, NeedsRefresh: true}, nil
		}
	case "request_user_input":
		if option != "" {
			return original, formInvalid("问题表单不支持此确认按钮。")
		}
		response, complete, invalid = buildRequestUserInputResponse(record, action.Answers)
	case "permissions_request_approval":
		if !requestHasOption(record, option) {
			return original, formInvalid("权限选项无效或当前不可用。")
		}
		response, complete, _ = buildPermissionsRequestResponse(record, control.Action{Request: &control.ActionRequestResponse{RequestOptionID: option}})
	case "mcp_server_elicitation":
		response, complete, invalid = transformMCPForm(form, record, option, action.Answers)
	default:
		return original, formInvalid("当前请求类型不支持交互表单响应。")
	}
	if invalid != "" {
		return original, formInvalid(invalid)
	}
	if response == nil && !complete && len(action.Answers) == 0 && action.SkipQuestionID == "" && record.StructuredForm == nil {
		return original, formInvalid(requestCurrentQuestionPendingText(record))
	}
	if !complete && len(record.Questions) > 0 {
		setRequestPromptCurrentQuestionIndex(record, firstIncompleteRequestQuestionIndex(record))
	}
	form.update(record)
	form.Submitted = complete
	return RequestFormTransition{NextForm: form, Response: response, Complete: complete, NeedsRefresh: !complete}, nil
}
func validateFormAnswerFields(record *state.RequestPromptRecord, answers map[string][]string) error {
	allowed := map[string]bool{}
	for _, question := range record.Questions {
		allowed[question.ID] = true
	}
	if record.StructuredForm != nil {
		for _, field := range record.StructuredForm.Fields {
			allowed[field.Name] = true
		}
	}
	for name := range answers {
		if !allowed[name] {
			return formInvalid("答案引用了当前表单中不存在的字段。")
		}
	}
	return nil
}
