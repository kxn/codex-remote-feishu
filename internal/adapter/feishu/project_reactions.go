package feishu

import (
	"context"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ProjectReaction struct{ ID, Emoji, OperatorID, OperatorType string }

func (a *ProjectChatAPI) CreateMessageReaction(ctx context.Context, messageID, emoji string) (string, error) {
	if messageID == "" || emoji == "" {
		return "", fmt.Errorf("reaction requires exact message and emoji")
	}
	const api = "im.v1.message_reaction.create"
	spec := a.callSpec(api, "", false)
	spec.ResourceKey.MessageID = messageID
	resp, err := DoSDK(ctx, a.broker, spec, func(ctx context.Context, c *lark.Client) (*larkim.CreateMessageReactionResp, error) {
		r, e := c.Im.V1.MessageReaction.Create(ctx, larkim.NewCreateMessageReactionReqBuilder().MessageId(messageID).Body(larkim.NewCreateMessageReactionReqBodyBuilder().ReactionType(larkim.NewEmojiBuilder().EmojiType(emoji).Build()).Build()).Build())
		if e != nil {
			return r, e
		}
		if r == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !r.Success() {
			return r, newAPIError(api, r.ApiResp, r.CodeError)
		}
		return r, nil
	})
	if err != nil {
		return "", err
	}
	if resp.Data == nil || resp.Data.ReactionId == nil || *resp.Data.ReactionId == "" {
		return "", fmt.Errorf("reaction creation lacks receipt")
	}
	return *resp.Data.ReactionId, nil
}
func (a *ProjectChatAPI) DeleteMessageReaction(ctx context.Context, messageID, reactionID string) error {
	if messageID == "" || reactionID == "" {
		return fmt.Errorf("reaction deletion requires exact receipt")
	}
	const api = "im.v1.message_reaction.delete"
	spec := a.callSpec(api, "", false)
	spec.ResourceKey.MessageID = messageID
	_, err := DoSDK(ctx, a.broker, spec, func(ctx context.Context, c *lark.Client) (*larkim.DeleteMessageReactionResp, error) {
		r, e := c.Im.V1.MessageReaction.Delete(ctx, larkim.NewDeleteMessageReactionReqBuilder().MessageId(messageID).ReactionId(reactionID).Build())
		if e != nil {
			return r, e
		}
		if r == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !r.Success() && !ignoredMissingReactionDeleteError(r.Code, r.Msg) {
			return r, newAPIError(api, r.ApiResp, r.CodeError)
		}
		return r, nil
	})
	return err
}

// ListMessageReactions includes operator identity. Recovery must match both
// operator_type=app and the frozen app_id; a human's equal emoji is not ours.
func (a *ProjectChatAPI) ListMessageReactions(ctx context.Context, messageID, emoji string) ([]ProjectReaction, error) {
	if messageID == "" || emoji == "" {
		return nil, fmt.Errorf("reaction lookup requires exact message and emoji")
	}
	const api = "im.v1.message_reaction.list"
	spec := a.callSpec(api, "", true)
	spec.ResourceKey.MessageID = messageID
	var result []ProjectReaction
	token := ""
	seen := map[string]bool{}
	for {
		resp, err := DoSDK(ctx, a.broker, spec, func(ctx context.Context, c *lark.Client) (*larkim.ListMessageReactionResp, error) {
			r, e := c.Im.V1.MessageReaction.List(ctx, larkim.NewListMessageReactionReqBuilder().MessageId(messageID).ReactionType(emoji).PageSize(50).PageToken(token).Build())
			if e != nil {
				return r, e
			}
			if r == nil {
				return nil, fmt.Errorf("%s returned nil", api)
			}
			if !r.Success() {
				return r, newAPIError(api, r.ApiResp, r.CodeError)
			}
			return r, nil
		})
		if err != nil {
			return nil, err
		}
		if resp.Data == nil {
			return nil, fmt.Errorf("reaction lookup lacks data")
		}
		for _, item := range resp.Data.Items {
			if item == nil || item.ReactionId == nil || item.ReactionType == nil || item.ReactionType.EmojiType == nil || item.Operator == nil || item.Operator.OperatorId == nil || item.Operator.OperatorType == nil {
				continue
			}
			result = append(result, ProjectReaction{ID: *item.ReactionId, Emoji: *item.ReactionType.EmojiType, OperatorID: *item.Operator.OperatorId, OperatorType: *item.Operator.OperatorType})
		}
		if resp.Data.HasMore == nil || !*resp.Data.HasMore {
			return result, nil
		}
		if resp.Data.PageToken == nil || *resp.Data.PageToken == "" || seen[*resp.Data.PageToken] {
			return nil, fmt.Errorf("reaction lookup pagination did not advance")
		}
		token = *resp.Data.PageToken
		seen[token] = true
		if len(seen) > 100 {
			return nil, fmt.Errorf("reaction lookup exceeds bounded page scan")
		}
	}
}
