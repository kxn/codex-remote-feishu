package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// UpdateNavigationText edits one previously recorded bot-owned text message.
// It exposes no retry/dedup claim; callers must read this exact ID on uncertainty.
func (a *ProjectChatAPI) UpdateNavigationText(ctx context.Context, messageID, text string) error {
	if err := projectBareToken(messageID, "navigation message id"); err != nil {
		return err
	}
	if text == "" || len(text) > 30000 {
		return fmt.Errorf("bounded navigation text required")
	}
	raw, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	content := string(raw)
	const api = "im.v1.message.update"
	spec := a.callSpec(api, "", false)
	spec.ResourceKey = FeishuResourceKey{MessageID: messageID}
	_, err = DoSDK(ctx, a.broker, spec, func(ctx context.Context, c *lark.Client) (*larkim.UpdateMessageResp, error) {
		resp, err := c.Im.V1.Message.Update(ctx, larkim.NewUpdateMessageReqBuilder().MessageId(messageID).Body(&larkim.UpdateMessageReqBody{MsgType: projectString("text"), Content: &content}).Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("navigation update returned nil")
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil || scopeStringValue(resp.Data.MessageId) != messageID {
			return resp, fmt.Errorf("navigation update returned another message identity")
		}
		return resp, nil
	})
	return err
}
