package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func normalizeRequestType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "", normalized == "approval", normalized == "confirm", normalized == "confirmation":
		return strings.ToLower(strings.TrimSpace(firstNonEmpty(value, "approval")))
	case strings.HasPrefix(normalized, "approval"):
		return "approval"
	case strings.HasPrefix(normalized, "confirm"):
		return "approval"
	case normalized == "request_user_input", normalized == "requestuserinput":
		return "request_user_input"
	case normalized == "permissions_request_approval", normalized == "permissionsrequestapproval":
		return "permissions_request_approval"
	case normalized == "mcp_server_elicitation", normalized == "mcpserverelicitation":
		return "mcp_server_elicitation"
	case normalized == "tool_callback", normalized == "toolcallback":
		return "tool_callback"
	default:
		return normalized
	}
}

func requestPromptRenderable(requestType string) bool {
	switch normalizeRequestType(requestType) {
	case "approval", "request_user_input", "permissions_request_approval", "mcp_server_elicitation", "tool_callback":
		return true
	default:
		return false
	}
}

func requestOptionIDFromApproved(approved bool) string {
	if approved {
		return "accept"
	}
	return "decline"
}

func requestHasOption(request *state.RequestPromptRecord, optionID string) bool {
	if request == nil {
		return false
	}
	if len(request.Options) == 0 {
		switch optionID {
		case "accept", "decline":
			return true
		default:
			return false
		}
	}
	for _, option := range request.Options {
		if control.NormalizeRequestOptionID(option.OptionID) == optionID {
			return true
		}
	}
	return false
}

func decisionForRequestOption(optionID string) string {
	switch control.NormalizeRequestOptionID(optionID) {
	case "accept":
		return "accept"
	case "acceptForSession":
		return "acceptForSession"
	case "decline":
		return "decline"
	case "cancel":
		return "cancel"
	default:
		return ""
	}
}

func activePendingRequest(surface *state.SurfaceConsoleRecord) *state.RequestPromptRecord {
	if surface == nil || len(surface.PendingRequests) == 0 {
		return nil
	}
	for requestID, request := range surface.PendingRequests {
		if request == nil {
			delete(surface.PendingRequests, requestID)
			continue
		}
		return request
	}
	return nil
}

func requestCaptureExpired(now time.Time, capture *state.RequestCaptureRecord) bool {
	if capture == nil || capture.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(capture.ExpiresAt)
}

func requestPromptOptionsToControl(options []state.RequestPromptOptionRecord) []control.RequestPromptOption {
	if len(options) == 0 {
		return nil
	}
	out := make([]control.RequestPromptOption, 0, len(options))
	for _, option := range options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			continue
		}
		out = append(out, control.RequestPromptOption{
			OptionID: strings.TrimSpace(option.OptionID),
			Label:    label,
			Style:    strings.TrimSpace(option.Style),
		})
	}
	return out
}

func requestPromptQuestionsToControl(questions []state.RequestPromptQuestionRecord, draftAnswers map[string]string, skippedQuestionIDs map[string]bool) []control.RequestPromptQuestion {
	if len(questions) == 0 {
		return nil
	}
	out := make([]control.RequestPromptQuestion, 0, len(questions))
	for _, question := range questions {
		questionID := strings.TrimSpace(question.ID)
		if questionID == "" {
			continue
		}
		options := make([]control.RequestPromptQuestionOption, 0, len(question.Options))
		for _, option := range question.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" {
				continue
			}
			options = append(options, control.RequestPromptQuestionOption{
				Label:       label,
				Description: strings.TrimSpace(option.Description),
			})
		}
		draftAnswer := strings.TrimSpace(draftAnswers[questionID])
		answered := draftAnswer != ""
		defaultValue := strings.TrimSpace(question.DefaultValue)
		if !question.Secret {
			defaultValue = firstNonEmpty(draftAnswer, defaultValue)
		}
		out = append(out, control.RequestPromptQuestion{
			ID:             questionID,
			Header:         strings.TrimSpace(question.Header),
			Question:       strings.TrimSpace(question.Question),
			Answered:       answered,
			Skipped:        question.Optional && skippedQuestionIDs != nil && skippedQuestionIDs[questionID],
			Optional:       question.Optional,
			AllowOther:     question.AllowOther,
			Secret:         question.Secret,
			Options:        options,
			Placeholder:    strings.TrimSpace(question.Placeholder),
			DefaultValue:   defaultValue,
			DirectResponse: question.DirectResponse,
		})
	}
	return out
}

func buildApprovalRequestOptions(backend agentproto.Backend, semanticKind string, metadata map[string]any) []state.RequestPromptOptionRecord {
	var options []state.RequestPromptOptionRecord
	seen := map[string]bool{}
	semanticKind = control.NormalizeRequestSemanticKind(semanticKind, "approval")
	add := func(optionID, label, style string) {
		optionID = control.NormalizeRequestOptionID(optionID)
		if optionID == "" || seen[optionID] {
			return
		}
		switch optionID {
		case "accept", "acceptForSession", "decline", "cancel", "captureFeedback":
		default:
			return
		}
		if label == "" {
			switch optionID {
			case "accept":
				label = "允许一次"
			case "acceptForSession":
				label = "本会话允许"
			case "decline":
				label = "拒绝"
			case "cancel":
				label = "取消"
			case "captureFeedback":
				label = requestFeedbackActionLabel(backend)
			default:
				return
			}
		}
		if style == "" {
			switch optionID {
			case "accept":
				style = "primary"
			default:
				style = "default"
			}
		}
		options = append(options, state.RequestPromptOptionRecord{
			OptionID: optionID,
			Label:    label,
			Style:    style,
		})
		seen[optionID] = true
	}

	upstreamOptions := metadataRequestOptions(metadata)
	for _, option := range upstreamOptions {
		add(option.OptionID, option.Label, option.Style)
	}
	if len(upstreamOptions) == 0 {
		add("accept", firstNonEmpty(metadataString(metadata, "acceptLabel"), "允许一次"), "primary")
		if approvalRequestSupportsSession(semanticKind) {
			add("acceptForSession", "本会话允许", "default")
		}
		add("decline", firstNonEmpty(metadataString(metadata, "declineLabel"), "拒绝"), "default")
		if approvalRequestSupportsCancel(semanticKind) {
			add("cancel", "取消", "default")
		}
	}
	add("captureFeedback", requestFeedbackActionLabel(backend), "default")
	return options
}

func metadataRequestQuestions(metadata map[string]any) []state.RequestPromptQuestionRecord {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["questions"]
	if !ok {
		return nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []map[string]any:
		values = make([]any, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
	default:
		return nil
	}
	questions := make([]state.RequestPromptQuestionRecord, 0, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		questionID := firstNonEmpty(
			lookupStringFromAny(record["id"]),
			lookupStringFromAny(record["questionId"]),
		)
		if questionID == "" {
			continue
		}
		options := make([]state.RequestPromptQuestionOptionRecord, 0)
		rawOptions := record["options"]
		if rawOptions == nil {
			rawOptions = record["choices"]
		}
		switch typed := rawOptions.(type) {
		case []any:
			for _, item := range typed {
				option, ok := item.(map[string]any)
				if !ok {
					continue
				}
				label := firstNonEmpty(
					lookupStringFromAny(option["label"]),
					lookupStringFromAny(option["title"]),
					lookupStringFromAny(option["text"]),
				)
				if label == "" {
					continue
				}
				options = append(options, state.RequestPromptQuestionOptionRecord{
					Label:       label,
					Description: firstNonEmpty(lookupStringFromAny(option["description"]), lookupStringFromAny(option["subtitle"])),
				})
			}
		case []map[string]any:
			for _, option := range typed {
				label := firstNonEmpty(
					lookupStringFromAny(option["label"]),
					lookupStringFromAny(option["title"]),
					lookupStringFromAny(option["text"]),
				)
				if label == "" {
					continue
				}
				options = append(options, state.RequestPromptQuestionOptionRecord{
					Label:       label,
					Description: firstNonEmpty(lookupStringFromAny(option["description"]), lookupStringFromAny(option["subtitle"])),
				})
			}
		}
		header := firstNonEmpty(
			lookupStringFromAny(record["header"]),
			lookupStringFromAny(record["title"]),
		)
		questionText := firstNonEmpty(
			lookupStringFromAny(record["question"]),
			lookupStringFromAny(record["label"]),
			lookupStringFromAny(record["prompt"]),
		)
		placeholder := firstNonEmpty(
			lookupStringFromAny(record["placeholder"]),
			lookupStringFromAny(record["inputPlaceholder"]),
		)
		directResponse := lookupBoolFromAny(record["directResponse"])
		if !directResponse && record["directResponse"] == nil {
			directResponse = len(options) != 0
		}
		if placeholder == "" && len(options) != 0 {
			labels := make([]string, 0, len(options))
			for _, option := range options {
				labels = append(labels, option.Label)
			}
			placeholder = "可填写：" + strings.Join(labels, " / ")
		}
		questions = append(questions, state.RequestPromptQuestionRecord{
			ID:             questionID,
			Header:         header,
			Question:       questionText,
			Optional:       lookupBoolFromAny(record["optional"]) || (record["required"] != nil && !lookupBoolFromAny(record["required"])),
			AllowOther:     lookupBoolFromAny(record["isOther"]),
			Secret:         lookupBoolFromAny(record["isSecret"]),
			Options:        options,
			Placeholder:    placeholder,
			DefaultValue:   strings.TrimSpace(lookupStringFromAny(record["defaultValue"])),
			DirectResponse: directResponse,
		})
	}
	return questions
}

func approvalRequestSupportsSession(semanticKind string) bool {
	switch control.NormalizeRequestSemanticKind(semanticKind, "approval") {
	case control.RequestSemanticApprovalCommand, control.RequestSemanticApprovalFileChange, control.RequestSemanticApprovalNetwork:
		return true
	default:
		return false
	}
}

func approvalRequestSupportsCancel(semanticKind string) bool {
	switch control.NormalizeRequestSemanticKind(semanticKind, "approval") {
	case control.RequestSemanticApprovalCommand, control.RequestSemanticApprovalFileChange, control.RequestSemanticApprovalNetwork:
		return true
	default:
		return false
	}
}

func metadataRequestOptions(metadata map[string]any) []state.RequestPromptOptionRecord {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["options"]
	if !ok {
		return nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []map[string]any:
		values = make([]any, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
	default:
		return nil
	}
	options := make([]state.RequestPromptOptionRecord, 0, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		optionID := firstNonEmpty(
			lookupStringFromAny(record["id"]),
			lookupStringFromAny(record["optionId"]),
			lookupStringFromAny(record["decision"]),
			lookupStringFromAny(record["value"]),
			lookupStringFromAny(record["action"]),
		)
		optionID = control.NormalizeRequestOptionID(optionID)
		if optionID == "" {
			continue
		}
		label := firstNonEmpty(
			lookupStringFromAny(record["label"]),
			lookupStringFromAny(record["title"]),
			lookupStringFromAny(record["text"]),
			lookupStringFromAny(record["name"]),
		)
		style := firstNonEmpty(
			lookupStringFromAny(record["style"]),
			lookupStringFromAny(record["appearance"]),
			lookupStringFromAny(record["variant"]),
		)
		options = append(options, state.RequestPromptOptionRecord{
			OptionID: optionID,
			Label:    label,
			Style:    style,
		})
	}
	return options
}

func pendingRequestNoticeText(request *state.RequestPromptRecord) string {
	waitingText := requestWaitingContinueText(requestPromptBackend(request))
	if request == nil {
		return "当前有待处理请求。"
	}
	switch requestPromptSemanticKind(request) {
	case control.RequestSemanticRequestUserInput:
		return "当前有待回答问题。请先在卡片上点击选项、提交当前答案，或跳过可选题。"
	case control.RequestSemanticApprovalCommand:
		return "当前有待确认执行命令请求。请先处理这张确认卡片后再继续。"
	case control.RequestSemanticApprovalFileChange:
		return "当前有待确认文件修改请求。请先处理这张确认卡片后再继续。"
	case control.RequestSemanticApprovalNetwork:
		return "当前有待确认网络访问请求。请先处理这张确认卡片后再继续。"
	case control.RequestSemanticApproval:
		return "当前有待确认请求。请先点击卡片上的处理按钮后再继续。"
	case control.RequestSemanticPermissionsRequestApproval:
		return "当前有待授予权限请求。请先在卡片上选择“允许本次”、“本会话允许”或“拒绝”。"
	case control.RequestSemanticMCPServerElicitation, control.RequestSemanticMCPServerElicitationForm, control.RequestSemanticMCPServerElicitationURL:
		return "当前有待处理的 MCP 请求。请先在卡片上填写返回内容、提交当前答案，或取消请求。"
	case control.RequestSemanticToolCallback:
		return "当前有工具回调正在自动上报 unsupported 结果。请" + waitingText + "，或使用 /stop 结束当前 turn。"
	default:
		return "当前有待处理请求。这个请求类型暂时不能在飞书端直接处理，请先回到本地处理或等待后续支持。"
	}
}
