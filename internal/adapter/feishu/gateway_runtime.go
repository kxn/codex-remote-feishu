package feishu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkimv2 "github.com/larksuite/oapi-sdk-go/v3/service/im/v2"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	maxFeishuCardBytes   = 20000
	oversizedCardMessage = "内容太多了，后面的内容已省略。"
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
	if control.AllowsInlineCardReplacement(action) {
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
			Data: trimCardPayloadForInlineCallback(renderOperationCard(*card, cardEnvelopeV2)),
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
			return newAPIError("im.v1.message.create", resp.ApiResp, resp.CodeError)
		}
		if resp.Data != nil {
			g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
		}
		return nil
	case OperationSendCard:
		card, err := json.Marshal(trimCardPayloadToFit(renderOperationCard(operation, operation.ordinaryCardEnvelope()), maxFeishuCardBytes))
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
			return newAPIError("im.v1.message.create", resp.ApiResp, resp.CodeError)
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
			return newAPIError("im.v1.message.create", resp.ApiResp, resp.CodeError)
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
			return newAPIError("im.v1.message_reaction.create", resp.ApiResp, resp.CodeError)
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
			return newAPIError("im.v1.message_reaction.delete", resp.ApiResp, resp.CodeError)
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
			return newAPIError("im.v2.feed_card.bot_time_sensitive", resp.ApiResp, resp.CodeError)
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

func trimCardPayloadForInlineCallback(payload map[string]any) map[string]any {
	return trimCardPayloadWithMeasure(payload, func(candidate map[string]any) bool {
		response := &larkcallback.CardActionTriggerResponse{
			Card: &larkcallback.Card{
				Type: "raw",
				Data: candidate,
			},
		}
		size, err := jsonSize(response)
		return err == nil && size <= maxFeishuCardBytes
	})
}

func trimCardPayloadToFit(payload map[string]any, maxBytes int) map[string]any {
	return trimCardPayloadWithMeasure(payload, func(candidate map[string]any) bool {
		size, err := jsonSize(candidate)
		return err == nil && size <= maxBytes
	})
}

func trimCardPayloadWithMeasure(payload map[string]any, fits func(map[string]any) bool) map[string]any {
	if len(payload) == 0 || fits == nil || fits(payload) {
		return payload
	}
	elements, path, ok := extractCardPayloadElements(payload)
	if !ok {
		return payload
	}
	blocks := partitionCardPayloadBlocks(elements)
	if len(blocks) == 0 {
		return payload
	}
	for keep := len(blocks) - 1; keep >= 0; keep-- {
		candidate := cloneCardMap(payload)
		trimmed := flattenCardPayloadBlocks(trimTrailingHeaderBlocks(blocks[:keep]))
		trimmed = append(trimmed, map[string]any{
			"tag":     "markdown",
			"content": oversizedCardMessage,
		})
		setCardPayloadElements(candidate, path, trimmed)
		if fits(candidate) {
			return candidate
		}
	}
	return payload
}

type cardPayloadBlock struct {
	Elements []map[string]any
}

func partitionCardPayloadBlocks(elements []map[string]any) []cardPayloadBlock {
	blocks := make([]cardPayloadBlock, 0, len(elements))
	current := make([]map[string]any, 0, 2)
	flush := func() {
		if len(current) == 0 {
			return
		}
		block := cardPayloadBlock{Elements: make([]map[string]any, 0, len(current))}
		for _, element := range current {
			block.Elements = append(block.Elements, cloneCardMap(element))
		}
		blocks = append(blocks, block)
		current = nil
	}
	for _, element := range elements {
		switch {
		case isCardSectionHeaderElement(element):
			flush()
			current = append(current, cloneCardMap(element))
		case startsNewCardPayloadBlock(current, element):
			flush()
			current = append(current, cloneCardMap(element))
		default:
			current = append(current, cloneCardMap(element))
		}
	}
	flush()
	return blocks
}

func startsNewCardPayloadBlock(current []map[string]any, next map[string]any) bool {
	if len(current) == 0 {
		return true
	}
	if isCardSectionHeaderElement(next) {
		return true
	}
	tag := strings.TrimSpace(cardStringValue(next["tag"]))
	if tag == "" {
		return false
	}
	if tag != "markdown" {
		return true
	}
	first := current[0]
	firstTag := strings.TrimSpace(cardStringValue(first["tag"]))
	if firstTag == "" {
		return false
	}
	if firstTag != "markdown" {
		return false
	}
	if isCardSectionHeaderElement(first) {
		return false
	}
	return true
}

func trimTrailingHeaderBlocks(blocks []cardPayloadBlock) []cardPayloadBlock {
	trimmed := blocks
	for len(trimmed) > 0 && isHeaderOnlyCardPayloadBlock(trimmed[len(trimmed)-1]) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed
}

func isHeaderOnlyCardPayloadBlock(block cardPayloadBlock) bool {
	return len(block.Elements) == 1 && isCardSectionHeaderElement(block.Elements[0])
}

func flattenCardPayloadBlocks(blocks []cardPayloadBlock) []map[string]any {
	total := 0
	for _, block := range blocks {
		total += len(block.Elements)
	}
	elements := make([]map[string]any, 0, total)
	for _, block := range blocks {
		for _, element := range block.Elements {
			elements = append(elements, cloneCardMap(element))
		}
	}
	return elements
}

func isCardSectionHeaderElement(element map[string]any) bool {
	if strings.TrimSpace(cardStringValue(element["tag"])) != "markdown" {
		return false
	}
	content := strings.TrimSpace(cardStringValue(element["content"]))
	if content == "" || strings.Contains(content, "\n") {
		return false
	}
	return strings.HasPrefix(content, "**") && strings.HasSuffix(content, "**")
}

func cardStringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

func extractCardPayloadElements(payload map[string]any) ([]map[string]any, string, bool) {
	if body, _ := payload["body"].(map[string]any); len(body) != 0 {
		if elements, ok := cardPayloadElementsSlice(body["elements"]); ok {
			return elements, "body.elements", true
		}
	}
	if elements, ok := cardPayloadElementsSlice(payload["elements"]); ok {
		return elements, "elements", true
	}
	return nil, "", false
}

func cardPayloadElementsSlice(raw any) ([]map[string]any, bool) {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed, true
	case []any:
		elements := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			element, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			elements = append(elements, element)
		}
		return elements, true
	default:
		return nil, false
	}
}

func setCardPayloadElements(payload map[string]any, path string, elements []map[string]any) {
	cloned := make([]map[string]any, 0, len(elements))
	for _, element := range elements {
		cloned = append(cloned, cloneCardMap(element))
	}
	switch path {
	case "body.elements":
		body, _ := payload["body"].(map[string]any)
		if len(body) == 0 {
			body = map[string]any{}
		} else {
			body = cloneCardMap(body)
		}
		body["elements"] = cloned
		payload["body"] = body
	case "elements":
		payload["elements"] = cloned
	}
}

func jsonSize(value any) (int, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return len(data), nil
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
		return "", newAPIError("im.v1.image.create", resp.ApiResp, resp.CodeError)
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
		return "", newAPIError("im.v1.image.create", resp.ApiResp, resp.CodeError)
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

func replyRespError(resp *larkim.ReplyMessageResp) error {
	if resp == nil || resp.Success() {
		return nil
	}
	return newAPIError("im.v1.message.reply", resp.ApiResp, larkcore.CodeError{
		Code: resp.Code,
		Msg:  resp.Msg,
		Err:  resp.CodeError.Err,
	})
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
