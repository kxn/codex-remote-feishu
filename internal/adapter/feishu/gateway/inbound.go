package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func quotedMessageInputs(ctx context.Context, env InboundEnv, message *larkim.EventMessage) QuotedMessageInputs {
	if env.QuotedMessageInputs != nil {
		return env.QuotedMessageInputs(ctx, message)
	}
	if env.QuotedInputs != nil {
		return QuotedMessageInputs{Inputs: env.QuotedInputs(ctx, message)}
	}
	return QuotedMessageInputs{}
}

func logInboundMessageIgnored(gatewayID, surfaceSessionID string, inbound *control.ActionInboundMeta, message *larkim.EventMessage, reason string) {
	log.Printf(
		"feishu inbound message ignored: gateway=%s surface=%s message=%s type=%s chat=%s chat_type=%s thread=%s root=%s parent=%s event=%s request=%s reason=%s preview=%q",
		strings.TrimSpace(gatewayID),
		strings.TrimSpace(surfaceSessionID),
		strings.TrimSpace(xutil.StringValue(message.MessageId)),
		strings.ToLower(strings.TrimSpace(xutil.StringValue(message.MessageType))),
		strings.TrimSpace(xutil.StringValue(message.ChatId)),
		strings.TrimSpace(xutil.StringValue(message.ChatType)),
		strings.TrimSpace(xutil.StringValue(message.ThreadId)),
		strings.TrimSpace(xutil.StringValue(message.RootId)),
		strings.TrimSpace(xutil.StringValue(message.ParentId)),
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
		strings.TrimSpace(xutil.StringValue(message.MessageId)),
		strings.ToLower(strings.TrimSpace(xutil.StringValue(message.MessageType))),
		strings.TrimSpace(xutil.StringValue(message.ChatId)),
		strings.TrimSpace(xutil.StringValue(message.ChatType)),
		strings.TrimSpace(xutil.StringValue(message.ThreadId)),
		strings.TrimSpace(xutil.StringValue(message.RootId)),
		strings.TrimSpace(xutil.StringValue(message.ParentId)),
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
	messageType := strings.ToLower(strings.TrimSpace(xutil.StringValue(message.MessageType)))
	rawContent := strings.TrimSpace(xutil.StringValue(message.Content))
	switch messageType {
	case "text":
		text, _, err := parseFeishuEventText(rawContent, message.Mentions)
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
		text, err := ParseMergeForwardContent(rawContent)
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

func ParseMessageRecalledEvent(env InboundEnv, event *larkim.P2MessageRecalledV1) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.MessageId == nil {
		return control.Action{}, false
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	if messageID == "" {
		return control.Action{}, false
	}
	surfaceSessionID := ""
	if env.LookupSurfaceMessage != nil {
		surfaceSessionID = strings.TrimSpace(env.LookupSurfaceMessage(messageID))
	}
	if surfaceSessionID == "" {
		return control.Action{}, false
	}
	return control.Action{
		Kind:             control.ActionMessageRecalled,
		GatewayID:        strings.TrimSpace(env.GatewayID),
		SurfaceSessionID: surfaceSessionID,
		ChatID:           strings.TrimSpace(xutil.StringValue(event.Event.ChatId)),
		TargetMessageID:  messageID,
		Inbound:          InboundMetaFromMessageRecalledEvent(event),
	}, true
}

func ParseMessageReactionCreatedEvent(env InboundEnv, event *larkim.P2MessageReactionCreatedV1) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.MessageId == nil || event.Event.ReactionType == nil {
		return control.Action{}, false
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	if messageID == "" {
		return control.Action{}, false
	}
	reactionType := strings.TrimSpace(xutil.StringValue(event.Event.ReactionType.EmojiType))
	if reactionType == "" {
		return control.Action{}, false
	}
	actorUserID := userIDFromLarkUserID(event.Event.UserId)
	if actorUserID == "" {
		return control.Action{}, false
	}
	surfaceSessionID := ""
	if env.LookupSurfaceMessage != nil {
		surfaceSessionID = strings.TrimSpace(env.LookupSurfaceMessage(messageID))
	}
	if surfaceSessionID == "" {
		return control.Action{}, false
	}
	return control.Action{
		Kind:             control.ActionReactionCreated,
		GatewayID:        strings.TrimSpace(env.GatewayID),
		SurfaceSessionID: surfaceSessionID,
		ActorUserID:      actorUserID,
		ReactionType:     reactionType,
		TargetMessageID:  messageID,
		Inbound:          InboundMetaFromMessageReactionCreatedEvent(event),
	}, true
}

func ParseMenuEvent(gatewayID string, event *larkapplication.P2BotMenuV6) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.EventKey == nil {
		return control.Action{}, false
	}
	rawKey := *event.Event.EventKey
	action, ok := menuAction(rawKey)
	if !ok {
		log.Printf("feishu bot menu ignored: raw_key=%q normalized=%q", rawKey, NormalizeMenuEventKey(rawKey))
		return control.Action{}, false
	}
	log.Printf("feishu bot menu handled: raw_key=%q normalized=%q action=%s", rawKey, NormalizeMenuEventKey(rawKey), action.Kind)
	operatorID := operatorUserID(event.Event.Operator)
	action.GatewayID = strings.TrimSpace(gatewayID)
	action.SurfaceSessionID = SurfaceIDForInbound(gatewayID, "", "p2p", operatorID)
	action.ActorUserID = operatorID
	action.Inbound = InboundMetaFromMenuEvent(event)
	return action, true
}

func ParseTextContent(rawContent string) (string, error) {
	var content feishuTextContent
	if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
		return "", err
	}
	return content.Text, nil
}

func ParseImageKey(rawContent string) (string, error) {
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

func ParseFileContent(rawContent string) (string, string, error) {
	var content struct {
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
		return "", "", err
	}
	fileKey := strings.TrimSpace(content.FileKey)
	if fileKey == "" {
		return "", "", fmt.Errorf("missing file_key")
	}
	fileName := strings.TrimSpace(content.FileName)
	if fileName == "" {
		fileName = strings.TrimSpace(content.Name)
	}
	return fileKey, fileName, nil
}
