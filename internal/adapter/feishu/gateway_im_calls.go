package feishu

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtheme"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkimv2 "github.com/larksuite/oapi-sdk-go/v3/service/im/v2"
)

func (g *LiveGateway) botTimeSensitive(ctx context.Context, userIDType string, timeSensitive bool, userIDs []string) (*larkimv2.BotTimeSentiveFeedCardResp, error) {
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v2.feed_card.bot_time_sensitive",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			ReceiveTarget: joinReceiveTarget(userIDType, strings.Join(userIDs, ",")),
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkimv2.BotTimeSentiveFeedCardResp, error) {
		resp, err := client.Im.V2.FeedCard.BotTimeSentive(
			callCtx,
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
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v2.feed_card.bot_time_sensitive", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID:  g.config.GatewayID,
		API:        "im.v1.image.create",
		Class:      CallClassIMSend,
		Priority:   CallPriorityInteractive,
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.CreateImageResp, error) {
		resp, err := client.Im.V1.Image.Create(callCtx, larkim.NewCreateImageReqBuilder().
			Body(body).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.image.create", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return "", err
	}
	if resp.Data == nil || strings.TrimSpace(stringPtr(resp.Data.ImageKey)) == "" {
		return "", fmt.Errorf("upload image failed: missing image key")
	}
	return strings.TrimSpace(stringPtr(resp.Data.ImageKey)), nil
}

func (g *LiveGateway) uploadImageBytes(ctx context.Context, data []byte) (string, error) {
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID:  g.config.GatewayID,
		API:        "im.v1.image.create",
		Class:      CallClassIMSend,
		Priority:   CallPriorityInteractive,
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.CreateImageResp, error) {
		resp, err := client.Im.V1.Image.Create(callCtx, larkim.NewCreateImageReqBuilder().
			Body(larkim.NewCreateImageReqBodyBuilder().
				ImageType("message").
				Image(bytes.NewReader(data)).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.image.create", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return "", err
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
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message.create",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			ReceiveTarget: joinReceiveTarget(receiveIDType, receiveID),
		},
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.CreateMessageResp, error) {
		resp, err := client.Im.V1.Message.Create(callCtx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(receiveIDType).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(receiveID).
				MsgType(msgType).
				Content(content).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message.create", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
}

func (g *LiveGateway) replyMessage(ctx context.Context, messageID, msgType, content string) (*larkim.ReplyMessageResp, error) {
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message.reply",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
		},
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.ReplyMessageResp, error) {
		resp, err := client.Im.V1.Message.Reply(callCtx, larkim.NewReplyMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(msgType).
				Content(content).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message.reply", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
}

func (g *LiveGateway) patchMessage(ctx context.Context, messageID, content string) (*larkim.PatchMessageResp, error) {
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message.patch",
		Class:     CallClassIMPatch,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
		},
		Retry:      RetrySafe,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.PatchMessageResp, error) {
		resp, err := client.Im.V1.Message.Patch(callCtx, larkim.NewPatchMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewPatchMessageReqBodyBuilder().
				Content(content).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message.patch", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
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

func ignoredMissingReactionCreateError(_ int, msg string) bool {
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

func ignoredMissingReactionDeleteError(code int, msg string) bool {
	if ignoredMissingReactionCreateError(code, msg) {
		return true
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "reaction") {
		if strings.Contains(msg, "not found") || strings.Contains(msg, "deleted") {
			return true
		}
	}
	missingHints := []string{
		"表情不存在",
		"表情已删除",
	}
	for _, hint := range missingHints {
		if strings.Contains(msg, strings.ToLower(hint)) {
			return true
		}
	}
	return false
}

func ignoredMissingMessageDeleteError(_ int, msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "not found") || strings.Contains(msg, "recalled") || strings.Contains(msg, "deleted") {
		return true
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

func cardTemplate(themeKey, fallback string) string { return cardtheme.Template(themeKey, fallback) }

func (g *LiveGateway) recordSurfaceMessage(messageID, surfaceSessionID string) {
	if messageID == "" || surfaceSessionID == "" {
		return
	}
	g.mu.Lock()
	g.messages[messageID] = surfaceSessionID
	g.mu.Unlock()
}

func (g *LiveGateway) deleteMessage(ctx context.Context, messageID string) (*larkim.DeleteMessageResp, error) {
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message.delete",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.DeleteMessageResp, error) {
		resp, err := client.Im.V1.Message.Delete(callCtx, larkim.NewDeleteMessageReqBuilder().
			MessageId(messageID).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message.delete", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
}

func (g *LiveGateway) createReaction(ctx context.Context, messageID, emojiType string) (*larkim.CreateMessageReactionResp, error) {
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message_reaction.create",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.CreateMessageReactionResp, error) {
		resp, err := client.Im.V1.MessageReaction.Create(callCtx, larkim.NewCreateMessageReactionReqBuilder().
			MessageId(messageID).
			Body(larkim.NewCreateMessageReactionReqBodyBuilder().
				ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message_reaction.create", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
}

func (g *LiveGateway) deleteReaction(ctx context.Context, messageID, reactionID string) (*larkim.DeleteMessageReactionResp, error) {
	return DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "im.v1.message_reaction.delete",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			MessageID: messageID,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.DeleteMessageReactionResp, error) {
		resp, err := client.Im.V1.MessageReaction.Delete(callCtx, larkim.NewDeleteMessageReactionReqBuilder().
			MessageId(messageID).
			ReactionId(reactionID).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.message_reaction.delete", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
}

func joinReceiveTarget(receiveIDType, receiveID string) string {
	receiveIDType = strings.TrimSpace(receiveIDType)
	receiveID = strings.TrimSpace(receiveID)
	if receiveIDType == "" || receiveID == "" {
		return ""
	}
	return receiveIDType + ":" + receiveID
}
