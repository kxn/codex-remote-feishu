package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ProjectMessagePin struct{ MessageID, ChatID, OperatorID, OperatorIDType, CreateTime string }

// PinMessage creates the independent Pin bookmark; it does not replace top notice.
func (a *ProjectChatAPI) PinMessage(ctx context.Context, messageID string) (ProjectMessagePin, error) {
	if strings.TrimSpace(messageID) == "" {
		return ProjectMessagePin{}, fmt.Errorf("missing message id")
	}
	const api = "im.v1.pin.create"
	spec := a.callSpec(api, "", false)
	spec.ResourceKey.MessageID = messageID
	resp, err := DoSDK(ctx, a.broker, spec, func(ctx context.Context, c *lark.Client) (*larkim.CreatePinResp, error) {
		resp, err := c.Im.V1.Pin.Create(ctx, larkim.NewCreatePinReqBuilder().Body(&larkim.CreatePinReqBody{MessageId: &messageID}).Build())
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return nil, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil || resp.Data.Pin == nil {
			return nil, fmt.Errorf("%s returned missing pin", api)
		}
		return resp, nil
	})
	if err != nil {
		return ProjectMessagePin{}, err
	}
	pin := resp.Data.Pin
	return ProjectMessagePin{MessageID: scopeStringValue(pin.MessageId), ChatID: scopeStringValue(pin.ChatId), OperatorID: scopeStringValue(pin.OperatorId), OperatorIDType: scopeStringValue(pin.OperatorIdType), CreateTime: scopeStringValue(pin.CreateTime)}, nil
}

// ProjectTopNoticeRequest is an explicit replacement request. The platform does
// not expose current notice ownership here: the service must require a human
// instruction or an existing management agreement before setting ExplicitlyRequested.
// Type is message or announcement; only message uses MessageID.
type ProjectTopNoticeRequest struct {
	ChatID              string
	Type                string
	MessageID           string
	ExplicitlyRequested bool
}

func (a *ProjectChatAPI) PutTopNotice(ctx context.Context, req ProjectTopNoticeRequest) error {
	if err := projectChatID(req.ChatID); err != nil {
		return err
	}
	if !req.ExplicitlyRequested {
		return fmt.Errorf("top notice replacement requires explicit request")
	}
	notice := &larkim.ChatTopNotice{}
	switch req.Type {
	case "message":
		if strings.TrimSpace(req.MessageID) == "" {
			return fmt.Errorf("missing message id")
		}
		action := "1"
		notice.ActionType = &action
		notice.MessageId = &req.MessageID
	case "announcement":
		if req.MessageID != "" {
			return fmt.Errorf("announcement top notice must not supply message id")
		}
		action := "2"
		notice.ActionType = &action
	default:
		return fmt.Errorf("unsupported top notice type %q", req.Type)
	}
	const api = "im.v1.chat.top_notice.put_top_notice"
	_, err := DoSDK(ctx, a.broker, a.callSpec(api, req.ChatID, false), func(ctx context.Context, c *lark.Client) (*larkim.PutTopNoticeChatTopNoticeResp, error) {
		resp, err := c.Im.V1.ChatTopNotice.PutTopNotice(ctx, larkim.NewPutTopNoticeChatTopNoticeReqBuilder().ChatId(req.ChatID).Body(&larkim.PutTopNoticeChatTopNoticeReqBody{ChatTopNotice: []*larkim.ChatTopNotice{notice}}).Build())
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return nil, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	return err
}
