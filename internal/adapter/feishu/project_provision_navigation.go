package feishu

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ProjectNavigationMessage struct{ MessageID, ChatID, Text string }

func (a *ProjectChatAPI) ReadProjectNavigation(ctx context.Context, messageID string) (ProjectNavigationMessage, error) {
	const api = "im.v1.message.get"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, "", true), func(ctx context.Context, c *lark.Client) (*larkim.GetMessageResp, error) {
		r, e := c.Im.V1.Message.Get(ctx, larkim.NewGetMessageReqBuilder().MessageId(messageID).Build())
		if e != nil {
			return r, e
		}
		if r == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !r.Success() {
			return r, newAPIError(api, r.ApiResp, r.CodeError)
		}
		if r.Data == nil {
			return r, fmt.Errorf("%s returned nil data", api)
		}
		return r, nil
	})
	if err != nil {
		return ProjectNavigationMessage{}, err
	}
	for _, item := range resp.Data.Items {
		if item == nil || scopeStringValue(item.MessageId) != messageID {
			continue
		}
		if item.Deleted != nil && *item.Deleted {
			return ProjectNavigationMessage{}, fmt.Errorf("navigation message was removed")
		}
		if scopeStringValue(item.MsgType) != "text" || item.Body == nil {
			return ProjectNavigationMessage{}, fmt.Errorf("navigation message is no longer plain text")
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(scopeStringValue(item.Body.Content)), &body); err != nil {
			return ProjectNavigationMessage{}, err
		}
		return ProjectNavigationMessage{MessageID: messageID, ChatID: scopeStringValue(item.ChatId), Text: body.Text}, nil
	}
	return ProjectNavigationMessage{}, fmt.Errorf("platform did not return the exact navigation message")
}
func (a *ProjectChatAPI) HasProjectPin(ctx context.Context, chatID, messageID string) (bool, error) {
	const api = "im.v1.pin.list"
	token := ""
	seen := map[string]bool{}
	for {
		resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, true), func(ctx context.Context, c *lark.Client) (*larkim.ListPinResp, error) {
			builder := larkim.NewListPinReqBuilder().ChatId(chatID).PageSize(100)
			if token != "" {
				builder.PageToken(token)
			}
			r, e := c.Im.V1.Pin.List(ctx, builder.Build())
			if e != nil {
				return r, e
			}
			if r == nil {
				return nil, fmt.Errorf("%s returned nil", api)
			}
			if !r.Success() {
				return r, newAPIError(api, r.ApiResp, r.CodeError)
			}
			if r.Data == nil || r.Data.HasMore == nil {
				return r, fmt.Errorf("%s returned missing pagination", api)
			}
			return r, nil
		})
		if err != nil {
			return false, err
		}
		for _, pin := range resp.Data.Items {
			if pin != nil && scopeStringValue(pin.MessageId) == messageID && scopeStringValue(pin.ChatId) == chatID {
				return true, nil
			}
		}
		if !*resp.Data.HasMore {
			return false, nil
		}
		next := scopeStringValue(resp.Data.PageToken)
		if next == "" || seen[next] {
			return false, fmt.Errorf("pin pagination did not advance")
		}
		seen[next] = true
		token = next
	}
}
