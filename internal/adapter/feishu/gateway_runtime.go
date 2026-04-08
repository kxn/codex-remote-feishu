package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func (g *LiveGateway) Start(ctx context.Context, handler ActionHandler) error {
	dispatch := dispatcher.NewEventDispatcher("", "")
	dispatch.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		action, ok, err := g.parseMessageEvent(ctx, event)
		if err != nil || !ok {
			return err
		}
		handler(ctx, action)
		return nil
	})
	dispatch.OnP2MessageRecalledV1(func(ctx context.Context, event *larkim.P2MessageRecalledV1) error {
		action, ok := g.parseMessageRecalledEvent(event)
		if !ok {
			return nil
		}
		handler(ctx, action)
		return nil
	})
	dispatch.OnP2CardActionTrigger(func(ctx context.Context, event *larkcallback.CardActionTriggerEvent) (*larkcallback.CardActionTriggerResponse, error) {
		action, ok := g.parseCardActionTriggerEvent(event)
		if ok {
			handler(ctx, action)
		}
		return &larkcallback.CardActionTriggerResponse{}, nil
	})
	dispatch.OnP2BotMenuV6(func(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
		if event == nil || event.Event == nil || event.Event.EventKey == nil {
			return nil
		}
		rawKey := *event.Event.EventKey
		action, ok := menuAction(rawKey)
		if !ok {
			log.Printf("feishu bot menu ignored: raw_key=%q normalized=%q", rawKey, normalizeMenuEventKey(rawKey))
			return nil
		}
		log.Printf("feishu bot menu handled: raw_key=%q normalized=%q action=%s", rawKey, normalizeMenuEventKey(rawKey), action.Kind)
		operatorID := operatorUserID(event.Event.Operator)
		action.GatewayID = g.config.GatewayID
		action.SurfaceSessionID = surfaceIDForInbound(g.config.GatewayID, "", "p2p", operatorID)
		action.ActorUserID = operatorID
		handler(ctx, action)
		return nil
	})
	return newGatewayWSRunner(g.config, dispatch, g.emitState).Run(ctx)
}

func (g *LiveGateway) SetStateHook(hook func(GatewayState, error)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stateHook = hook
}

func (g *LiveGateway) emitState(state GatewayState, err error) {
	g.mu.Lock()
	hook := g.stateHook
	g.mu.Unlock()
	if hook != nil {
		hook(state, err)
	}
}

func (g *LiveGateway) Apply(ctx context.Context, operations []Operation) error {
	for _, operation := range operations {
		if operation.GatewayID != "" && normalizeGatewayID(operation.GatewayID) != g.config.GatewayID {
			return fmt.Errorf("gateway apply mismatch: operation gateway=%s gateway=%s", operation.GatewayID, g.config.GatewayID)
		}
		if err := g.applyOne(ctx, operation); err != nil {
			return err
		}
	}
	return nil
}

func (g *LiveGateway) applyOne(ctx context.Context, operation Operation) error {
	switch operation.Kind {
	case OperationSendText:
		body, _ := json.Marshal(map[string]string{"text": operation.Text})
		receiveID, receiveIDType := operation.ReceiveID, operation.ReceiveIDType
		if receiveID == "" || receiveIDType == "" {
			receiveID, receiveIDType = ResolveReceiveTarget(operation.ChatID, "")
		}
		if receiveID == "" || receiveIDType == "" {
			return fmt.Errorf("send text failed: missing receive target")
		}
		resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "text", string(body))
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("send text failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		if resp.Data != nil {
			g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
		}
		return nil
	case OperationSendCard:
		card, err := json.Marshal(buildCard(operation.CardTitle, operation.CardBody, operation.CardThemeKey, operation.CardElements))
		if err != nil {
			return err
		}
		receiveID, receiveIDType := operation.ReceiveID, operation.ReceiveIDType
		if receiveID == "" || receiveIDType == "" {
			receiveID, receiveIDType = ResolveReceiveTarget(operation.ChatID, "")
		}
		if receiveID == "" || receiveIDType == "" {
			return fmt.Errorf("send card failed: missing receive target")
		}
		if strings.TrimSpace(operation.ReplyToMessageID) != "" {
			resp, err := g.replyMessageFn(ctx, operation.ReplyToMessageID, "interactive", string(card))
			if err == nil && resp != nil && resp.Success() {
				if resp.Data != nil {
					g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
				}
				return nil
			}
			log.Printf(
				"feishu reply fallback: surface=%s reply_to=%s err=%v code=%d msg=%s",
				operation.SurfaceSessionID,
				operation.ReplyToMessageID,
				err,
				replyRespCode(resp),
				replyRespMsg(resp),
			)
		}
		resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "interactive", string(card))
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("send card failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		if resp.Data != nil {
			g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
		}
		return nil
	case OperationAddReaction:
		if operation.MessageID == "" {
			return nil
		}
		resp, err := g.client.Im.V1.MessageReaction.Create(ctx, larkim.NewCreateMessageReactionReqBuilder().
			MessageId(operation.MessageID).
			Body(larkim.NewCreateMessageReactionReqBodyBuilder().
				ReactionType(larkim.NewEmojiBuilder().EmojiType(operation.EmojiType).Build()).
				Build()).
			Build())
		if err != nil {
			return err
		}
		if !resp.Success() {
			if ignoredMissingReactionError(resp.Code, resp.Msg) {
				return nil
			}
			return fmt.Errorf("add reaction failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		g.mu.Lock()
		if resp.Data != nil && resp.Data.ReactionId != nil {
			g.reactions[reactionKey(operation.MessageID, operation.EmojiType)] = *resp.Data.ReactionId
		}
		g.mu.Unlock()
		return nil
	case OperationRemoveReaction:
		g.mu.Lock()
		reactionID := g.reactions[reactionKey(operation.MessageID, operation.EmojiType)]
		g.mu.Unlock()
		if reactionID == "" {
			return nil
		}
		resp, err := g.client.Im.V1.MessageReaction.Delete(ctx, larkim.NewDeleteMessageReactionReqBuilder().
			MessageId(operation.MessageID).
			ReactionId(reactionID).
			Build())
		if err != nil {
			return err
		}
		if !resp.Success() {
			if ignoredMissingReactionError(resp.Code, resp.Msg) {
				g.mu.Lock()
				delete(g.reactions, reactionKey(operation.MessageID, operation.EmojiType))
				g.mu.Unlock()
				return nil
			}
			return fmt.Errorf("remove reaction failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		g.mu.Lock()
		delete(g.reactions, reactionKey(operation.MessageID, operation.EmojiType))
		g.mu.Unlock()
		return nil
	default:
		return nil
	}
}

func (g *LiveGateway) createMessage(ctx context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
	return g.client.Im.V1.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build())
}

func (g *LiveGateway) replyMessage(ctx context.Context, messageID, msgType, content string) (*larkim.ReplyMessageResp, error) {
	return g.client.Im.V1.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			Build()).
		Build())
}

func replyRespCode(resp *larkim.ReplyMessageResp) int {
	if resp == nil {
		return 0
	}
	return resp.Code
}

func replyRespMsg(resp *larkim.ReplyMessageResp) string {
	if resp == nil {
		return ""
	}
	return resp.Msg
}

func ignoredMissingReactionError(_ int, msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	missingHints := []string{
		"message not found",
		"target message not found",
		"message has been recalled",
		"message recalled",
		"message has been deleted",
		"message deleted",
		"目标消息不存在",
		"消息不存在",
		"消息已撤回",
		"消息已删除",
	}
	for _, hint := range missingHints {
		if strings.Contains(msg, strings.ToLower(hint)) {
			return true
		}
	}
	return false
}

func buildCard(title, body, themeKey string, extraElements []map[string]any) map[string]any {
	elements := make([]map[string]any, 0, len(extraElements)+1)
	if strings.TrimSpace(body) != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": body,
		})
	}
	elements = append(elements, extraElements...)
	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"enable_forward":   true,
		},
		"header": map[string]any{
			"template": cardTemplate(themeKey, title),
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
		},
		"elements": elements,
	}
}

func cardTemplate(themeKey, fallback string) string {
	key := strings.ToLower(strings.TrimSpace(themeKey))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(fallback))
	}
	switch {
	case key == cardThemeFinal:
		return "blue"
	case key == cardThemeSuccess, key == cardThemeApproval:
		return "green"
	case key == cardThemeError || strings.Contains(key, "error") || strings.Contains(key, "fail") || strings.Contains(key, "reject"):
		return "red"
	default:
		return "grey"
	}
}

func (g *LiveGateway) recordSurfaceMessage(messageID, surfaceSessionID string) {
	if messageID == "" || surfaceSessionID == "" {
		return
	}
	g.mu.Lock()
	g.messages[messageID] = surfaceSessionID
	g.mu.Unlock()
}
