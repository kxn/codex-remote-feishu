package feishu

import (
	"context"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

func (a *ProjectDocumentAPI) GetDocumentBlock(ctx context.Context, documentID ProjectDocumentID, blockID string, revision int) (*larkdocx.Block, error) {
	if err := projectBareToken(string(documentID), "document ID"); err != nil {
		return nil, err
	}
	if err := projectBareToken(blockID, "block ID"); err != nil {
		return nil, err
	}
	if revision < 1 {
		return nil, fmt.Errorf("explicit observed document revision required")
	}
	const api = "docx.v1.document.block.get"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(documentID), true), func(callCtx context.Context, client *lark.Client) (*larkdocx.GetDocumentBlockResp, error) {
		response, e := client.Docx.V1.DocumentBlock.Get(callCtx, larkdocx.NewGetDocumentBlockReqBuilder().DocumentId(string(documentID)).BlockId(blockID).DocumentRevisionId(revision).Build())
		if e != nil {
			return response, e
		}
		if response == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !response.Success() {
			return response, newAPIError(api, response.ApiResp, response.CodeError)
		}
		if response.Data == nil || response.Data.Block == nil {
			return response, fmt.Errorf("%s returned no block", api)
		}
		return response, nil
	})
	if err != nil {
		return nil, err
	}
	if scopeStringValue(resp.Data.Block.BlockId) != blockID {
		return nil, fmt.Errorf("SDK returned a different block identity")
	}
	return resp.Data.Block, nil
}
