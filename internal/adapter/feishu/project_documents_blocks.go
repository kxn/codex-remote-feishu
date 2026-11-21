package feishu

import (
	"context"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

// AppendDocumentBlocks inserts only the explicitly supplied children under one
// parent block. The service owns managed-block IDs, conflict checks and read-back.
// RevisionID is the platform's operation revision, not expected-current CAS.
func (a *ProjectDocumentAPI) AppendDocumentBlocks(ctx context.Context, req ProjectAppendDocumentBlocksRequest) (ProjectDocumentBlocksMutation, error) {
	result := ProjectDocumentBlocksMutation{DocumentID: req.DocumentID}
	if err := projectDocumentRevision(req.DocumentID, req.RevisionID); err != nil {
		return result, err
	}
	if err := projectBareToken(req.ParentBlockID, "parent block id"); err != nil {
		return result, err
	}
	if req.Index < -1 || len(req.Blocks) == 0 {
		return result, fmt.Errorf("explicit nonempty children and index >= -1 required")
	}
	for _, block := range req.Blocks {
		if block == nil {
			return result, fmt.Errorf("nil child block")
		}
	}
	const api = "docx.v1.document.block.children.create"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(req.DocumentID), false), func(ctx context.Context, c *lark.Client) (*larkdocx.CreateDocumentBlockChildrenResp, error) {
		builder := larkdocx.NewCreateDocumentBlockChildrenReqBuilder().DocumentId(string(req.DocumentID)).BlockId(req.ParentBlockID).DocumentRevisionId(req.RevisionID).Body(&larkdocx.CreateDocumentBlockChildrenReqBody{Children: req.Blocks, Index: &req.Index})
		if req.ClientToken != "" {
			builder.ClientToken(req.ClientToken)
		}
		resp, err := c.Docx.V1.DocumentBlockChildren.Create(ctx, builder.Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("%s returned nil data", api)
		}
		return resp, nil
	})
	if resp == nil {
		return result, err
	}
	result.RequestID = resp.RequestId()
	if resp.Data != nil {
		result.Blocks = resp.Data.Children
		result.RevisionID = resp.Data.DocumentRevisionId
		result.ClientToken = scopeStringValue(resp.Data.ClientToken)
	}
	if err != nil {
		return result, err
	}
	if result.RevisionID == nil {
		return result, fmt.Errorf("%s returned missing revision", api)
	}
	return result, nil
}

// UpdateDocumentBlocks changes only specified blocks; duplicate target IDs are
// rejected rather than risking an ambiguous batch. It never replaces a document.
func (a *ProjectDocumentAPI) UpdateDocumentBlocks(ctx context.Context, req ProjectUpdateDocumentBlocksRequest) (ProjectDocumentBlocksMutation, error) {
	result := ProjectDocumentBlocksMutation{DocumentID: req.DocumentID}
	if err := projectDocumentRevision(req.DocumentID, req.RevisionID); err != nil {
		return result, err
	}
	if len(req.Requests) == 0 {
		return result, fmt.Errorf("missing block update requests")
	}
	seen := map[string]bool{}
	for _, update := range req.Requests {
		if update == nil || update.BlockId == nil {
			return result, fmt.Errorf("missing update block id")
		}
		if err := projectBareToken(*update.BlockId, "update block id"); err != nil {
			return result, err
		}
		if seen[*update.BlockId] {
			return result, fmt.Errorf("duplicate update block id %q", *update.BlockId)
		}
		seen[*update.BlockId] = true
	}
	const api = "docx.v1.document.block.batch_update"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(req.DocumentID), false), func(ctx context.Context, c *lark.Client) (*larkdocx.BatchUpdateDocumentBlockResp, error) {
		builder := larkdocx.NewBatchUpdateDocumentBlockReqBuilder().DocumentId(string(req.DocumentID)).DocumentRevisionId(req.RevisionID).Body(&larkdocx.BatchUpdateDocumentBlockReqBody{Requests: req.Requests})
		if req.ClientToken != "" {
			builder.ClientToken(req.ClientToken)
		}
		resp, err := c.Docx.V1.DocumentBlock.BatchUpdate(ctx, builder.Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("%s returned nil data", api)
		}
		return resp, nil
	})
	if resp == nil {
		return result, err
	}
	result.RequestID = resp.RequestId()
	if resp.Data != nil {
		result.Blocks = resp.Data.Blocks
		result.RevisionID = resp.Data.DocumentRevisionId
		result.ClientToken = scopeStringValue(resp.Data.ClientToken)
	}
	if err != nil {
		return result, err
	}
	if result.RevisionID == nil {
		return result, fmt.Errorf("%s returned missing revision", api)
	}
	return result, nil
}
