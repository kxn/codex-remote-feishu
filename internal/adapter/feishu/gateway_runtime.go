package feishu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkimv2 "github.com/larksuite/oapi-sdk-go/v3/service/im/v2"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (g *LiveGateway) Start(ctx context.Context, handler ActionHandler) error {
	dispatch := dispatcher.NewEventDispatcher("", "")
	dispatch.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		action, ok, err := g.parseMessageEvent(ctx, event)
		if err != nil || !ok {
			return err
		}
		return handleGatewayEventAction(ctx, action, handler)
	})
	dispatch.OnP2MessageRecalledV1(func(ctx context.Context, event *larkim.P2MessageRecalledV1) error {
		action, ok := g.parseMessageRecalledEvent(event)
		if !ok {
			return nil
		}
		return handleGatewayEventAction(ctx, action, handler)
	})
	dispatch.OnP2MessageReactionCreatedV1(func(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
		action, ok := g.parseMessageReactionCreatedEvent(event)
		if !ok {
			return nil
		}
		return handleGatewayEventAction(ctx, action, handler)
	})
	dispatch.OnP2CardActionTrigger(func(ctx context.Context, event *larkcallback.CardActionTriggerEvent) (*larkcallback.CardActionTriggerResponse, error) {
		action, ok := g.parseCardActionTriggerEvent(event)
		if ok {
			return handleCardActionTrigger(ctx, action, handler)
		}
		return &larkcallback.CardActionTriggerResponse{}, nil
	})
	dispatch.OnP2BotMenuV6(func(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
		action, ok := g.parseMenuEvent(event)
		if !ok {
			return nil
		}
		return handleGatewayEventAction(ctx, action, handler)
	})
	return newGatewayWSRunner(g.config, dispatch, g.emitState).Run(ctx)
}

func handleGatewayEventAction(ctx context.Context, action control.Action, handler ActionHandler) error {
	if shouldAcknowledgeGatewayActionImmediately(action) {
		go handler(context.Background(), action)
		return nil
	}
	handler(ctx, action)
	return nil
}

func handleCardActionTrigger(ctx context.Context, action control.Action, handler ActionHandler) (*larkcallback.CardActionTriggerResponse, error) {
	if shouldAcknowledgeCardActionImmediately(action) {
		go handler(context.Background(), action)
		return &larkcallback.CardActionTriggerResponse{}, nil
	}
	if result := handler(ctx, action); result != nil {
		if response := callbackCardResponse(result); response != nil {
			return response, nil
		}
	}
	return &larkcallback.CardActionTriggerResponse{}, nil
}

func shouldAcknowledgeGatewayActionImmediately(action control.Action) bool {
	switch action.Kind {
	case control.ActionTextMessage,
		control.ActionImageMessage,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return false
	default:
		// Command, menu, button and request-control actions do not currently rely
		// on a synchronous callback payload. Ack them immediately so long-running
		// control flows do not get redelivered by Feishu.
		return true
	}
}

func shouldAcknowledgeCardActionImmediately(action control.Action) bool {
	if action.Inbound != nil &&
		strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != "" &&
		control.SupportsInlineCardReplacement(action) {
		return false
	}
	return shouldAcknowledgeGatewayActionImmediately(action)
}

func callbackCardResponse(result *ActionResult) *larkcallback.CardActionTriggerResponse {
	if result == nil || result.ReplaceCurrentCard == nil {
		return nil
	}
	card := result.ReplaceCurrentCard
	if card.Kind != OperationSendCard {
		return nil
	}
	return &larkcallback.CardActionTriggerResponse{
		Card: &larkcallback.Card{
			// New-style card.action.trigger callback responses use `raw` for JSON cards.
			Type: "raw",
			Data: renderOperationCard(*card, cardEnvelopeV2),
		},
	}
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
		card, err := json.Marshal(renderOperationCard(operation, operation.ordinaryCardEnvelope()))
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
	case OperationSendImage:
		receiveID, receiveIDType := operation.ReceiveID, operation.ReceiveIDType
		if receiveID == "" || receiveIDType == "" {
			receiveID, receiveIDType = ResolveReceiveTarget(operation.ChatID, "")
		}
		if receiveID == "" || receiveIDType == "" {
			return fmt.Errorf("send image failed: missing receive target")
		}
		imageKey, err := g.uploadOperationImage(ctx, operation)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(map[string]string{"image_key": imageKey})
		if strings.TrimSpace(operation.ReplyToMessageID) != "" {
			resp, err := g.replyMessageFn(ctx, operation.ReplyToMessageID, "image", string(body))
			if err == nil && resp != nil && resp.Success() {
				if resp.Data != nil {
					g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
				}
				return nil
			}
			log.Printf(
				"feishu image reply fallback: surface=%s reply_to=%s err=%v code=%d msg=%s",
				operation.SurfaceSessionID,
				operation.ReplyToMessageID,
				err,
				replyRespCode(resp),
				replyRespMsg(resp),
			)
		}
		resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "image", string(body))
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("send image failed: code=%d msg=%s", resp.Code, resp.Msg)
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
	case OperationSetTimeSensitive:
		userID := strings.TrimSpace(operation.ReceiveID)
		userIDType := strings.TrimSpace(operation.ReceiveIDType)
		if userID == "" || userIDType == "" {
			return fmt.Errorf("set time sensitive failed: missing user target")
		}
		resp, err := g.botTimeSensitiveFn(ctx, userIDType, operation.TimeSensitive, []string{userID})
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("set time sensitive failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		if resp.Data != nil && len(resp.Data.FailedUserReasons) != 0 {
			reason := resp.Data.FailedUserReasons[0]
			code := 0
			if reason.ErrorCode != nil {
				code = *reason.ErrorCode
			}
			return fmt.Errorf(
				"set time sensitive failed: user=%s code=%d msg=%s",
				strings.TrimSpace(stringPtr(reason.UserId)),
				code,
				strings.TrimSpace(stringPtr(reason.ErrorMessage)),
			)
		}
		return nil
	default:
		return nil
	}
}

func (g *LiveGateway) botTimeSensitive(ctx context.Context, userIDType string, timeSensitive bool, userIDs []string) (*larkimv2.BotTimeSentiveFeedCardResp, error) {
	return g.client.Im.V2.FeedCard.BotTimeSentive(
		ctx,
		larkimv2.NewBotTimeSentiveFeedCardReqBuilder().
			UserIdType(userIDType).
			Body(
				larkimv2.NewBotTimeSentiveFeedCardReqBodyBuilder().
					TimeSensitive(timeSensitive).
					UserIds(userIDs).
					Build(),
			).
			Build(),
	)
}

func (g *LiveGateway) uploadOperationImage(ctx context.Context, operation Operation) (string, error) {
	path := strings.TrimSpace(operation.ImagePath)
	base64Data := strings.TrimSpace(operation.ImageBase64)
	if path != "" {
		imageKey, err := g.uploadImagePathFn(ctx, path)
		if err == nil {
			return imageKey, nil
		}
		if base64Data == "" {
			return "", fmt.Errorf("upload image from saved path %q failed: %w", path, err)
		}
		log.Printf("feishu image upload path fallback: surface=%s path=%s err=%v", operation.SurfaceSessionID, path, err)
		imageKey, fallbackErr := g.uploadImageBase64(ctx, base64Data)
		if fallbackErr == nil {
			return imageKey, nil
		}
		return "", fmt.Errorf("upload image failed: saved path %q: %v; base64 fallback: %w", path, err, fallbackErr)
	}
	if base64Data != "" {
		return g.uploadImageBase64(ctx, base64Data)
	}
	return "", fmt.Errorf("upload image failed: missing image payload")
}

func (g *LiveGateway) uploadImageBase64(ctx context.Context, value string) (string, error) {
	data, err := decodeBase64Image(value)
	if err != nil {
		return "", fmt.Errorf("decode image base64 failed: %w", err)
	}
	return g.uploadImageBytesFn(ctx, data)
}

func (g *LiveGateway) uploadImagePath(ctx context.Context, path string) (string, error) {
	body, err := larkim.NewCreateImagePathReqBodyBuilder().
		ImageType("message").
		ImagePath(path).
		Build()
	if err != nil {
		return "", err
	}
	resp, err := g.client.Im.V1.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(body).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("upload image failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || strings.TrimSpace(stringPtr(resp.Data.ImageKey)) == "" {
		return "", fmt.Errorf("upload image failed: missing image key")
	}
	return strings.TrimSpace(stringPtr(resp.Data.ImageKey)), nil
}

func (g *LiveGateway) uploadImageBytes(ctx context.Context, data []byte) (string, error) {
	resp, err := g.client.Im.V1.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(bytes.NewReader(data)).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("upload image failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || strings.TrimSpace(stringPtr(resp.Data.ImageKey)) == "" {
		return "", fmt.Errorf("upload image failed: missing image key")
	}
	return strings.TrimSpace(stringPtr(resp.Data.ImageKey)), nil
}

func decodeBase64Image(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("empty image payload")
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		if comma := strings.Index(trimmed, ","); comma >= 0 {
			trimmed = trimmed[comma+1:]
		}
	}
	var builder strings.Builder
	builder.Grow(len(trimmed))
	for _, r := range trimmed {
		switch r {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			builder.WriteRune(r)
		}
	}
	compact := builder.String()
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, decoder := range decoders {
		data, err := decoder.DecodeString(compact)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 image payload")
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
	if strings.Contains(msg, "message") {
		if strings.Contains(msg, "not found") || strings.Contains(msg, "recalled") || strings.Contains(msg, "deleted") {
			return true
		}
	}
	missingHints := []string{
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
