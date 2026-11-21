package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type ProjectCardPatchRequest struct {
	ChatID, MessageID string
	Card              json.RawMessage
}

// PatchCard updates a previously authenticated publication. Unlike create,
// patch has no UUID deduplication and must never be replayed after uncertainty.
func (a *ProjectChatAPI) PatchCard(ctx context.Context, req ProjectCardPatchRequest) (ProjectPublishReceipt, error) {
	if req.ChatID == "" || req.MessageID == "" || len(req.Card) == 0 || len(req.Card) > 28*1024 || !json.Valid(req.Card) {
		return ProjectPublishReceipt{}, fmt.Errorf("invalid card update")
	}
	const api = "im.v1.message.patch"
	spec := a.callSpec(api, req.ChatID, false)
	spec.ResourceKey.MessageID = req.MessageID
	_, err := DoSDK(ctx, a.broker, spec, func(ctx context.Context, client *lark.Client) (*larkim.PatchMessageResp, error) {
		response, e := client.Im.V1.Message.Patch(ctx, larkim.NewPatchMessageReqBuilder().MessageId(req.MessageID).Body(larkim.NewPatchMessageReqBodyBuilder().Content(string(req.Card)).Build()).Build())
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
		return ProjectPublishReceipt{}, err
	}
	return ProjectPublishReceipt{ChatID: req.ChatID, MessageID: req.MessageID}, nil
}
