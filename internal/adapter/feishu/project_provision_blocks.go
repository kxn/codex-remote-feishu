package feishu

import (
	"context"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

type ProjectDocumentBlockReceipt struct {
	DocumentID          ProjectDocumentID
	Block               *larkdocx.Block
	RequestedRevisionID int
	RequestID           string
}

func (a *ProjectDocumentAPI) GetProjectDocumentBlock(ctx context.Context, id ProjectDocumentID, blockID string, revision int) (ProjectDocumentBlockReceipt, error) {
	result := ProjectDocumentBlockReceipt{DocumentID: id, RequestedRevisionID: revision}
	if err := projectDocumentRevision(id, revision); err != nil {
		return result, err
	}
	if err := projectBareToken(blockID, "block id"); err != nil {
		return result, err
	}
	const api = "docx.v1.document.block.get"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(id), true), func(ctx context.Context, c *lark.Client) (*larkdocx.GetDocumentBlockResp, error) {
		r, e := c.Docx.V1.DocumentBlock.Get(ctx, larkdocx.NewGetDocumentBlockReqBuilder().DocumentId(string(id)).BlockId(blockID).DocumentRevisionId(revision).Build())
		if e != nil {
			return r, e
		}
		if r == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !r.Success() {
			return r, newAPIError(api, r.ApiResp, r.CodeError)
		}
		if r.Data == nil || r.Data.Block == nil {
			return r, fmt.Errorf("%s returned missing block", api)
		}
		return r, nil
	})
	if resp != nil {
		result.RequestID = resp.RequestId()
		if resp.Data != nil {
			result.Block = resp.Data.Block
		}
	}
	return result, err
}
