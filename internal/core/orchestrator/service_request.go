package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"strings"
	"time"
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
	requestType := normalizeRequestType(firstNonEmpty(action.RequestType, request.RequestType))
	if requestType == "" {
		requestType = "approval"
	}
	response, clearOnDispatch, errNotice := s.buildRequestResponse(surface, request, action, requestType)
	if errNotice != nil {
		return errNotice
	}
	if clearOnDispatch {
		delete(surface.PendingRequests, request.RequestID)
		clearSurfaceRequestCaptureByRequestID(surface, request.RequestID)
	}
	return []control.UIEvent{{
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
		RequestID:   event.RequestID,
		RequestType: requestType,
		InstanceID:  instanceID,
		ThreadID:    event.ThreadID,
		TurnID:      event.TurnID,
		ItemID:      strings.TrimSpace(metadataString(event.Metadata, "itemId")),
		Title:       title,
		Body:        body,
		Options:     options,
		Questions:   questions,
		CreatedAt:   s.now(),
	}
	surface.PendingRequests[event.RequestID] = record
	return []control.UIEvent{s.feishuDirectRequestPromptEvent(surface, control.FeishuDirectRequestPrompt{
		RequestID:   record.RequestID,
		RequestType: record.RequestType,
		Title:       record.Title,
		Body:        record.Body,
		ThreadID:    record.ThreadID,
		ThreadTitle: threadTitle,
		Options:     requestPromptOptionsToControl(record.Options),
		Questions:   requestPromptQuestionsToControl(record.Questions),
	})}
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
		response, errText := buildRequestUserInputResponse(request, action.RequestAnswers)
		if errText != "" {
			return nil, false, notice(surface, "request_invalid", errText)
		}
		return response, true, nil
	default:
		return nil, false, notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
}

func buildRequestUserInputResponse(request *state.RequestPromptRecord, rawAnswers map[string][]string) (map[string]any, string) {
	if request == nil || len(request.Questions) == 0 {
		return nil, "这个问题请求缺少有效的问题定义，当前无法提交。"
	}
	answers := map[string]any{}
	for _, question := range request.Questions {
		questionID := strings.TrimSpace(question.ID)
		if questionID == "" {
			continue
		}
		answerText := firstTrimmedAnswer(rawAnswers[questionID])
		if answerText == "" {
			label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), questionID)
			return nil, fmt.Sprintf("问题“%s”还没有填写答案。", label)
		}
		if canonical, ok := canonicalQuestionOptionAnswer(question, answerText); ok {
			answerText = canonical
		} else if len(question.Options) != 0 && !question.AllowOther {
			label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), questionID)
			return nil, fmt.Sprintf("问题“%s”的答案不在可选项中。", label)
		}
		answers[questionID] = map[string]any{
			"answers": []string{answerText},
		}
	}
	if len(answers) == 0 {
		return nil, "当前没有可提交的答案。"
	}
	return map[string]any{"answers": answers}, ""
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
