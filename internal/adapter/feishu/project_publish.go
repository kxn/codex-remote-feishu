package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ProjectPublishRequest struct {
	ChatID           string
	ReplyToMessageID string
	Text             string
	UUID             string
}
type ProjectPublishReceipt struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
	RootID    string `json:"root_id,omitempty"`
	ParentID  string `json:"parent_id,omitempty"`
}

// PublishText preserves the SDK's one-hour UUID deduplication, not permanent
// exactly-once delivery. The caller owns persistence, retry windows and unknown
// reconciliation. This adapter never retries a write or assumes a human read it.
func (a *ProjectChatAPI) PublishText(ctx context.Context, req ProjectPublishRequest) (ProjectPublishReceipt, error) {
	var receipt ProjectPublishReceipt
	if strings.TrimSpace(req.ChatID) == "" || strings.TrimSpace(req.Text) == "" || strings.TrimSpace(req.UUID) == "" {
		return receipt, fmt.Errorf("chat, text and persistent UUID are required")
	}
	if len(req.Text) > 24*1024 || len(req.UUID) > 50 {
		return receipt, fmt.Errorf("publication exceeds text or UUID limit")
	}
	content, err := json.Marshal(map[string]string{"text": req.Text})
	if err != nil {
		return receipt, err
	}
	return a.publishContent(ctx, req, "text", content)
}

type ProjectCardPublishRequest struct {
	ChatID           string
	ReplyToMessageID string
	Card             json.RawMessage
	UUID             string
}

func (a *ProjectChatAPI) PublishCard(ctx context.Context, req ProjectCardPublishRequest) (ProjectPublishReceipt, error) {
	if req.ChatID == "" || req.UUID == "" || len(req.UUID) > 50 || len(req.Card) == 0 || len(req.Card) > 28*1024 || !json.Valid(req.Card) {
		return ProjectPublishReceipt{}, fmt.Errorf("invalid card publication")
	}
	return a.publishContent(ctx, ProjectPublishRequest{ChatID: req.ChatID, ReplyToMessageID: req.ReplyToMessageID, UUID: req.UUID}, "interactive", req.Card)
}

func (a *ProjectChatAPI) publishContent(ctx context.Context, req ProjectPublishRequest, kind string, content []byte) (ProjectPublishReceipt, error) {
	var receipt ProjectPublishReceipt
	if req.ReplyToMessageID != "" {
		const api = "im.v1.message.reply"
		resp, e := DoSDK(ctx, a.broker, a.callSpec(api, req.ChatID, false), func(ctx context.Context, client *lark.Client) (*larkim.ReplyMessageResp, error) {
			r, e := client.Im.V1.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().MessageId(req.ReplyToMessageID).Body(larkim.NewReplyMessageReqBodyBuilder().MsgType(kind).Content(string(content)).ReplyInThread(true).Uuid(req.UUID).Build()).Build())
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
		if resp != nil && resp.Data != nil {
			receipt = ProjectPublishReceipt{MessageID: scopeStringValue(resp.Data.MessageId), ChatID: scopeStringValue(resp.Data.ChatId), RootID: scopeStringValue(resp.Data.RootId), ParentID: scopeStringValue(resp.Data.ParentId)}
		}
		if e != nil {
			return receipt, e
		}
	} else {
		const api = "im.v1.message.create"
		resp, e := DoSDK(ctx, a.broker, a.callSpec(api, req.ChatID, false), func(ctx context.Context, client *lark.Client) (*larkim.CreateMessageResp, error) {
			r, e := client.Im.V1.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().ReceiveIdType("chat_id").Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(req.ChatID).MsgType(kind).Content(string(content)).Uuid(req.UUID).Build()).Build())
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
		if resp != nil && resp.Data != nil {
			receipt = ProjectPublishReceipt{MessageID: scopeStringValue(resp.Data.MessageId), ChatID: scopeStringValue(resp.Data.ChatId), RootID: scopeStringValue(resp.Data.RootId), ParentID: scopeStringValue(resp.Data.ParentId)}
		}
		if e != nil {
			return receipt, e
		}
	}
	if receipt.MessageID == "" || receipt.ChatID != req.ChatID {
		return receipt, fmt.Errorf("publication response lacks the expected message/chat receipt")
	}
	return receipt, nil
}
