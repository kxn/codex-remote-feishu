package feishu

import (
	"fmt"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (g *LiveGateway) parseCardActionTriggerEvent(event *larkcallback.CardActionTriggerEvent) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return control.Action{}, false
	}
	value := event.Event.Action.Value
	kind := strings.TrimSpace(stringMapValue(value, "kind"))
	if kind == "" {
		return control.Action{}, false
	}

	operatorID := operatorUserIDFromCard(event.Event.Operator)
	chatID := ""
	messageID := ""
	if event.Event.Context != nil {
		chatID = strings.TrimSpace(event.Event.Context.OpenChatID)
		messageID = strings.TrimSpace(event.Event.Context.OpenMessageID)
	}
	surfaceSessionID := g.surfaceForCardAction(messageID, chatID, operatorID)
	if surfaceSessionID == "" {
		return control.Action{}, false
	}

	switch kind {
	case "attach_instance":
		instanceID := strings.TrimSpace(stringMapValue(value, "instance_id"))
		if instanceID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionAttachInstance,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			InstanceID:       instanceID,
		}, true
	case "use_thread":
		threadID := strings.TrimSpace(stringMapValue(value, "thread_id"))
		if threadID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionUseThread,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			ThreadID:         threadID,
		}, true
	case "resume_headless_thread":
		return control.Action{
			Kind:             control.ActionRemovedCommand,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Text:             "resume_headless_thread",
		}, true
	case "kick_thread_confirm":
		threadID := strings.TrimSpace(stringMapValue(value, "thread_id"))
		if threadID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionConfirmKickThread,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			ThreadID:         threadID,
		}, true
	case "kick_thread_cancel":
		return control.Action{
			Kind:             control.ActionCancelKickThread,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
		}, true
	case "prompt_select":
		promptID := strings.TrimSpace(stringMapValue(value, "prompt_id"))
		optionID := strings.TrimSpace(stringMapValue(value, "option_id"))
		if promptID == "" || optionID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionSelectPrompt,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PromptID:         promptID,
			OptionID:         optionID,
		}, true
	case "request_respond":
		requestID := strings.TrimSpace(stringMapValue(value, "request_id"))
		if requestID == "" {
			return control.Action{}, false
		}
		optionID := strings.TrimSpace(stringMapValue(value, "request_option_id"))
		if optionID == "" {
			if value["approved"] != nil {
				if boolMapValue(value, "approved") {
					optionID = "accept"
				} else {
					optionID = "decline"
				}
			}
		}
		return control.Action{
			Kind:             control.ActionRespondRequest,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			RequestID:        requestID,
			RequestType:      strings.TrimSpace(stringMapValue(value, "request_type")),
			RequestOptionID:  optionID,
			Approved:         boolMapValue(value, "approved"),
		}, true
	default:
		return control.Action{}, false
	}
}

func (g *LiveGateway) surfaceForCardAction(messageID, chatID, operatorID string) string {
	if operatorID != "" {
		return surfaceIDForInbound(g.config.GatewayID, "", "p2p", operatorID)
	}
	if messageID != "" {
		g.mu.Lock()
		surfaceSessionID := g.messages[messageID]
		g.mu.Unlock()
		if surfaceSessionID != "" {
			return surfaceSessionID
		}
	}
	if chatID != "" {
		return surfaceID(g.config.GatewayID, chatID, "")
	}
	return ""
}

func parseTextAction(text string) (control.Action, bool) {
	trimmed := strings.TrimSpace(text)
	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		switch strings.ToLower(fields[0]) {
		case "/model":
			return control.Action{Kind: control.ActionModelCommand, Text: trimmed}, true
		case "/reasoning", "/effort":
			return control.Action{Kind: control.ActionReasoningCommand, Text: trimmed}, true
		case "/access", "/approval":
			return control.Action{Kind: control.ActionAccessCommand, Text: trimmed}, true
		}
	}
	switch trimmed {
	case "/list":
		return control.Action{Kind: control.ActionListInstances}, true
	case "/status":
		return control.Action{Kind: control.ActionStatus}, true
	case "/stop":
		return control.Action{Kind: control.ActionStop}, true
	case "/new":
		return control.Action{Kind: control.ActionNewThread}, true
	case "/newinstance":
		return control.Action{Kind: control.ActionRemovedCommand, Text: "/newinstance"}, true
	case "/killinstance":
		return control.Action{Kind: control.ActionKillInstance}, true
	case "/threads", "/use", "/sessions":
		return control.Action{Kind: control.ActionShowThreads}, true
	case "/useall", "/sessionsall", "/sessions/all":
		return control.Action{Kind: control.ActionShowAllThreads}, true
	case "/follow":
		return control.Action{Kind: control.ActionFollowLocal}, true
	case "/detach":
		return control.Action{Kind: control.ActionDetach}, true
	default:
		return control.Action{}, false
	}
}

func menuAction(eventKey string) (control.Action, bool) {
	if action, ok := dynamicMenuAction(eventKey); ok {
		return action, true
	}
	switch normalizeMenuEventKey(eventKey) {
	case "list":
		return control.Action{Kind: control.ActionListInstances}, true
	case "status":
		return control.Action{Kind: control.ActionStatus}, true
	case "stop":
		return control.Action{Kind: control.ActionStop}, true
	case "new", "newthread":
		return control.Action{Kind: control.ActionNewThread}, true
	case "newinstance":
		return control.Action{Kind: control.ActionRemovedCommand, Text: "new_instance"}, true
	case "killinstance":
		return control.Action{Kind: control.ActionKillInstance}, true
	case "threads", "use", "sessions", "showthreads", "showsessions":
		return control.Action{Kind: control.ActionShowThreads}, true
	case "threadsall", "useall", "sessionsall", "showallthreads", "showallsessions":
		return control.Action{Kind: control.ActionShowAllThreads}, true
	case "reasonlow", "reason_low", "reason-low":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning low"}, true
	case "reasonmedium", "reason_medium", "reason-medium":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning medium"}, true
	case "reasonhigh", "reason_high", "reason-high":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning high"}, true
	case "reasonxhigh", "reason_xhigh", "reason-xhigh":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning xhigh"}, true
	case "accessfull", "approvalfull":
		return control.Action{Kind: control.ActionAccessCommand, Text: "/access full"}, true
	case "accessconfirm", "approvalconfirm":
		return control.Action{Kind: control.ActionAccessCommand, Text: "/access confirm"}, true
	default:
		return control.Action{}, false
	}
}

func dynamicMenuAction(eventKey string) (control.Action, bool) {
	trimmed := strings.TrimSpace(eventKey)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"model_", "model-"} {
		if strings.HasPrefix(lower, prefix) {
			model := strings.TrimSpace(trimmed[len(prefix):])
			if model == "" {
				return control.Action{}, false
			}
			return control.Action{Kind: control.ActionModelCommand, Text: "/model " + model}, true
		}
	}
	for _, prefix := range []string{"reason_", "reason-"} {
		if strings.HasPrefix(lower, prefix) {
			effort := strings.ToLower(strings.TrimSpace(trimmed[len(prefix):]))
			if !menuReasoningEffort(effort) {
				return control.Action{}, false
			}
			return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning " + effort}, true
		}
	}
	return control.Action{}, false
}

func menuReasoningEffort(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func normalizeMenuEventKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func menuActionKind(eventKey string) (control.ActionKind, bool) {
	action, ok := menuAction(eventKey)
	if !ok {
		return "", false
	}
	return action.Kind, true
}

func surfaceID(gatewayID, chatID, fallbackUserID string) string {
	if chatID != "" {
		return SurfaceRef{
			Platform:  PlatformFeishu,
			GatewayID: normalizeGatewayID(gatewayID),
			ScopeKind: ScopeKindChat,
			ScopeID:   strings.TrimSpace(chatID),
		}.SurfaceID()
	}
	return SurfaceRef{
		Platform:  PlatformFeishu,
		GatewayID: normalizeGatewayID(gatewayID),
		ScopeKind: ScopeKindUser,
		ScopeID:   strings.TrimSpace(fallbackUserID),
	}.SurfaceID()
}

func surfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID string) string {
	if strings.EqualFold(chatType, "p2p") && fallbackUserID != "" {
		return surfaceID(gatewayID, "", fallbackUserID)
	}
	return surfaceID(gatewayID, chatID, fallbackUserID)
}

func userIDFromMessage(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	return chooseFirst(
		stringPtr(sender.SenderId.UserId),
		stringPtr(sender.SenderId.OpenId),
		stringPtr(sender.SenderId.UnionId),
	)
}

func operatorUserID(operator *larkapplication.Operator) string {
	if operator == nil || operator.OperatorId == nil {
		return ""
	}
	return chooseFirst(
		stringPtr(operator.OperatorId.UserId),
		stringPtr(operator.OperatorId.OpenId),
		stringPtr(operator.OperatorId.UnionId),
	)
}

func operatorUserIDFromCard(operator *larkcallback.Operator) string {
	if operator == nil {
		return ""
	}
	return chooseFirst(
		stringPtr(operator.UserID),
		strings.TrimSpace(operator.OpenID),
	)
}

func reactionKey(messageID, emojiType string) string {
	return messageID + "|" + emojiType
}

func mimeExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func chooseFirst(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringMapValue(values map[string]interface{}, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch current := value.(type) {
	case string:
		return current
	case fmt.Stringer:
		return current.String()
	default:
		return fmt.Sprint(current)
	}
}

func boolMapValue(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	current, _ := value.(bool)
	return current
}

func ResolveReceiveTarget(chatID, actorUserID string) (string, string) {
	if strings.TrimSpace(chatID) != "" {
		return chatID, larkim.ReceiveIdTypeChatId
	}
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return "", ""
	}
	switch {
	case strings.HasPrefix(actorUserID, "ou_"):
		return actorUserID, larkim.ReceiveIdTypeOpenId
	case strings.HasPrefix(actorUserID, "on_"):
		return actorUserID, larkim.ReceiveIdTypeUnionId
	default:
		return actorUserID, larkim.ReceiveIdTypeUserId
	}
}
