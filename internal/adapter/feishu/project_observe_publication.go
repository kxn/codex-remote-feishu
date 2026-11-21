package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ObservedPublication struct {
	MessageID    string `json:"message_id"`
	ChatID       string `json:"chat_id"`
	RootID       string `json:"root_id,omitempty"`
	ParentID     string `json:"parent_id,omitempty"`
	MessageType  string `json:"message_type"`
	Content      string `json:"content"`
	SenderID     string `json:"sender_id"`
	SenderIDType string `json:"sender_id_type"`
	SenderType   string `json:"sender_type"`
	Deleted      bool   `json:"deleted"`
	RequestID    string `json:"request_id,omitempty"`
}

// ReadPublishedMessage reads one exact SDK message. No UUID is returned by this
// endpoint: matching text alone must never be treated as an operation receipt.
func (a *ProjectChatAPI) ReadPublishedMessage(ctx context.Context, id string) (ObservedPublication, error) {
	if strings.TrimSpace(id) == "" {
		return ObservedPublication{}, fmt.Errorf("actual message ID required")
	}
	const api = "im.v1.message.get"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, "", true), func(callCtx context.Context, client *lark.Client) (*larkim.GetMessageResp, error) {
		// The default interactive body is a rendering projection without
		// callbacks. Recovery needs the original send-time card JSON.
		request := larkim.NewGetMessageReqBuilder().MessageId(id).UserIdType("open_id").CardMsgContentType("user_card_content").Build()
		response, e := client.Im.V1.Message.Get(callCtx, request)
		if e != nil {
			return response, e
		}
		if response == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !response.Success() {
			return response, newAPIError(api, response.ApiResp, response.CodeError)
		}
		return response, nil
	})
	if err != nil {
		return ObservedPublication{}, err
	}
	if resp.Data == nil {
		return ObservedPublication{}, fmt.Errorf("message not returned")
	}
	for _, message := range resp.Data.Items {
		if message == nil || scopeStringValue(message.MessageId) != id {
			continue
		}
		value := ObservedPublication{MessageID: id, ChatID: scopeStringValue(message.ChatId), RootID: scopeStringValue(message.RootId), ParentID: scopeStringValue(message.ParentId), MessageType: scopeStringValue(message.MsgType), Deleted: message.Deleted != nil && *message.Deleted}
		if message.Body != nil {
			value.Content = scopeStringValue(message.Body.Content)
		}
		if message.Sender != nil {
			value.SenderID = scopeStringValue(message.Sender.Id)
			value.SenderIDType = scopeStringValue(message.Sender.IdType)
			value.SenderType = scopeStringValue(message.Sender.SenderType)
		}
		if resp.ApiResp != nil {
			value.RequestID = resp.ApiResp.Header.Get("X-Tt-Logid")
		}
		return value, nil
	}
	return ObservedPublication{}, fmt.Errorf("exact message was not returned")
}
