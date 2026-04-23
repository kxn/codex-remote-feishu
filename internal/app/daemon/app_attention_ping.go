package daemon

import (
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const attentionRequestDedupTTL = 24 * time.Hour

type attentionTurnBatchCandidate struct {
	anchorEvent     eventcontract.Event
	anchorIndex     int
	hasFailure      bool
	hasFinal        bool
	hasPlanProposal bool
}

func (a *App) planTurnAttentionPingsLocked(events []eventcontract.Event) map[int][]eventcontract.Event {
	if len(events) == 0 {
		return nil
	}
	turns := map[string]*attentionTurnBatchCandidate{}
	for index, event := range events {
		a.recordTurnAttentionCandidateLocked(turns, index, event)
	}
	followups := map[int][]eventcontract.Event{}
	for _, candidate := range turns {
		if ping := a.turnAttentionPingLocked(candidate); ping != nil {
			followups[candidate.anchorIndex] = append(followups[candidate.anchorIndex], *ping)
		}
	}
	return followups
}

func (a *App) requestAttentionPingCandidateLocked(event eventcontract.Event, now time.Time) (*eventcontract.Event, string) {
	if event.Kind != eventcontract.EventFeishuRequestView || event.RequestView == nil || event.InlineReplaceCurrentCard {
		return nil, ""
	}
	request := event.RequestView
	text, ok := attentionRequestPingText(request.RequestType)
	if !ok {
		return nil, ""
	}
	surfaceID := strings.TrimSpace(event.SurfaceSessionID)
	requestID := strings.TrimSpace(request.RequestID)
	if surfaceID == "" || requestID == "" {
		return nil, ""
	}
	mentionUserID, ok := a.attentionPingMentionTarget(surfaceID)
	if !ok {
		return nil, ""
	}
	key := surfaceID + "::" + requestID + "::" + strconv.Itoa(request.RequestRevision)
	if last := a.feishuRuntime.attentionRequests[key]; !last.IsZero() && now.Sub(last) < attentionRequestDedupTTL {
		return nil, ""
	}
	return a.newAttentionPingEvent(surfaceID, mentionUserID, a.attentionPingReplyTarget(event), text), key
}

func (a *App) globalRuntimeAttentionPingForEventLocked(event eventcontract.Event, now time.Time, honorSuppression bool) *eventcontract.Event {
	normalized, ok := normalizeGlobalRuntimeNoticeEvent(event)
	if !ok || normalized.Notice == nil {
		return nil
	}
	text, ok := attentionGlobalRuntimePingText(normalized.Notice.DeliveryFamily)
	if !ok {
		return nil
	}
	if honorSuppression && a.shouldSuppressGlobalRuntimeNoticeLocked(normalized, now) {
		return nil
	}
	mentionUserID, ok := a.attentionPingMentionTarget(normalized.SurfaceSessionID)
	if !ok {
		return nil
	}
	return a.newAttentionPingEvent(normalized.SurfaceSessionID, mentionUserID, a.attentionPingReplyTarget(normalized), text)
}

func (a *App) recordRequestAttentionPingLocked(key string, now time.Time) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	a.feishuRuntime.attentionRequests[key] = now
	a.pruneAttentionRequestsLocked(now.Add(-attentionRequestDedupTTL))
}

func (a *App) recordTurnAttentionCandidateLocked(candidates map[string]*attentionTurnBatchCandidate, index int, event eventcontract.Event) {
	surfaceID := strings.TrimSpace(event.SurfaceSessionID)
	if surfaceID == "" {
		return
	}
	candidate := candidates[surfaceID]
	if candidate == nil {
		candidate = &attentionTurnBatchCandidate{}
		candidates[surfaceID] = candidate
	}
	switch {
	case isTurnFailureAttentionEvent(event):
		candidate.hasFailure = true
		candidate.anchorEvent = event
		candidate.anchorIndex = index
	case isPlanProposalAttentionEvent(event):
		candidate.hasPlanProposal = true
		if !candidate.hasFailure {
			candidate.anchorEvent = event
			candidate.anchorIndex = index
		}
	case isFinalResultAttentionEvent(event):
		candidate.hasFinal = true
		if !candidate.hasFailure && !candidate.hasPlanProposal {
			candidate.anchorEvent = event
			candidate.anchorIndex = index
		}
	}
}

func (a *App) turnAttentionPingLocked(candidate *attentionTurnBatchCandidate) *eventcontract.Event {
	if candidate == nil {
		return nil
	}
	surfaceID := strings.TrimSpace(candidate.anchorEvent.SurfaceSessionID)
	if surfaceID == "" {
		return nil
	}
	mentionUserID, ok := a.attentionPingMentionTarget(surfaceID)
	if !ok {
		return nil
	}
	var text string
	switch {
	case candidate.hasFailure:
		text = "需要你回来处理：本轮执行已停止。"
	case candidate.hasPlanProposal:
		text = "需要你回来处理：本轮执行已结束，并生成了提案计划。"
	case candidate.hasFinal:
		text = "需要你回来处理：本轮执行已结束。"
	default:
		return nil
	}
	return a.newAttentionPingEvent(surfaceID, mentionUserID, a.attentionPingReplyTarget(candidate.anchorEvent), text)
}

func (a *App) attentionPingMentionTarget(surfaceID string) (string, bool) {
	mentionUserID := strings.TrimSpace(a.service.SurfaceActorUserID(surfaceID))
	return mentionUserID, mentionUserID != ""
}

func (a *App) attentionPingReplyTarget(event eventcontract.Event) string {
	chatID := strings.TrimSpace(a.service.SurfaceChatID(event.SurfaceSessionID))
	if chatID == "" {
		return ""
	}
	for _, operation := range a.projector.ProjectEvent(chatID, event.Normalized()) {
		switch operation.Kind {
		case feishu.OperationSendText, feishu.OperationSendCard, feishu.OperationSendImage:
			return strings.TrimSpace(operation.ReplyToMessageID)
		}
	}
	return ""
}

func (a *App) newAttentionPingEvent(surfaceID, mentionUserID, replyToMessageID, text string) *eventcontract.Event {
	text = strings.TrimSpace(text)
	mentionUserID = strings.TrimSpace(mentionUserID)
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" || mentionUserID == "" || text == "" {
		return nil
	}
	replyToMessageID = strings.TrimSpace(replyToMessageID)
	return &eventcontract.Event{
		Kind:             eventcontract.EventTimelineText,
		SurfaceSessionID: surfaceID,
		SourceMessageID:  replyToMessageID,
		TimelineText: &control.TimelineText{
			Type:             control.TimelineTextAttentionPing,
			Text:             text,
			MentionUserID:    mentionUserID,
			ReplyToMessageID: replyToMessageID,
		},
	}
}

func (a *App) pruneAttentionRequestsLocked(cutoff time.Time) {
	for key, seenAt := range a.feishuRuntime.attentionRequests {
		if seenAt.Before(cutoff) {
			delete(a.feishuRuntime.attentionRequests, key)
		}
	}
}

func attentionRequestPingText(requestType string) (string, bool) {
	switch strings.TrimSpace(requestType) {
	case "approval":
		return "需要你回来处理：请确认这条请求。", true
	case "request_user_input":
		return "需要你回来处理：请补充输入。", true
	case "permissions_request_approval":
		return "需要你回来处理：请授予权限。", true
	case "mcp_server_elicitation":
		return "需要你回来处理：请处理 MCP 请求。", true
	default:
		return "", false
	}
}

func attentionGlobalRuntimePingText(family control.NoticeDeliveryFamily) (string, bool) {
	switch family {
	case control.NoticeDeliveryFamilyTransportDegraded:
		return "需要你回来处理：当前连接状态异常。", true
	case control.NoticeDeliveryFamilyGatewayApplyFailure:
		return "需要你回来处理：飞书投递失败。", true
	case control.NoticeDeliveryFamilyDaemonShutdown:
		return "需要你回来处理：当前服务即将停止。", true
	default:
		return "", false
	}
}

func isFinalResultAttentionEvent(event eventcontract.Event) bool {
	return event.Kind == eventcontract.EventBlockCommitted && event.Block != nil && event.Block.Final
}

func isTurnFailureAttentionEvent(event eventcontract.Event) bool {
	return event.Kind == eventcontract.EventNotice && event.Notice != nil && strings.TrimSpace(event.Notice.Code) == "turn_failed"
}

func isPlanProposalAttentionEvent(event eventcontract.Event) bool {
	return event.Kind == eventcontract.EventFeishuPageView &&
		event.PageView != nil &&
		strings.TrimSpace(event.PageView.CommandID) == control.FeishuCommandPlan &&
		strings.TrimSpace(event.PageView.Title) == "提案计划"
}
