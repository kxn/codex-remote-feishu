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
	case "run_command":
		commandText := strings.TrimSpace(stringMapValue(value, "command_text"))
		if commandText == "" {
			commandText = strings.TrimSpace(stringMapValue(value, "command"))
		}
		action, ok := parseTextAction(commandText)
		if !ok {
			return control.Action{}, false
		}
		action.GatewayID = g.config.GatewayID
		action.SurfaceSessionID = surfaceSessionID
		action.ChatID = chatID
		action.ActorUserID = operatorID
		action.MessageID = messageID
		return action, true
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
	return control.ParseFeishuTextAction(text)
}

func menuAction(eventKey string) (control.Action, bool) {
	return control.ParseFeishuMenuAction(eventKey)
}

func normalizeMenuEventKey(value string) string {
	return control.NormalizeFeishuMenuEventKey(value)
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
