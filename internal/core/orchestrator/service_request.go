package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"strconv"
	"strings"
	"time"
)

const (
	requestUserInputSubmitOptionID                      = "submit"
	requestUserInputSubmitWithUnansweredOptionID        = "submit_with_unanswered"
	requestUserInputConfirmSubmitWithUnansweredOptionID = "confirm_submit_with_unanswered"
	requestUserInputCancelSubmitWithUnansweredOptionID  = "cancel_submit_with_unanswered"
)

func (s *Service) respondRequest(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || action.RequestID == "" {
		return nil
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	request := surface.PendingRequests[action.RequestID]
	if request == nil {
		return notice(surface, "request_expired", "这个确认请求已经结束或过期了。")
	}
	if request.PendingDispatchCommandID != "" {
		return notice(surface, "request_pending_dispatch", "这条请求已经提交，正在等待本地 Codex 处理。")
	}
	if action.RequestRevision != 0 && action.RequestRevision != request.CardRevision {
		return notice(surface, "request_card_expired", "这张请求卡片已经过期，请使用最新卡片继续操作。")
	}
	requestType := normalizeRequestType(firstNonEmpty(action.RequestType, request.RequestType))
	if requestType == "" {
		requestType = "approval"
	}
	response, _, followup := s.buildRequestResponse(surface, request, action, requestType)
	if followup != nil {
		return followup
	}
	if response == nil {
		return nil
	}
	request.PendingDispatchCommandID = s.nextRequestDispatchCommandID()
	return []control.UIEvent{{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			CommandID: request.PendingDispatchCommandID,
			Kind:      agentproto.CommandRequestRespond,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    surface.ActorUserID,
				ChatID:    surface.ChatID,
				MessageID: action.MessageID,
			},
			Target: agentproto.Target{
				ThreadID:               request.ThreadID,
				TurnID:                 request.TurnID,
				UseActiveTurnIfOmitted: request.TurnID == "",
			},
			Request: agentproto.Request{
				RequestID: request.RequestID,
				Response:  response,
			},
		},
	}}
}

func (s *Service) presentRequestPrompt(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.RequestID == "" {
		return nil
	}
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	requestType := normalizeRequestType(metadataString(event.Metadata, "requestType"))
	if requestType == "" {
		requestType = "approval"
	}
	inst := s.root.Instances[instanceID]
	var thread *state.ThreadRecord
	if inst != nil {
		thread = inst.Threads[event.ThreadID]
	}
	threadTitle := displayThreadTitle(inst, thread, event.ThreadID)
	title := firstNonEmpty(metadataString(event.Metadata, "title"), "需要处理请求")
	body := strings.TrimSpace(metadataString(event.Metadata, "body"))
	options := []state.RequestPromptOptionRecord(nil)
	questions := []state.RequestPromptQuestionRecord(nil)
	switch requestType {
	case "approval":
		if title == "需要处理请求" {
			title = "需要确认"
		}
		if body == "" {
			body = "本地 Codex 正在等待你的确认。"
		}
		options = buildApprovalRequestOptions(event.Metadata)
	case "request_user_input":
		if title == "需要处理请求" {
			title = "需要补充输入"
		}
		if body == "" {
			body = "本地 Codex 正在等待你补充参数或说明。"
		}
		questions = metadataRequestQuestions(event.Metadata)
		if len(questions) == 0 {
			return notice(surface, "request_unsupported", "收到缺少问题定义的 request_user_input 请求，当前无法在飞书端处理。")
		}
	default:
		return notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
	record := &state.RequestPromptRecord{
		RequestID:    event.RequestID,
		RequestType:  requestType,
		InstanceID:   instanceID,
		ThreadID:     event.ThreadID,
		TurnID:       event.TurnID,
		ItemID:       strings.TrimSpace(metadataString(event.Metadata, "itemId")),
		Title:        title,
		Body:         body,
		Options:      options,
		Questions:    questions,
		CardRevision: 1,
		CreatedAt:    s.now(),
	}
	surface.PendingRequests[event.RequestID] = record
	return []control.UIEvent{s.requestPromptEvent(surface, record, threadTitle)}
}

func (s *Service) resolveRequestPrompt(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.RequestID != "" {
		for _, surface := range s.findAttachedSurfaces(instanceID) {
			if surface.PendingRequests == nil {
				continue
			}
			delete(surface.PendingRequests, event.RequestID)
			clearSurfaceRequestCaptureByRequestID(surface, event.RequestID)
		}
		return nil
	}
	s.clearRequestsForTurn(instanceID, event.ThreadID, event.TurnID)
	return nil
}

func (s *Service) consumeCapturedRequestFeedback(surface *state.SurfaceConsoleRecord, action control.Action, text string) []control.UIEvent {
	capture := surface.ActiveRequestCapture
	if requestCaptureExpired(s.now(), capture) {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "request_capture_expired", "上一条确认反馈已过期，请重新点击卡片按钮后再发送处理意见。")
	}
	if capture == nil || capture.Mode != requestCaptureModeDeclineWithFeedback {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "request_capture_expired", "当前反馈模式已失效，请重新处理确认卡片。")
	}
	request := surface.PendingRequests[capture.RequestID]
	if request == nil {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "request_expired", "这个确认请求已经结束或过期了。请重新发送消息。")
	}
	inst := s.root.Instances[request.InstanceID]
	if inst == nil {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "not_attached", s.attachedTargetUnavailableText(surface))
	}

	threadID := request.ThreadID
	cwd := inst.WorkspaceRoot
	routeMode := state.RouteModePinned
	if thread := inst.Threads[threadID]; threadVisible(thread) && thread.CWD != "" {
		cwd = thread.CWD
	}
	if threadID == "" {
		var createThread bool
		threadID, cwd, routeMode, createThread = freezeRoute(inst, surface)
		_ = createThread
	}

	clearSurfaceRequestCapture(surface)
	events := []control.UIEvent{{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandRequestRespond,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    surface.ActorUserID,
				ChatID:    surface.ChatID,
				MessageID: action.MessageID,
			},
			Target: agentproto.Target{
				ThreadID:               request.ThreadID,
				TurnID:                 request.TurnID,
				UseActiveTurnIfOmitted: request.TurnID == "",
			},
			Request: agentproto.Request{
				RequestID: request.RequestID,
				Response: map[string]any{
					"type":     "approval",
					"decision": "decline",
				},
			},
		},
	}}
	events = append(events, notice(surface, "request_feedback_queued", "已记录处理意见。当前确认会先被拒绝，随后继续处理你的下一步要求。")...)
	events = append(events, s.enqueueQueueItem(surface, action.MessageID, text, nil, []agentproto.Input{{Type: agentproto.InputText, Text: text}}, threadID, cwd, routeMode, surface.PromptOverride, true)...)
	return events
}

func (s *Service) buildRequestResponse(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action, requestType string) (map[string]any, bool, []control.UIEvent) {
	switch requestType {
	case "approval":
		optionID := control.NormalizeRequestOptionID(firstNonEmpty(action.RequestOptionID, requestOptionIDFromApproved(action.Approved)))
		if optionID == "" {
			return nil, false, notice(surface, "request_invalid", "这个确认按钮缺少有效的处理选项。")
		}
		if !requestHasOption(request, optionID) {
			return nil, false, notice(surface, "request_invalid", "这个确认按钮对应的选项无效或当前不可用。")
		}
		if optionID == "captureFeedback" {
			surface.ActiveRequestCapture = &state.RequestCaptureRecord{
				RequestID:   request.RequestID,
				RequestType: request.RequestType,
				InstanceID:  request.InstanceID,
				ThreadID:    request.ThreadID,
				TurnID:      request.TurnID,
				Mode:        requestCaptureModeDeclineWithFeedback,
				CreatedAt:   s.now(),
				ExpiresAt:   s.now().Add(10 * time.Minute),
			}
			return nil, false, notice(surface, "request_capture_started", "已进入反馈模式。接下来一条普通文本会作为对当前确认请求的处理意见，不会进入普通消息队列。")
		}
		decision := decisionForRequestOption(optionID)
		if decision == "" {
			return nil, false, notice(surface, "request_invalid", "这个确认按钮对应的决策暂不支持。")
		}
		clearSurfaceRequestCaptureByRequestID(surface, request.RequestID)
		return map[string]any{
			"type":     requestType,
			"decision": decision,
		}, false, nil
	case "request_user_input":
		if requestUserInputCancelSubmitWithUnanswered(action.RequestOptionID) {
			if !request.SubmitWithUnansweredConfirmPending {
				return nil, false, notice(surface, "request_invalid", "留空提交确认已失效，请先点击“提交答案”。")
			}
			clearRequestUserInputSubmitConfirmState(request)
			bumpRequestCardRevision(request)
			return nil, false, []control.UIEvent{s.requestPromptEvent(surface, request, "")}
		}
		if requestUserInputConfirmSubmitWithUnanswered(action.RequestOptionID) && !request.SubmitWithUnansweredConfirmPending {
			return nil, false, notice(surface, "request_invalid", "留空提交确认已失效，请先点击“提交答案”。")
		}
		submitIntent := requestUserInputSubmitIntent(action.RequestOptionID)
		allowSubmitWithUnanswered := requestUserInputAllowSubmitWithUnanswered(action.RequestOptionID)
		response, complete, missingLabels, errText := buildRequestUserInputResponse(request, action.RequestAnswers, allowSubmitWithUnanswered)
		if errText != "" {
			return nil, false, notice(surface, "request_invalid", errText)
		}
		if !complete {
			if submitIntent {
				setRequestUserInputSubmitConfirmState(request, missingLabels)
				bumpRequestCardRevision(request)
				return nil, false, []control.UIEvent{s.requestPromptEvent(surface, request, "")}
			}
			if len(action.RequestAnswers) == 0 {
				if len(missingLabels) != 0 {
					return nil, false, notice(surface, "request_invalid", fmt.Sprintf("问题“%s”还没有填写答案。", missingLabels[0]))
				}
				return nil, false, notice(surface, "request_invalid", "当前没有可提交的答案。")
			}
			if len(missingLabels) == 0 {
				return nil, false, notice(surface, "request_saved", "已记录当前答案，请继续补全其他问题后再提交。")
			}
			if len(missingLabels) == 1 {
				return nil, false, notice(surface, "request_saved", fmt.Sprintf("已记录当前答案。还差 1 个问题：%s。", missingLabels[0]))
			}
			return nil, false, notice(surface, "request_saved", fmt.Sprintf("已记录当前答案。还差 %d 个问题待填写。", len(missingLabels)))
		}
		clearRequestUserInputSubmitConfirmState(request)
		return response, true, nil
	default:
		return nil, false, notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
}

func buildRequestUserInputResponse(request *state.RequestPromptRecord, rawAnswers map[string][]string, allowSubmitWithUnanswered bool) (map[string]any, bool, []string, string) {
	if request == nil || len(request.Questions) == 0 {
		return nil, false, nil, "这个问题请求缺少有效的问题定义，当前无法提交。"
	}
	if request.DraftAnswers == nil {
		request.DraftAnswers = map[string]string{}
	}
	sawNewAnswer := false
	for _, question := range request.Questions {
		questionID := strings.TrimSpace(question.ID)
		if questionID == "" {
			continue
		}
		answerText := firstTrimmedAnswer(rawAnswers[questionID])
		if answerText == "" {
			continue
		}
		if canonical, ok := canonicalQuestionOptionAnswer(question, answerText); ok {
			answerText = canonical
		} else if len(question.Options) != 0 && !question.AllowOther {
			label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), questionID)
			return nil, false, nil, fmt.Sprintf("问题“%s”的答案不在可选项中。", label)
		}
		request.DraftAnswers[questionID] = answerText
		sawNewAnswer = true
	}
	if sawNewAnswer {
		clearRequestUserInputSubmitConfirmState(request)
	}
	answers := map[string]any{}
	missingLabels := make([]string, 0, len(request.Questions))
	for _, question := range request.Questions {
		questionID := strings.TrimSpace(question.ID)
		if questionID == "" {
			continue
		}
		answerText := strings.TrimSpace(request.DraftAnswers[questionID])
		if answerText == "" {
			missingLabels = append(missingLabels, firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), questionID))
			if allowSubmitWithUnanswered {
				answers[questionID] = map[string]any{"answers": []string{}}
			}
			continue
		}
		if canonical, ok := canonicalQuestionOptionAnswer(question, answerText); ok {
			answerText = canonical
		} else if len(question.Options) != 0 && !question.AllowOther {
			label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), questionID)
			return nil, false, nil, fmt.Sprintf("问题“%s”的答案不在可选项中。", label)
		}
		answers[questionID] = map[string]any{"answers": []string{answerText}}
	}
	if len(answers) == 0 {
		return nil, false, missingLabels, "当前没有可提交的答案。"
	}
	if len(missingLabels) != 0 && !allowSubmitWithUnanswered {
		return nil, false, missingLabels, ""
	}
	return map[string]any{"answers": answers}, true, nil, ""
}

func requestUserInputAllowSubmitWithUnanswered(optionID string) bool {
	normalized := requestUserInputNormalizedOptionID(optionID)
	switch normalized {
	case "submitwithunanswered", "allowunanswered", "submitpartial", "proceedwithunanswered":
		return true
	case requestUserInputNormalizedOptionID(requestUserInputConfirmSubmitWithUnansweredOptionID):
		return true
	default:
		return false
	}
}

func requestUserInputConfirmSubmitWithUnanswered(optionID string) bool {
	return requestUserInputNormalizedOptionID(optionID) == requestUserInputNormalizedOptionID(requestUserInputConfirmSubmitWithUnansweredOptionID)
}

func requestUserInputCancelSubmitWithUnanswered(optionID string) bool {
	return requestUserInputNormalizedOptionID(optionID) == requestUserInputNormalizedOptionID(requestUserInputCancelSubmitWithUnansweredOptionID)
}

func requestUserInputSubmitIntent(optionID string) bool {
	normalized := requestUserInputNormalizedOptionID(optionID)
	switch normalized {
	case requestUserInputNormalizedOptionID(requestUserInputSubmitOptionID),
		requestUserInputNormalizedOptionID("submit_answers"),
		requestUserInputNormalizedOptionID(requestUserInputSubmitWithUnansweredOptionID),
		requestUserInputNormalizedOptionID(requestUserInputConfirmSubmitWithUnansweredOptionID):
		return true
	default:
		return false
	}
}

func requestUserInputNormalizedOptionID(optionID string) string {
	normalized := strings.ToLower(strings.TrimSpace(optionID))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

func setRequestUserInputSubmitConfirmState(request *state.RequestPromptRecord, missingLabels []string) {
	if request == nil {
		return
	}
	request.SubmitWithUnansweredConfirmPending = true
	request.SubmitWithUnansweredMissingLabels = append([]string(nil), missingLabels...)
}

func clearRequestUserInputSubmitConfirmState(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.SubmitWithUnansweredConfirmPending = false
	request.SubmitWithUnansweredMissingLabels = nil
}

func bumpRequestCardRevision(request *state.RequestPromptRecord) {
	if request == nil {
		return
	}
	request.CardRevision++
	if request.CardRevision <= 0 {
		request.CardRevision = 1
	}
}

func (s *Service) nextRequestDispatchCommandID() string {
	s.nextRequestCommandID++
	return "reqcmd-" + strconv.Itoa(s.nextRequestCommandID)
}

func (s *Service) requestPromptEvent(surface *state.SurfaceConsoleRecord, record *state.RequestPromptRecord, threadTitleHint string) control.UIEvent {
	threadTitle := strings.TrimSpace(threadTitleHint)
	if threadTitle == "" {
		inst := s.root.Instances[record.InstanceID]
		var thread *state.ThreadRecord
		if inst != nil {
			thread = inst.Threads[record.ThreadID]
		}
		threadTitle = displayThreadTitle(inst, thread, record.ThreadID)
	}
	return s.feishuDirectRequestPromptEvent(surface, control.FeishuDirectRequestPrompt{
		RequestID:                          record.RequestID,
		RequestType:                        record.RequestType,
		RequestRevision:                    record.CardRevision,
		Title:                              record.Title,
		Body:                               record.Body,
		ThreadID:                           record.ThreadID,
		ThreadTitle:                        threadTitle,
		Options:                            requestPromptOptionsToControl(record.Options),
		Questions:                          requestPromptQuestionsToControl(record.Questions, record.DraftAnswers),
		SubmitWithUnansweredConfirmPending: record.SubmitWithUnansweredConfirmPending,
		SubmitWithUnansweredMissingLabels:  append([]string(nil), record.SubmitWithUnansweredMissingLabels...),
	})
}

func firstTrimmedAnswer(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func canonicalQuestionOptionAnswer(question state.RequestPromptQuestionRecord, answer string) (string, bool) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return "", false
	}
	for _, option := range question.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			continue
		}
		if strings.EqualFold(label, trimmed) {
			return label, true
		}
	}
	return "", false
}
