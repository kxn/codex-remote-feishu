package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

// CreateDocument makes a stable Docx object. With tenant identity, an explicit
// folder must be application-created, as documented by the SDK; this operation
// neither shares the document nor binds it to a project. It has no client_token.
func (a *ProjectDocumentAPI) CreateDocument(ctx context.Context, req ProjectCreateDocumentRequest) (ProjectDocumentMetadata, error) {
	if strings.TrimSpace(req.Title) == "" {
		return ProjectDocumentMetadata{}, fmt.Errorf("missing document title")
	}
	body := &larkdocx.CreateDocumentReqBody{Title: &req.Title}
	if req.FolderToken != "" {
		if err := projectBareToken(req.FolderToken, "folder token"); err != nil {
			return ProjectDocumentMetadata{}, err
		}
		body.FolderToken = &req.FolderToken
	}
	const api = "docx.v1.document.create"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, req.FolderToken, false), func(ctx context.Context, c *lark.Client) (*larkdocx.CreateDocumentResp, error) {
		resp, err := c.Docx.V1.Document.Create(ctx, larkdocx.NewCreateDocumentReqBuilder().Body(body).Build())
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
		return ProjectDocumentMetadata{}, err
	}
	result := ProjectDocumentMetadata{RequestID: resp.RequestId()}
	if resp.Data == nil {
		return result, err
	}
	metadata, parseErr := projectDocumentMetadata(resp.Data.Document, resp.RequestId())
	if err != nil {
		return metadata, err
	}
	return metadata, parseErr
}

func (a *ProjectDocumentAPI) GetDocument(ctx context.Context, id ProjectDocumentID) (ProjectDocumentMetadata, error) {
	if err := projectBareToken(string(id), "document id"); err != nil {
		return ProjectDocumentMetadata{}, err
	}
	const api = "docx.v1.document.get"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(id), true), func(ctx context.Context, c *lark.Client) (*larkdocx.GetDocumentResp, error) {
		resp, err := c.Docx.V1.Document.Get(ctx, larkdocx.NewGetDocumentReqBuilder().DocumentId(string(id)).Build())
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
		return ProjectDocumentMetadata{}, err
	}
	if resp.Data == nil {
		return ProjectDocumentMetadata{RequestID: resp.RequestId()}, err
	}
	metadata, parseErr := projectDocumentMetadata(resp.Data.Document, resp.RequestId())
	if err != nil {
		return metadata, err
	}
	return metadata, parseErr
}

// ListDocumentBlocksPage reads exactly one platform page. Historical revisions
// require document edit permission. Latest-version pagination is not atomic;
// the service should resolve metadata then request that specific revision.
func (a *ProjectDocumentAPI) ListDocumentBlocksPage(ctx context.Context, req ProjectDocumentBlocksRequest) (ProjectDocumentBlocksPage, error) {
	result := ProjectDocumentBlocksPage{DocumentID: req.DocumentID, RequestedRevisionID: req.RevisionID}
	if err := projectDocumentRevision(req.DocumentID, req.RevisionID); err != nil {
		return result, err
	}
	if req.PageSize < 1 || req.PageSize > 500 {
		return result, fmt.Errorf("block page size must be 1..500")
	}
	const api = "docx.v1.document.block.list"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(req.DocumentID), true), func(ctx context.Context, c *lark.Client) (*larkdocx.ListDocumentBlockResp, error) {
		builder := larkdocx.NewListDocumentBlockReqBuilder().DocumentId(string(req.DocumentID)).DocumentRevisionId(req.RevisionID).PageSize(req.PageSize)
		if req.PageToken != "" {
			builder.PageToken(req.PageToken)
		}
		resp, err := c.Docx.V1.DocumentBlock.List(ctx, builder.Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil || resp.Data.HasMore == nil {
			return resp, fmt.Errorf("%s returned missing pagination data", api)
		}
		return resp, nil
	})
	if resp == nil {
		return result, err
	}
	result.RequestID = resp.RequestId()
	if resp.Data == nil {
		return result, err
	}
	result.Blocks = resp.Data.Items
	result.NextPageToken = scopeStringValue(resp.Data.PageToken)
	if resp.Data.HasMore != nil {
		result.HasMore = *resp.Data.HasMore
	}
	if err != nil {
		return result, err
	}
	if result.HasMore && (result.NextPageToken == "" || result.NextPageToken == req.PageToken) {
		return result, fmt.Errorf("%s returned non-advancing page token", api)
	}
	return result, nil
}
