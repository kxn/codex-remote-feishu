package orchestrator

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	requestVisibilityPendingVisibility = "pending_visibility"
	requestVisibilityVisible           = "visible"
	requestVisibilityDeliveryDegraded  = "delivery_degraded"
	requestVisibilityResolved          = "resolved"
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

func normalizeRequestVisibilityState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case requestVisibilityVisible:
		return requestVisibilityVisible
	case requestVisibilityDeliveryDegraded:
		return requestVisibilityDeliveryDegraded
	case requestVisibilityResolved:
		return requestVisibilityResolved
	default:
		return requestVisibilityPendingVisibility
	}
}

func normalizeRequestPromptRecord(record *state.RequestPromptRecord) {
	if record == nil {
		return
	}
	record.RequestID = strings.TrimSpace(record.RequestID)
	record.RequestType = normalizeRequestType(record.RequestType)
	record.SemanticKind = control.NormalizeRequestSemanticKind(record.SemanticKind, record.RequestType)
	record.InstanceID = strings.TrimSpace(record.InstanceID)
	record.ThreadID = strings.TrimSpace(record.ThreadID)
	record.TurnID = strings.TrimSpace(record.TurnID)
	record.OwnerSurfaceSessionID = strings.TrimSpace(record.OwnerSurfaceSessionID)
	record.OwnerGatewayID = strings.TrimSpace(record.OwnerGatewayID)
	record.OwnerChatID = strings.TrimSpace(record.OwnerChatID)
	record.LifecycleState = normalizeRequestLifecycleState(record.LifecycleState)
	if record.LifecycleState == "" {
		record.LifecycleState = inferRequestLifecycleState(record)
	}
	record.VisibilityState = normalizeRequestVisibilityState(record.VisibilityState)
	record.VisibleMessageID = strings.TrimSpace(record.VisibleMessageID)
	record.LastDeliveryError = strings.TrimSpace(record.LastDeliveryError)
	record.SourceContextLabel = strings.TrimSpace(record.SourceContextLabel)
	record.SourceMessageID = strings.TrimSpace(record.SourceMessageID)
	record.ItemID = strings.TrimSpace(record.ItemID)
	record.Title = strings.TrimSpace(record.Title)
	record.HintText = strings.TrimSpace(record.HintText)
	if record.DraftAnswers == nil {
		record.DraftAnswers = map[string]string{}
	}
	if record.SkippedQuestionIDs == nil {
		record.SkippedQuestionIDs = map[string]bool{}
	}
}

func joinRequestSourceContextLabels(labels ...string) string {
	seen := make(map[string]bool, len(labels))
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	return strings.Join(out, " · ")
}

func normalizePendingRequestOnSurface(surface *state.SurfaceConsoleRecord, record *state.RequestPromptRecord) *state.RequestPromptRecord {
	if record == nil {
		return nil
	}
	normalizeRequestPromptRecord(record)
	adoptRequestOwner(record, surface)
	return record
}

func adoptRequestOwner(record *state.RequestPromptRecord, surface *state.SurfaceConsoleRecord) {
	if record == nil || surface == nil {
		return
	}
	if record.OwnerSurfaceSessionID == "" {
		record.OwnerSurfaceSessionID = strings.TrimSpace(surface.SurfaceSessionID)
	}
	if record.OwnerGatewayID == "" {
		record.OwnerGatewayID = strings.TrimSpace(surface.GatewayID)
	}
	if record.OwnerChatID == "" {
		record.OwnerChatID = strings.TrimSpace(surface.ChatID)
	}
}

func requestOwnerSurface(s *Service, request *state.RequestPromptRecord) *state.SurfaceConsoleRecord {
	if s == nil || request == nil {
		return nil
	}
	if surfaceID := strings.TrimSpace(request.OwnerSurfaceSessionID); surfaceID != "" {
		if surface := s.root.Surfaces[surfaceID]; surface != nil {
			return surface
		}
	}
	return nil
}

func requestOwnerMatchesSurface(request *state.RequestPromptRecord, surface *state.SurfaceConsoleRecord) bool {
	if request == nil || surface == nil {
		return false
	}
	if ownerSurfaceID := strings.TrimSpace(request.OwnerSurfaceSessionID); ownerSurfaceID != "" {
		return ownerSurfaceID == strings.TrimSpace(surface.SurfaceSessionID)
	}
	return false
}

func requestMatchesTurn(request *state.RequestPromptRecord, threadID, turnID string) bool {
	if request == nil {
		return false
	}
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if turnID != "" && request.TurnID != "" && strings.TrimSpace(request.TurnID) != turnID {
		return false
	}
	if threadID != "" && request.ThreadID != "" && strings.TrimSpace(request.ThreadID) != threadID {
		return false
	}
	return true
}

func (s *Service) findPendingRequestByCommandID(commandID string) (*state.SurfaceConsoleRecord, *state.RequestPromptRecord) {
	if s == nil {
		return nil, nil
	}
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return nil, nil
	}
	for _, surface := range s.root.Surfaces {
		if surface == nil {
			continue
		}
		for _, request := range surface.PendingRequests {
			request = normalizePendingRequestOnSurface(surface, request)
			if request == nil || request.PendingDispatchCommandID != commandID {
				continue
			}
			if owner := requestOwnerSurface(s, request); owner != nil {
				if owned := pendingRequestRecord(owner, request.RequestID); owned != nil && owned.PendingDispatchCommandID == commandID {
					return owner, owned
				}
			}
			return surface, request
		}
	}
	return nil, nil
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

func ensurePendingRequestOrder(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	known := make(map[string]bool, len(surface.PendingRequests))
	ordered := make([]string, 0, len(surface.PendingRequests))
	for _, requestID := range surface.PendingRequestOrder {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" || known[requestID] {
			continue
		}
		if surface.PendingRequests[requestID] == nil {
			continue
		}
		known[requestID] = true
		ordered = append(ordered, requestID)
	}
	if len(known) == len(surface.PendingRequests) {
		surface.PendingRequestOrder = ordered
		return
	}
	missing := make([]*state.RequestPromptRecord, 0, len(surface.PendingRequests)-len(known))
	for requestID, request := range surface.PendingRequests {
		requestID = strings.TrimSpace(requestID)
		if request == nil || requestID == "" || known[requestID] {
			continue
		}
		missing = append(missing, request)
	}
	sort.SliceStable(missing, func(i, j int) bool {
		left := missing[i]
		right := missing[j]
		if left == nil || right == nil {
			return right != nil
		}
		leftAt := left.CreatedAt
		rightAt := right.CreatedAt
		switch {
		case leftAt.Equal(rightAt):
			return strings.TrimSpace(left.RequestID) < strings.TrimSpace(right.RequestID)
		case leftAt.IsZero():
			return false
		case rightAt.IsZero():
			return true
		default:
			return leftAt.Before(rightAt)
		}
	})
	for _, request := range missing {
		if request == nil {
			continue
		}
		requestID := strings.TrimSpace(request.RequestID)
		if requestID == "" || known[requestID] {
			continue
		}
		known[requestID] = true
		ordered = append(ordered, requestID)
	}
	surface.PendingRequestOrder = ordered
}

func enqueuePendingRequest(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord) {
	if surface == nil || request == nil {
		return
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	request = normalizePendingRequestOnSurface(surface, request)
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		return
	}
	surface.PendingRequests[requestID] = request
	ensurePendingRequestOrder(surface)
	if slices.Contains(surface.PendingRequestOrder, requestID) {
		return
	}
	surface.PendingRequestOrder = append(surface.PendingRequestOrder, requestID)
}

func removePendingRequest(surface *state.SurfaceConsoleRecord, requestID string) {
	if surface == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	delete(surface.PendingRequests, requestID)
	if len(surface.PendingRequestOrder) == 0 {
		return
	}
	filtered := surface.PendingRequestOrder[:0]
	for _, candidate := range surface.PendingRequestOrder {
		if strings.TrimSpace(candidate) == requestID {
			continue
		}
		filtered = append(filtered, candidate)
	}
	surface.PendingRequestOrder = filtered
}

func pendingRequestRecord(surface *state.SurfaceConsoleRecord, requestID string) *state.RequestPromptRecord {
	if surface == nil {
		return nil
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	return normalizePendingRequestOnSurface(surface, surface.PendingRequests[requestID])
}

func activePendingRequestID(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || len(surface.PendingRequests) == 0 {
		return ""
	}
	ensurePendingRequestOrder(surface)
	for _, requestID := range surface.PendingRequestOrder {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			continue
		}
		if surface.PendingRequests[requestID] == nil {
			continue
		}
		return requestID
	}
	return ""
}

func activePendingRequest(surface *state.SurfaceConsoleRecord) *state.RequestPromptRecord {
	return pendingRequestRecord(surface, activePendingRequestID(surface))
}

func pendingRequestIsActive(surface *state.SurfaceConsoleRecord, requestID string) bool {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false
	}
	return activePendingRequestID(surface) == requestID
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
		if approvalRequestSupportsExtendedDecisions(semanticKind) {
			add("acceptForSession", "本会话允许", "default")
		}
		add("decline", firstNonEmpty(metadataString(metadata, "declineLabel"), "拒绝"), "default")
		if approvalRequestSupportsExtendedDecisions(semanticKind) {
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

func approvalRequestSupportsExtendedDecisions(semanticKind string) bool {
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
	if requestLifecycleUsesWaitingDispatchPhase(request) {
		return requestPromptPendingDispatchStatusText(request)
	}
	switch normalizeRequestVisibilityState(request.VisibilityState) {
	case requestVisibilityPendingVisibility:
		return "当前有待处理请求，正在尝试把确认卡片显示到前台。请稍后重试，或发送 `/status` 触发恢复。"
	case requestVisibilityDeliveryDegraded:
		return "当前有待处理请求，但确认卡片尚未成功送达前台。请发送 `/status` 或进行一次前台交互以触发恢复。"
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

type RequestDeliveryReport struct {
	SurfaceSessionID string
	RequestID        string
	MessageID        string
	DeliveredAt      time.Time
}

func (s *Service) activePendingRequestNeedingVisibility(surface *state.SurfaceConsoleRecord) *state.RequestPromptRecord {
	request := activePendingRequest(surface)
	if request == nil {
		return nil
	}
	switch normalizeRequestVisibilityState(request.VisibilityState) {
	case requestVisibilityPendingVisibility, requestVisibilityDeliveryDegraded:
		return request
	default:
		return nil
	}
}

func requestNeedsVisibleRefresh(request *state.RequestPromptRecord) bool {
	if request == nil {
		return false
	}
	switch normalizeRequestLifecycleState(request.LifecycleState) {
	case requestLifecycleResolved, requestLifecycleAborted:
		return false
	}
	switch normalizeRequestVisibilityState(request.VisibilityState) {
	case requestVisibilityPendingVisibility, requestVisibilityDeliveryDegraded:
		return true
	default:
		return false
	}
}

func requestVisibilityRefreshStatusText(request *state.RequestPromptRecord) string {
	if request == nil {
		return ""
	}
	if normalizeRequestVisibilityState(request.VisibilityState) != requestVisibilityDeliveryDegraded {
		return ""
	}
	if errText := strings.TrimSpace(request.LastDeliveryError); errText != "" {
		return fmt.Sprintf("上一轮前台投递失败：%s。系统会继续尝试恢复这张确认卡片。", errText)
	}
	return "上一轮前台投递失败。系统会继续尝试恢复这张确认卡片。"
}

func markRequestVisible(request *state.RequestPromptRecord, messageID string, deliveredAt time.Time) {
	if request == nil {
		return
	}
	if normalizeRequestLifecycleState(request.LifecycleState) == requestLifecycleAwaitingVisibility {
		setRequestLifecycleState(request, requestLifecycleEditingVisible)
	}
	request.VisibleMessageID = strings.TrimSpace(messageID)
	if deliveredAt.IsZero() {
		deliveredAt = time.Now().UTC()
	}
	request.VisibleAt = deliveredAt
	request.LastDeliveryAttemptAt = deliveredAt
	request.LastDeliveryError = ""
	request.NeedsRedelivery = false
	request.VisibilityState = requestVisibilityVisible
	if request.DeliveryAttemptCount < 1 {
		request.DeliveryAttemptCount = 1
	}
}

func markRequestDeliveryDegraded(request *state.RequestPromptRecord, attemptedAt time.Time, errText string) {
	if request == nil {
		return
	}
	if attemptedAt.IsZero() {
		attemptedAt = time.Now().UTC()
	}
	request.LastDeliveryAttemptAt = attemptedAt
	request.LastDeliveryError = strings.TrimSpace(errText)
	request.NeedsRedelivery = true
	request.VisibilityState = requestVisibilityDeliveryDegraded
	request.VisibleMessageID = ""
}

func (s *Service) ensurePendingRequestVisible(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord, threadTitleHint string) []eventcontract.Event {
	if surface == nil || request == nil {
		return nil
	}
	request = normalizePendingRequestOnSurface(surface, request)
	if !pendingRequestIsActive(surface, request.RequestID) {
		return nil
	}
	if !requestPromptRenderable(request.RequestType) {
		return notice(surface, "request_unsupported", fmt.Sprintf("收到 %s 请求，当前飞书端还不能直接处理，已保持为待处理状态。", request.RequestType))
	}
	if normalizeRequestType(request.RequestType) == "tool_callback" && !requestLifecycleUsesWaitingDispatchPhase(request) {
		return s.autoDispatchUnsupportedToolCallback(surface, request, threadTitleHint)
	}
	if !requestNeedsVisibleRefresh(request) {
		return nil
	}
	if request.LastDeliveryAttemptAt.IsZero() || request.LastDeliveryAttemptAt.Before(s.now()) || request.DeliveryAttemptCount == 0 {
		request.LastDeliveryAttemptAt = s.now()
	}
	request.DeliveryAttemptCount++
	return []eventcontract.Event{s.requestPromptDeliveryEvent(surface, request, threadTitleHint)}
}
