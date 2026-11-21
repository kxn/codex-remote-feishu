package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type PrivateEntryPublishRequest struct{ OpenID, Text, UUID string }

// PublishPrivateEntry addresses an actual SDK menu operator by app-scoped OpenID.
// The chat ID is learned only from the platform receipt. The caller must persist
// the intent and reconcile unknown results; UUID is not permanent deduplication.
func (a *ProjectChatAPI) PublishPrivateEntry(ctx context.Context, req PrivateEntryPublishRequest) (ProjectPublishReceipt, error) {
	var receipt ProjectPublishReceipt
	if strings.TrimSpace(req.OpenID) == "" || strings.TrimSpace(req.Text) == "" || req.UUID == "" || len(req.UUID) > 50 || len(req.Text) > 24*1024 {
		return receipt, fmt.Errorf("actual open_id, bounded text and persistent UUID required")
	}
	content, err := json.Marshal(map[string]string{"text": req.Text})
	if err != nil {
		return receipt, err
	}
	const api = "im.v1.message.create"
	spec := a.callSpec(api, "", false)
	spec.ResourceKey = FeishuResourceKey{}
	resp, err := DoSDK(ctx, a.broker, spec, func(callCtx context.Context, client *lark.Client) (*larkim.CreateMessageResp, error) {
		response, e := client.Im.V1.Message.Create(callCtx, larkim.NewCreateMessageReqBuilder().ReceiveIdType("open_id").Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(req.OpenID).MsgType("text").Content(string(content)).Uuid(req.UUID).Build()).Build())
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
	if resp != nil && resp.Data != nil {
		receipt = ProjectPublishReceipt{MessageID: scopeStringValue(resp.Data.MessageId), ChatID: scopeStringValue(resp.Data.ChatId), RootID: scopeStringValue(resp.Data.RootId), ParentID: scopeStringValue(resp.Data.ParentId)}
	}
	if err != nil {
		return receipt, err
	}
	if receipt.MessageID == "" || receipt.ChatID == "" {
		return receipt, fmt.Errorf("private entry lacks actual message/chat receipt")
	}
	return receipt, nil
}
