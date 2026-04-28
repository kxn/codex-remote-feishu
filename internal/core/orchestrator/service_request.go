package orchestrator

import (
	"fmt"

	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) respondRequest(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	requestAction := requestActionFromAction(action)
	if surface == nil || requestAction == nil || strings.TrimSpace(requestAction.RequestID) == "" {
		return nil
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	request := surface.PendingRequests[requestAction.RequestID]
	if request == nil {
		return notice(surface, "request_expired", "这个确认请求已经结束或过期了。")
	}
	if request.PendingDispatchCommandID != "" {
		return notice(surface, "request_pending_dispatch", "这条请求已经提交，正在等待本地 Codex 处理。")
	}
	if requestAction.RequestRevision != 0 && requestAction.RequestRevision != request.CardRevision {
		return notice(surface, "request_card_expired", "这张请求卡片已经过期，请使用最新卡片继续操作。")
	}
	if strings.TrimSpace(request.LocalKind) != "" {
		return s.respondLocalRequest(surface, request, action)
	}
	requestType := normalizeRequestType(firstNonEmpty(requestAction.RequestType, request.RequestType))
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
	return s.dispatchRequestResponse(surface, request, action, response, "")
}

func (s *Service) controlRequest(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	requestControl := requestControlFromAction(action)
	if surface == nil || requestControl == nil || strings.TrimSpace(requestControl.RequestID) == "" {
		return nil
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	request := surface.PendingRequests[requestControl.RequestID]
	if request == nil {
		return notice(surface, "request_expired", "这个确认请求已经结束或过期了。")
	}
	if request.PendingDispatchCommandID != "" {
		return notice(surface, "request_pending_dispatch", "这条请求已经提交，正在等待本地 Codex 处理。")
	}
	if requestControl.RequestRevision != 0 && requestControl.RequestRevision != request.CardRevision {
		return notice(surface, "request_card_expired", "这张请求卡片已经过期，请使用最新卡片继续操作。")
	}
	switch normalizedRequestControl(requestControl.Control) {
	case normalizedRequestControl(frontstagecontract.RequestControlSkipOptional):
		return s.skipOptionalRequestQuestion(surface, request, action, requestControl)
	case normalizedRequestControl(frontstagecontract.RequestControlCancelTurn):
		if normalizeRequestType(firstNonEmpty(requestControl.RequestType, request.RequestType)) != "request_user_input" {
			return notice(surface, "request_invalid", "当前请求不支持中断 turn。")
		}
		return s.cancelRequestUserInputTurn(surface, request, action)
	case normalizedRequestControl(frontstagecontract.RequestControlCancelRequest):
		requestType := normalizeRequestType(firstNonEmpty(requestControl.RequestType, request.RequestType))
		if requestType != "mcp_server_elicitation" || len(request.Questions) == 0 {
			return notice(surface, "request_invalid", "当前请求不支持直接取消。")
		}
		return s.dispatchRequestResponse(
			surface,
			request,
			action,
			buildMCPElicitationPayload("cancel", nil, promptMCPElicitationMeta(request.Prompt, nil)),
			"已提交取消请求，等待 Codex 继续。",
		)
	default:
		return notice(surface, "request_invalid", "这个请求控制动作当前不支持。")
	}
}

func (s *Service) presentRequestPrompt(instanceID string, event agentproto.Event) []eventcontract.Event {
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
	promptType := ""
	if event.RequestPrompt != nil {
		promptType = string(event.RequestPrompt.Type)
	}
	definition, unsupportedText := buildRequestPromptPresentationDefinition(event.RequestPrompt, event.Metadata)
	requestType := normalizeRequestType(firstNonEmpty(definition.RequestType, promptType, metadataString(event.Metadata, "requestType")))
	if requestType == "" {
		requestType = "approval"
	}
	inst := s.root.Instances[instanceID]
	var thread *state.ThreadRecord
	if inst != nil {
		thread = inst.Threads[event.ThreadID]
	}
	threadTitle := displayThreadTitle(inst, thread, event.ThreadID)
	if unsupportedText != "" {
		return notice(surface, "request_unsupported", unsupportedText)
	}
	record := &state.RequestPromptRecord{
		RequestID:    event.RequestID,
		RequestType:  requestType,
		SemanticKind: definition.SemanticKind,
		Prompt:       event.RequestPrompt,
		InstanceID:   instanceID,
		ThreadID:     event.ThreadID,
		TurnID:       event.TurnID,
		SourceMessageID: func() string {
			sourceMessageID, _ := s.replyAnchorForTurn(instanceID, event.ThreadID, event.TurnID)
			return sourceMessageID
		}(),
		ItemID:               strings.TrimSpace(metadataString(event.Metadata, "itemId")),
		Title:                definition.Title,
		Sections:             definition.Sections,
		Options:              definition.Options,
		Questions:            definition.Questions,
		CurrentQuestionIndex: 0,
		HintText:             definition.HintText,
		DraftAnswers:         map[string]string{},
		SkippedQuestionIDs:   map[string]bool{},
		CardRevision:         1,
		Phase:                frontstagecontract.PhaseEditing,
		CreatedAt:            s.now(),
	}
	surface.PendingRequests[event.RequestID] = record
	if !requestPromptRenderable(requestType) {
		return notice(surface, "request_unsupported", fmt.Sprintf("收到 %s 请求，当前飞书端还不能直接处理，已保持为待处理状态。", requestType))
	}
	if requestType == "tool_callback" {
		return s.autoDispatchUnsupportedToolCallback(surface, record, threadTitle)
	}
	return []eventcontract.Event{s.requestPromptEvent(surface, record, threadTitle)}
}

func (s *Service) resolveRequestPrompt(instanceID string, event agentproto.Event) []eventcontract.Event {
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

func (s *Service) consumeCapturedRequestFeedback(surface *state.SurfaceConsoleRecord, action control.Action, text string) []eventcontract.Event {
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
	events := []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
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

func (s *Service) buildRequestResponse(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action, requestType string) (map[string]any, bool, []eventcontract.Event) {
	requestAction := requestActionFromAction(action)
	if requestAction == nil {
		return nil, false, notice(surface, "request_invalid", "这个请求动作缺少有效的请求上下文。")
	}
	requestAnswers := requestAction.Answers
	switch requestType {
	case "approval":
		optionID := control.NormalizeRequestOptionID(requestAction.RequestOptionID)
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
		if requestPromptStepPrevious(requestAction.RequestOptionID) {
			moveRequestPromptCurrentQuestion(request, -1)
			bumpRequestCardRevision(request)
			return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}
		}
		if requestPromptStepNext(requestAction.RequestOptionID) {
			moveRequestPromptCurrentQuestion(request, 1)
			bumpRequestCardRevision(request)
			return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}
		}
		response, complete, errText := buildRequestUserInputResponse(request, requestAnswers)
		if errText != "" {
			return nil, false, notice(surface, "request_invalid", errText)
		}
		if !complete {
			if len(requestAnswers) == 0 {
				return nil, false, notice(surface, "request_invalid", requestCurrentQuestionPendingText(request))
			}
			bumpRequestCardRevision(request)
			setRequestPromptCurrentQuestionIndex(request, firstIncompleteRequestQuestionIndex(request))
			return nil, false, []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}
		}
		return response, true, nil
	case "permissions_request_approval":
		response, complete, followup := buildPermissionsRequestResponse(request, action)
		if followup != nil {
			return nil, false, followup
		}
		if !complete || response == nil {
			return nil, false, notice(surface, "request_invalid", "这个权限请求按钮无效或当前不支持。")
		}
		return response, true, nil
	case "mcp_server_elicitation":
		return s.buildMCPElicitationResponse(surface, request, action)
	default:
		return nil, false, notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
}

func requestActionFromAction(action control.Action) *control.ActionRequestResponse {
	return action.Request
}

func requestControlFromAction(action control.Action) *control.ActionRequestControl {
	if action.RequestControl != nil {
		return action.RequestControl
	}
	return nil
}

func buildRequestUserInputResponse(request *state.RequestPromptRecord, rawAnswers map[string][]string) (map[string]any, bool, string) {
	if request == nil || len(request.Questions) == 0 {
		return nil, false, "这个问题请求缺少有效的问题定义，当前无法提交。"
	}
	if request.DraftAnswers == nil {
		request.DraftAnswers = map[string]string{}
	}
	if request.SkippedQuestionIDs == nil {
		request.SkippedQuestionIDs = map[string]bool{}
	}
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
			return nil, false, fmt.Sprintf("问题“%s”的答案不在可选项中。", label)
		}
		request.DraftAnswers[questionID] = answerText
		delete(request.SkippedQuestionIDs, questionID)
	}
	answers := map[string]any{}
	complete := true
	for _, question := range request.Questions {
		questionID := strings.TrimSpace(question.ID)
		if questionID == "" {
			continue
		}
		answerText := strings.TrimSpace(request.DraftAnswers[questionID])
		if answerText == "" {
			if question.Optional && requestQuestionSkipped(request, question) {
				continue
			}
			complete = false
			continue
		}
		if canonical, ok := canonicalQuestionOptionAnswer(question, answerText); ok {
			answerText = canonical
		} else if len(question.Options) != 0 && !question.AllowOther {
			label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), questionID)
			return nil, false, fmt.Sprintf("问题“%s”的答案不在可选项中。", label)
		}
		answers[questionID] = map[string]any{"answers": []string{answerText}}
	}
	if !complete {
		return nil, false, ""
	}
	return map[string]any{"answers": answers}, true, ""
}

func requestPromptStepPrevious(optionID string) bool {
	return normalizedRequestControl(optionID) == normalizedRequestControl(frontstagecontract.RequestPromptOptionStepPrevious)
}

func requestPromptStepNext(optionID string) bool {
	return normalizedRequestControl(optionID) == normalizedRequestControl(frontstagecontract.RequestPromptOptionStepNext)
}

func normalizedRequestControl(optionID string) string {
	return frontstagecontract.NormalizeRequestControlToken(optionID)
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

func requestPromptQuestionCount(request *state.RequestPromptRecord) int {
	if request == nil {
		return 0
	}
	return len(request.Questions)
}

func normalizedRequestPromptCurrentQuestionIndex(request *state.RequestPromptRecord) int {
	if request == nil || len(request.Questions) == 0 {
		return 0
	}
	if request.CurrentQuestionIndex < 0 {
		return 0
	}
	if request.CurrentQuestionIndex >= len(request.Questions) {
		return len(request.Questions) - 1
	}
	return request.CurrentQuestionIndex
}

func setRequestPromptCurrentQuestionIndex(request *state.RequestPromptRecord, index int) {
	if request == nil || len(request.Questions) == 0 {
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(request.Questions) {
		index = len(request.Questions) - 1
	}
	request.CurrentQuestionIndex = index
}

func moveRequestPromptCurrentQuestion(request *state.RequestPromptRecord, delta int) {
	setRequestPromptCurrentQuestionIndex(request, normalizedRequestPromptCurrentQuestionIndex(request)+delta)
}

func requestQuestionAnswered(request *state.RequestPromptRecord, question state.RequestPromptQuestionRecord) bool {
	if request == nil {
		return false
	}
	return strings.TrimSpace(request.DraftAnswers[strings.TrimSpace(question.ID)]) != ""
}

func requestQuestionSkipped(request *state.RequestPromptRecord, question state.RequestPromptQuestionRecord) bool {
	if request == nil || request.SkippedQuestionIDs == nil {
		return false
	}
	return request.SkippedQuestionIDs[strings.TrimSpace(question.ID)]
}

func requestQuestionCompleted(request *state.RequestPromptRecord, question state.RequestPromptQuestionRecord) bool {
	if requestQuestionAnswered(request, question) {
		return true
	}
	return question.Optional && requestQuestionSkipped(request, question)
}

func firstIncompleteRequestQuestionIndex(request *state.RequestPromptRecord) int {
	if request == nil {
		return 0
	}
	for index, question := range request.Questions {
		if !requestQuestionCompleted(request, question) {
			return index
		}
	}
	return normalizedRequestPromptCurrentQuestionIndex(request)
}

func requestPromptQuestionsComplete(request *state.RequestPromptRecord) bool {
	if request == nil || len(request.Questions) == 0 {
		return false
	}
	for _, question := range request.Questions {
		if !requestQuestionCompleted(request, question) {
			return false
		}
	}
	return true
}

func requestPromptPendingDispatchStatusText(request *state.RequestPromptRecord) string {
	if request == nil {
		return "已提交当前请求，等待 Codex 继续。"
	}
	switch requestPromptSemanticKind(request) {
	case control.RequestSemanticApprovalCommand, control.RequestSemanticApprovalFileChange, control.RequestSemanticApprovalNetwork, control.RequestSemanticApproval:
		return "已提交当前确认，等待 Codex 继续。"
	case control.RequestSemanticPermissionsRequestApproval:
		return "已提交授权决定，等待 Codex 继续。"
	case control.RequestSemanticMCPServerElicitationForm:
		return "已提交当前表单，等待 Codex 继续。"
	case control.RequestSemanticToolCallback:
		return "当前客户端不支持执行该工具回调，已自动上报 unsupported 结果，等待 Codex 继续。"
	case control.RequestSemanticMCPServerElicitationURL, control.RequestSemanticMCPServerElicitation:
		return "已提交当前请求，等待 Codex 继续。"
	default:
		return "已提交当前答案，等待 Codex 继续。"
	}
}

func requestCurrentQuestionPendingText(request *state.RequestPromptRecord) string {
	question, _, ok := requestPromptCurrentQuestionRecord(request)
	if !ok {
		return "当前没有可提交的答案。"
	}
	label := firstNonEmpty(strings.TrimSpace(question.Header), strings.TrimSpace(question.Question), strings.TrimSpace(question.ID))
	if question.Optional {
		return fmt.Sprintf("问题“%s”还没有处理。你可以先填写答案，或直接跳过。", label)
	}
	return fmt.Sprintf("问题“%s”还没有填写答案。", label)
}

func requestPromptCurrentQuestionRecord(request *state.RequestPromptRecord) (state.RequestPromptQuestionRecord, int, bool) {
	if request == nil || len(request.Questions) == 0 {
		return state.RequestPromptQuestionRecord{}, 0, false
	}
	index := normalizedRequestPromptCurrentQuestionIndex(request)
	return request.Questions[index], index, true
}

func markRequestQuestionSkipped(request *state.RequestPromptRecord, questionID string) {
	if request == nil {
		return
	}
	if request.SkippedQuestionIDs == nil {
		request.SkippedQuestionIDs = map[string]bool{}
	}
	questionID = strings.TrimSpace(questionID)
	if questionID == "" {
		return
	}
	request.SkippedQuestionIDs[questionID] = true
	delete(request.DraftAnswers, questionID)
}

func (s *Service) requestPromptView(record *state.RequestPromptRecord, threadTitleHint string) control.FeishuRequestView {
	threadTitle := strings.TrimSpace(threadTitleHint)
	if threadTitle == "" {
		inst := s.root.Instances[record.InstanceID]
		var thread *state.ThreadRecord
		if inst != nil {
			thread = inst.Threads[record.ThreadID]
		}
		threadTitle = displayThreadTitle(inst, thread, record.ThreadID)
	}
	view := control.FeishuRequestView{
		RequestID:            record.RequestID,
		RequestType:          record.RequestType,
		SemanticKind:         requestPromptSemanticKind(record),
		RequestRevision:      record.CardRevision,
		Title:                record.Title,
		DetourLabel:          s.requestDetourLabel(record),
		ThreadID:             record.ThreadID,
		ThreadTitle:          threadTitle,
		Sections:             requestPromptSectionsToControl(record.Sections),
		Options:              requestPromptOptionsToControl(record.Options),
		Questions:            requestPromptQuestionsToControl(record.Questions, record.DraftAnswers, record.SkippedQuestionIDs),
		CurrentQuestionIndex: normalizedRequestPromptCurrentQuestionIndex(record),
		HintText:             strings.TrimSpace(record.HintText),
		Phase:                record.Phase,
	}
	if strings.TrimSpace(record.PendingDispatchCommandID) != "" {
		view.Phase = frontstagecontract.PhaseWaitingDispatch
		if strings.TrimSpace(view.StatusText) == "" {
			view.StatusText = requestPromptPendingDispatchStatusText(record)
		}
	}
	return control.NormalizeFeishuRequestView(view)
}

func toolCallbackUnsupportedResultText(request *state.RequestPromptRecord) string {
	toolName := ""
	callID := ""
	if request != nil && request.Prompt != nil && request.Prompt.ToolCallback != nil {
		toolName = strings.TrimSpace(request.Prompt.ToolCallback.ToolName)
		callID = strings.TrimSpace(request.Prompt.ToolCallback.CallID)
	}
	switch {
	case toolName != "" && callID != "":
		return fmt.Sprintf("Dynamic tool callback is unsupported in this relay/headless client. Tool %q (call %q) was not executed.", toolName, callID)
	case toolName != "":
		return fmt.Sprintf("Dynamic tool callback is unsupported in this relay/headless client. Tool %q was not executed.", toolName)
	case callID != "":
		return fmt.Sprintf("Dynamic tool callback is unsupported in this relay/headless client. Call %q was not executed.", callID)
	default:
		return "Dynamic tool callback is unsupported in this relay/headless client and was not executed."
	}
}

func buildUnsupportedToolCallbackResponse(request *state.RequestPromptRecord) map[string]any {
	return map[string]any{
		"type": "structured",
		"result": map[string]any{
			"success": false,
			"contentItems": []map[string]any{{
				"type": "inputText",
				"text": toolCallbackUnsupportedResultText(request),
			}},
		},
	}
}

func (s *Service) autoDispatchUnsupportedToolCallback(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, threadTitleHint string) []eventcontract.Event {
	if surface == nil || request == nil {
		return nil
	}
	request.PendingDispatchCommandID = s.nextRequestDispatchCommandID()
	request.Phase = frontstagecontract.PhaseWaitingDispatch
	return []eventcontract.Event{
		s.requestPromptEvent(surface, request, threadTitleHint),
		{
			Kind:             eventcontract.KindAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				CommandID: request.PendingDispatchCommandID,
				Kind:      agentproto.CommandRequestRespond,
				Origin: agentproto.Origin{
					Surface: surface.SurfaceSessionID,
					UserID:  surface.ActorUserID,
					ChatID:  surface.ChatID,
				},
				Target: agentproto.Target{
					ThreadID:               request.ThreadID,
					TurnID:                 request.TurnID,
					UseActiveTurnIfOmitted: request.TurnID == "",
				},
				Request: agentproto.Request{
					RequestID: request.RequestID,
					Response:  buildUnsupportedToolCallbackResponse(request),
				},
			},
		},
	}
}

func (s *Service) requestPromptEvent(surface *state.SurfaceConsoleRecord, record *state.RequestPromptRecord, threadTitleHint string) eventcontract.Event {
	event := s.requestViewEvent(surface, s.requestPromptView(record, threadTitleHint))
	event.SourceMessageID = strings.TrimSpace(record.SourceMessageID)
	if event.SourceMessageID != "" {
		event.Meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return event
}

func (s *Service) requestPromptInlineEvent(surface *state.SurfaceConsoleRecord, record *state.RequestPromptRecord, threadTitleHint string) eventcontract.Event {
	event := s.requestPromptEvent(surface, record, threadTitleHint)
	event.InlineReplaceCurrentCard = true
	return event
}

func (s *Service) requestPromptRefreshWithNotice(surface *state.SurfaceConsoleRecord, record *state.RequestPromptRecord, code, text string) []eventcontract.Event {
	events := []eventcontract.Event{s.requestPromptEvent(surface, record, "")}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  strings.TrimSpace(record.SourceMessageID),
		Notice: &control.Notice{
			Code: code,
			Text: text,
		},
	})
	return events
}

func (s *Service) requestPromptInlinePhaseEvent(surface *state.SurfaceConsoleRecord, record *state.RequestPromptRecord, threadTitleHint string, phase frontstagecontract.Phase, statusText string) eventcontract.Event {
	view := s.requestPromptView(record, threadTitleHint)
	view.Phase = phase
	view.StatusText = strings.TrimSpace(statusText)
	event := s.requestViewEvent(surface, control.NormalizeFeishuRequestView(view))
	event.InlineReplaceCurrentCard = true
	event.SourceMessageID = strings.TrimSpace(record.SourceMessageID)
	return event
}

func (s *Service) dispatchRequestResponse(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action, response map[string]any, statusText string) []eventcontract.Event {
	if surface == nil || request == nil || response == nil {
		return nil
	}
	bridge := control.ResolveRequestBridgeContract(request.SemanticKind, request.RequestType)
	request.PendingDispatchCommandID = s.nextRequestDispatchCommandID()
	request.Phase = frontstagecontract.PhaseWaitingDispatch
	bumpRequestCardRevision(request)
	events := make([]eventcontract.Event, 0, 2)
	if requestPromptRenderable(request.RequestType) {
		events = append(events, s.requestPromptInlinePhaseEvent(surface, request, "", frontstagecontract.PhaseWaitingDispatch, firstNonEmpty(strings.TrimSpace(statusText), requestPromptPendingDispatchStatusText(request))))
	}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindAgentCommand,
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
				RequestID:          request.RequestID,
				Response:           response,
				BridgeKind:         string(bridge.Kind),
				SemanticKind:       control.NormalizeRequestSemanticKind(request.SemanticKind, request.RequestType),
				InterruptOnDecline: control.RequestBridgeShouldInterruptOnDecline(bridge, response),
			},
		},
	})
	return events
}

func (s *Service) skipOptionalRequestQuestion(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action, requestControl *control.ActionRequestControl) []eventcontract.Event {
	if request == nil || requestControl == nil {
		return nil
	}
	question, _, ok := requestPromptCurrentQuestionRecord(request)
	if !ok {
		return notice(surface, "request_invalid", "当前没有可跳过的问题。")
	}
	if !question.Optional {
		return notice(surface, "request_invalid", "当前问题不是可跳过题。")
	}
	if questionID := strings.TrimSpace(requestControl.QuestionID); questionID != "" && questionID != strings.TrimSpace(question.ID) {
		return notice(surface, "request_card_expired", "当前题目已变化，请使用最新卡片继续。")
	}
	markRequestQuestionSkipped(request, question.ID)
	requestType := normalizeRequestType(firstNonEmpty(requestControl.RequestType, request.RequestType))
	switch requestType {
	case "request_user_input":
		response, complete, errText := buildRequestUserInputResponse(request, nil)
		if errText != "" {
			return notice(surface, "request_invalid", errText)
		}
		if complete {
			return s.dispatchRequestResponse(surface, request, action, response, "")
		}
	case "mcp_server_elicitation":
		content, complete, _, errText := buildMCPElicitationContent(request, nil)
		if errText != "" {
			return notice(surface, "request_invalid", errText)
		}
		if complete {
			return s.dispatchRequestResponse(surface, request, action, buildMCPElicitationPayload("accept", content, promptMCPElicitationMeta(request.Prompt, nil)), "")
		}
	default:
		return notice(surface, "request_invalid", "当前请求不支持跳过可选题。")
	}
	bumpRequestCardRevision(request)
	setRequestPromptCurrentQuestionIndex(request, firstIncompleteRequestQuestionIndex(request))
	return []eventcontract.Event{s.requestPromptInlineEvent(surface, request, "")}
}

func (s *Service) cancelRequestUserInputTurn(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, action control.Action) []eventcontract.Event {
	if surface == nil || request == nil {
		return nil
	}
	request.Phase = frontstagecontract.PhaseCancelled
	events := []eventcontract.Event{s.requestPromptInlinePhaseEvent(surface, request, "", frontstagecontract.PhaseCancelled, "已放弃答题，并向当前 turn 发送停止请求。")}
	if strings.TrimSpace(request.RequestID) != "" && surface.PendingRequests != nil {
		delete(surface.PendingRequests, request.RequestID)
	}
	clearSurfaceRequestCaptureByRequestID(surface, request.RequestID)
	if request.ThreadID == "" && request.TurnID == "" {
		events[0] = s.requestPromptInlinePhaseEvent(surface, request, "", frontstagecontract.PhaseCancelled, "已放弃答题。当前 turn 已不在可中断状态。")
		return events
	}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandTurnInterrupt,
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
		},
	})
	return events
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
