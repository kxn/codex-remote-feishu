package daemon

import (
	"strconv"
	"strings"
	"time"

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

func (a *App) planTurnAttentionAnnotationsLocked(events []eventcontract.Event) map[int]eventcontract.AttentionAnnotation {
	if len(events) == 0 {
		return nil
	}
	turns := map[string]*attentionTurnBatchCandidate{}
	for index, event := range events {
		a.recordTurnAttentionCandidateLocked(turns, index, event)
	}
	annotations := map[int]eventcontract.AttentionAnnotation{}
	for _, candidate := range turns {
		if attention := a.turnAttentionAnnotationLocked(candidate); !attention.Empty() {
			annotations[candidate.anchorIndex] = attention
		}
	}
	return annotations
}

func (a *App) requestAttentionAnnotationCandidateLocked(event eventcontract.Event, now time.Time) (eventcontract.AttentionAnnotation, string) {
	if event.CanonicalKind() != eventcontract.KindRequest || event.InlineReplaceCurrentCard {
		return eventcontract.AttentionAnnotation{}, ""
	}
	requestPayload, ok := requestPayloadFromEvent(event)
	if !ok {
		return eventcontract.AttentionAnnotation{}, ""
	}
	request := requestPayload.View
	text, ok := attentionRequestPingText(request.SemanticKind, request.RequestType)
	if !ok {
		return eventcontract.AttentionAnnotation{}, ""
	}
	surfaceID := strings.TrimSpace(event.SurfaceSessionID)
	requestID := strings.TrimSpace(request.RequestID)
	if surfaceID == "" || requestID == "" {
		return eventcontract.AttentionAnnotation{}, ""
	}
	mentionUserID, ok := a.attentionPingMentionTarget(surfaceID)
	if !ok {
		return eventcontract.AttentionAnnotation{}, ""
	}
	key := surfaceID + "::" + requestID + "::" + strconv.Itoa(request.RequestRevision)
	if last := a.feishuRuntime.attentionRequests[key]; !last.IsZero() && now.Sub(last) < attentionRequestDedupTTL {
		return eventcontract.AttentionAnnotation{}, ""
	}
	return newAttentionAnnotation(text, mentionUserID), key
}

func (a *App) globalRuntimeAttentionAnnotationForEventLocked(event eventcontract.Event, now time.Time, honorSuppression bool) eventcontract.AttentionAnnotation {
	normalized, ok := normalizeGlobalRuntimeNoticeEvent(event)
	if !ok || normalized.Notice == nil {
		return eventcontract.AttentionAnnotation{}
	}
	text, ok := attentionGlobalRuntimePingText(normalized.Notice.DeliveryFamily)
	if !ok {
		return eventcontract.AttentionAnnotation{}
	}
	if honorSuppression && a.shouldSuppressGlobalRuntimeNoticeLocked(normalized, now) {
		return eventcontract.AttentionAnnotation{}
	}
	mentionUserID, ok := a.attentionPingMentionTarget(normalized.SurfaceSessionID)
	if !ok {
		return eventcontract.AttentionAnnotation{}
	}
	return newAttentionAnnotation(text, mentionUserID)
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

func (a *App) turnAttentionAnnotationLocked(candidate *attentionTurnBatchCandidate) eventcontract.AttentionAnnotation {
	if candidate == nil {
		return eventcontract.AttentionAnnotation{}
	}
	surfaceID := strings.TrimSpace(candidate.anchorEvent.SurfaceSessionID)
	if surfaceID == "" {
		return eventcontract.AttentionAnnotation{}
	}
	mentionUserID, ok := a.attentionPingMentionTarget(surfaceID)
	if !ok {
		return eventcontract.AttentionAnnotation{}
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
		return eventcontract.AttentionAnnotation{}
	}
	return newAttentionAnnotation(text, mentionUserID)
}

func (a *App) attentionPingMentionTarget(surfaceID string) (string, bool) {
	mentionUserID := strings.TrimSpace(a.service.SurfaceActorUserID(surfaceID))
	return mentionUserID, mentionUserID != ""
}

func newAttentionAnnotation(text, mentionUserID string) eventcontract.AttentionAnnotation {
	return eventcontract.AttentionAnnotation{
		Text:          text,
		MentionUserID: mentionUserID,
	}.Normalized()
}

func (a *App) pruneAttentionRequestsLocked(cutoff time.Time) {
	for key, seenAt := range a.feishuRuntime.attentionRequests {
		if seenAt.Before(cutoff) {
			delete(a.feishuRuntime.attentionRequests, key)
		}
	}
}

func attentionRequestPingText(semanticKind, requestType string) (string, bool) {
	switch control.NormalizeRequestSemanticKind(strings.TrimSpace(semanticKind), strings.TrimSpace(requestType)) {
	case control.RequestSemanticApprovalCommand:
		return "需要你回来处理：请确认是否执行命令。", true
	case control.RequestSemanticApprovalFileChange:
		return "需要你回来处理：请确认是否修改文件。", true
	case control.RequestSemanticApprovalNetwork:
		return "需要你回来处理：请确认是否允许网络访问。", true
	case control.RequestSemanticApproval:
		return "需要你回来处理：请确认这条请求。", true
	case control.RequestSemanticRequestUserInput:
		return "需要你回来处理：请补充输入。", true
	case control.RequestSemanticPermissionsRequestApproval:
		return "需要你回来处理：请授予权限。", true
	case control.RequestSemanticMCPServerElicitation, control.RequestSemanticMCPServerElicitationForm, control.RequestSemanticMCPServerElicitationURL:
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
	payload, ok := blockPayloadFromEvent(event)
	return event.CanonicalKind() == eventcontract.KindBlockCommitted && ok && payload.Block.Final
}

func isTurnFailureAttentionEvent(event eventcontract.Event) bool {
	payload, ok := noticePayloadFromEvent(event)
	return event.CanonicalKind() == eventcontract.KindNotice && ok && strings.TrimSpace(payload.Notice.Code) == "turn_failed"
}

func isPlanProposalAttentionEvent(event eventcontract.Event) bool {
	payload, ok := pagePayloadFromEvent(event)
	return event.CanonicalKind() == eventcontract.KindPage &&
		ok &&
		strings.TrimSpace(payload.View.CommandID) == control.FeishuCommandPlan &&
		strings.TrimSpace(payload.View.Title) == "提案计划"
}
