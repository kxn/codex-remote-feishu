package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (g *LiveGateway) parseMessageEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) (control.Action, bool, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return control.Action{}, false, nil
	}
	message := event.Event.Message
	chatID := stringPtr(message.ChatId)
	chatType := stringPtr(message.ChatType)
	senderUserID := userIDFromMessage(event.Event.Sender)
	surfaceSessionID := surfaceIDForInbound(g.config.GatewayID, chatID, chatType, senderUserID)
	action := control.Action{
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           chatID,
		ActorUserID:      senderUserID,
		MessageID:        stringPtr(message.MessageId),
		Inbound:          inboundMetaFromMessageEvent(event),
	}
	replyTargetMessageID := referencedMessageID(message)
	if replyTargetMessageID != "" {
		action.TargetMessageID = replyTargetMessageID
	}

	switch strings.ToLower(stringPtr(message.MessageType)) {
	case "text":
		text, err := parseTextContent(stringPtr(message.Content))
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "parse_text_content", err)
			return control.Action{}, false, err
		}
		commandAction, handled := parseTextAction(text)
		if handled {
			commandAction.GatewayID = g.config.GatewayID
			commandAction.SurfaceSessionID = surfaceSessionID
			commandAction.ChatID = chatID
			commandAction.ActorUserID = action.ActorUserID
			commandAction.MessageID = action.MessageID
			commandAction.Inbound = action.Inbound
			return commandAction, true, nil
		}
		currentInputs := []agentproto.Input{{Type: agentproto.InputText, Text: text}}
		inputs := append(g.quotedInputs(ctx, message), currentInputs...)
		action.Kind = control.ActionTextMessage
		action.Text = text
		action.Inputs = inputs
		action.SteerInputs = currentInputs
		g.recordSurfaceMessage(action.MessageID, surfaceSessionID)
		return action, true, nil
	case "post":
		inputs, text, err := g.parsePostInputs(ctx, action.MessageID, stringPtr(message.Content))
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "parse_post_content", err)
			return control.Action{}, false, err
		}
		if len(inputs) == 0 {
			logInboundMessageIgnored(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "empty_post_inputs")
			return control.Action{}, false, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = text
		action.Inputs = append(g.quotedInputs(ctx, message), inputs...)
		action.SteerInputs = append([]agentproto.Input(nil), inputs...)
		g.recordSurfaceMessage(action.MessageID, surfaceSessionID)
		return action, true, nil
	case "image":
		imageKey, err := parseImageKey(stringPtr(message.Content))
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "parse_image_content", err)
			return control.Action{}, false, err
		}
		path, mimeType, err := g.downloadImageFn(ctx, stringPtr(message.MessageId), imageKey)
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "download_image", err)
			return control.Action{}, false, err
		}
		action.Kind = control.ActionImageMessage
		action.LocalPath = path
		action.MIMEType = mimeType
		action.SteerInputs = []agentproto.Input{{Type: agentproto.InputLocalImage, Path: path, MIMEType: mimeType}}
		g.recordSurfaceMessage(action.MessageID, surfaceSessionID)
		return action, true, nil
	case "merge_forward":
		text, err := g.parseMergeForwardEventContent(ctx, message)
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "parse_merge_forward_content", err)
			return control.Action{}, false, err
		}
		merged := mergeForwardTextInput(text)
		if merged.Text == "" {
			logInboundMessageIgnored(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "empty_merge_forward_content")
			return control.Action{}, false, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = text
		action.Inputs = append(g.quotedInputs(ctx, message), merged)
		g.recordSurfaceMessage(action.MessageID, surfaceSessionID)
		return action, true, nil
	default:
		logInboundMessageIgnored(g.config.GatewayID, surfaceSessionID, action.Inbound, message, "unsupported_message_type")
		return control.Action{}, false, nil
	}
}

func logInboundMessageIgnored(gatewayID, surfaceSessionID string, inbound *control.ActionInboundMeta, message *larkim.EventMessage, reason string) {
	log.Printf(
		"feishu inbound message ignored: gateway=%s surface=%s message=%s type=%s chat=%s chat_type=%s thread=%s root=%s parent=%s event=%s request=%s reason=%s preview=%q",
		strings.TrimSpace(gatewayID),
		strings.TrimSpace(surfaceSessionID),
		strings.TrimSpace(stringPtr(message.MessageId)),
		strings.ToLower(strings.TrimSpace(stringPtr(message.MessageType))),
		strings.TrimSpace(stringPtr(message.ChatId)),
		strings.TrimSpace(stringPtr(message.ChatType)),
		strings.TrimSpace(stringPtr(message.ThreadId)),
		strings.TrimSpace(stringPtr(message.RootId)),
		strings.TrimSpace(stringPtr(message.ParentId)),
		inboundMetaValue(inbound, func(meta *control.ActionInboundMeta) string { return meta.EventID }),
		inboundMetaValue(inbound, func(meta *control.ActionInboundMeta) string { return meta.RequestID }),
		strings.TrimSpace(reason),
		inboundMessagePreview(message),
	)
}

func logInboundMessageParseFailed(gatewayID, surfaceSessionID string, inbound *control.ActionInboundMeta, message *larkim.EventMessage, reason string, err error) {
	log.Printf(
		"feishu inbound message parse failed: gateway=%s surface=%s message=%s type=%s chat=%s chat_type=%s thread=%s root=%s parent=%s event=%s request=%s reason=%s err=%v preview=%q",
		strings.TrimSpace(gatewayID),
		strings.TrimSpace(surfaceSessionID),
		strings.TrimSpace(stringPtr(message.MessageId)),
		strings.ToLower(strings.TrimSpace(stringPtr(message.MessageType))),
		strings.TrimSpace(stringPtr(message.ChatId)),
		strings.TrimSpace(stringPtr(message.ChatType)),
		strings.TrimSpace(stringPtr(message.ThreadId)),
		strings.TrimSpace(stringPtr(message.RootId)),
		strings.TrimSpace(stringPtr(message.ParentId)),
		inboundMetaValue(inbound, func(meta *control.ActionInboundMeta) string { return meta.EventID }),
		inboundMetaValue(inbound, func(meta *control.ActionInboundMeta) string { return meta.RequestID }),
		strings.TrimSpace(reason),
		err,
		inboundMessagePreview(message),
	)
}

func inboundMetaValue(meta *control.ActionInboundMeta, pick func(*control.ActionInboundMeta) string) string {
	if meta == nil || pick == nil {
		return ""
	}
	return strings.TrimSpace(pick(meta))
}

func inboundMessagePreview(message *larkim.EventMessage) string {
	if message == nil {
		return ""
	}
	messageType := strings.ToLower(strings.TrimSpace(stringPtr(message.MessageType)))
	rawContent := strings.TrimSpace(stringPtr(message.Content))
	switch messageType {
	case "text":
		text, err := parseTextContent(rawContent)
		if err == nil {
			return trimLogPreview(text)
		}
	case "post":
		var content feishuPostContent
		if err := json.Unmarshal([]byte(rawContent), &content); err == nil {
			textParts := make([]string, 0, len(content.Content)+1)
			if title := strings.TrimSpace(content.Title); title != "" {
				textParts = append(textParts, title)
			}
			for _, paragraph := range content.Content {
				var segment strings.Builder
				for _, node := range paragraph {
					switch strings.ToLower(strings.TrimSpace(node.Tag)) {
					case "text":
						segment.WriteString(node.Text)
					case "a":
						if text := strings.TrimSpace(node.Text); text != "" {
							segment.WriteString(text)
						}
					case "at":
						if text := strings.TrimSpace(node.Text); text != "" {
							segment.WriteString(text)
						}
					case "emotion":
						if emoji := strings.TrimSpace(node.EmojiType); emoji != "" {
							segment.WriteString(":" + emoji + ":")
						}
					case "code_block":
						if text := strings.TrimSpace(node.Text); text != "" {
							segment.WriteString(text)
						}
					}
				}
				if text := strings.TrimSpace(segment.String()); text != "" {
					textParts = append(textParts, text)
				}
			}
			if len(textParts) > 0 {
				return trimLogPreview(strings.Join(textParts, "\n\n"))
			}
		}
	case "merge_forward":
		text, err := parseMergeForwardContent(rawContent)
		if err == nil {
			return trimLogPreview(text)
		}
	}
	return trimLogPreview(rawContent)
}

func trimLogPreview(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const maxPreviewRunes = 160
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxPreviewRunes {
		return text
	}
	return string(runes[:maxPreviewRunes]) + "..."
}

func (g *LiveGateway) parseMessageRecalledEvent(event *larkim.P2MessageRecalledV1) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.MessageId == nil {
		return control.Action{}, false
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	if messageID == "" {
		return control.Action{}, false
	}
	g.mu.Lock()
	surfaceSessionID := g.messages[messageID]
	g.mu.Unlock()
	if surfaceSessionID == "" {
		return control.Action{}, false
	}
	return control.Action{
		Kind:             control.ActionMessageRecalled,
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           strings.TrimSpace(stringPtr(event.Event.ChatId)),
		TargetMessageID:  messageID,
		Inbound:          inboundMetaFromMessageRecalledEvent(event),
	}, true
}

func (g *LiveGateway) parseMessageReactionCreatedEvent(event *larkim.P2MessageReactionCreatedV1) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.MessageId == nil || event.Event.ReactionType == nil {
		return control.Action{}, false
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	if messageID == "" {
		return control.Action{}, false
	}
	reactionType := strings.TrimSpace(stringPtr(event.Event.ReactionType.EmojiType))
	if reactionType == "" {
		return control.Action{}, false
	}
	actorUserID := userIDFromLarkUserID(event.Event.UserId)
	if actorUserID == "" {
		return control.Action{}, false
	}
	g.mu.Lock()
	surfaceSessionID := g.messages[messageID]
	g.mu.Unlock()
	if surfaceSessionID == "" {
		return control.Action{}, false
	}
	return control.Action{
		Kind:             control.ActionReactionCreated,
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: surfaceSessionID,
		ActorUserID:      actorUserID,
		ReactionType:     reactionType,
		TargetMessageID:  messageID,
		Inbound:          inboundMetaFromMessageReactionCreatedEvent(event),
	}, true
}

func (g *LiveGateway) parseMenuEvent(event *larkapplication.P2BotMenuV6) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.EventKey == nil {
		return control.Action{}, false
	}
	rawKey := *event.Event.EventKey
	action, ok := menuAction(rawKey)
	if !ok {
		log.Printf("feishu bot menu ignored: raw_key=%q normalized=%q", rawKey, normalizeMenuEventKey(rawKey))
		return control.Action{}, false
	}
	log.Printf("feishu bot menu handled: raw_key=%q normalized=%q action=%s", rawKey, normalizeMenuEventKey(rawKey), action.Kind)
	operatorID := operatorUserID(event.Event.Operator)
	action.GatewayID = g.config.GatewayID
	action.SurfaceSessionID = surfaceIDForInbound(g.config.GatewayID, "", "p2p", operatorID)
	action.ActorUserID = operatorID
	action.Inbound = inboundMetaFromMenuEvent(event)
	return action, true
}

func parseTextContent(rawContent string) (string, error) {
	var content feishuTextContent
	if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
		return "", err
	}
	return content.Text, nil
}

func parseImageKey(rawContent string) (string, error) {
	var content struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
		return "", err
	}
	if strings.TrimSpace(content.ImageKey) == "" {
		return "", fmt.Errorf("missing image_key")
	}
	return strings.TrimSpace(content.ImageKey), nil
}
