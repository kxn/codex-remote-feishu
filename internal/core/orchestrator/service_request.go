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
	if requestType != "approval" {
		return notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
	optionID := normalizeRequestOptionID(firstNonEmpty(action.RequestOptionID, requestOptionIDFromApproved(action.Approved)))
	if optionID == "" {
		return notice(surface, "request_invalid", "这个确认按钮缺少有效的处理选项。")
	}
	if !requestHasOption(request, optionID) {
		return notice(surface, "request_invalid", "这个确认按钮对应的选项无效或当前不可用。")
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
		return notice(surface, "request_capture_started", "已进入反馈模式。接下来一条普通文本会作为对当前确认请求的处理意见，不会进入普通消息队列。")
	}
	decision := decisionForRequestOption(optionID)
	if decision == "" {
		return notice(surface, "request_invalid", "这个确认按钮对应的决策暂不支持。")
	}
	clearSurfaceRequestCaptureByRequestID(surface, request.RequestID)
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
				Response: map[string]any{
					"type":     requestType,
					"decision": decision,
				},
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
	if requestType != "approval" {
		return notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
	inst := s.root.Instances[instanceID]
	var thread *state.ThreadRecord
	if inst != nil {
		thread = inst.Threads[event.ThreadID]
	}
	threadTitle := displayThreadTitle(inst, thread, event.ThreadID)
	title := firstNonEmpty(metadataString(event.Metadata, "title"), "需要确认")
	body := strings.TrimSpace(metadataString(event.Metadata, "body"))
	if body == "" {
		body = "本地 Codex 正在等待你的确认。"
	}
	options := buildApprovalRequestOptions(event.Metadata)
	record := &state.RequestPromptRecord{
		RequestID:   event.RequestID,
		RequestType: requestType,
		InstanceID:  instanceID,
		ThreadID:    event.ThreadID,
		TurnID:      event.TurnID,
		Title:       title,
		Body:        body,
		Options:     options,
		CreatedAt:   s.now(),
	}
	surface.PendingRequests[event.RequestID] = record
	return []control.UIEvent{{
		Kind:             control.UIEventRequestPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		RequestPrompt: &control.RequestPrompt{
			RequestID:   record.RequestID,
			RequestType: record.RequestType,
			Title:       record.Title,
			Body:        record.Body,
			ThreadID:    record.ThreadID,
			ThreadTitle: threadTitle,
			Options:     requestPromptOptionsToControl(record.Options),
		},
	}}
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
