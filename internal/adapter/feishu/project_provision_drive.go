package feishu

import (
	"context"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

type ProjectFolderReceipt struct{ Token, URL, RequestID string }

// CreateProjectFolder has no universal client_token. A transport failure with no
// token is unknown and must not be retried by matching a human-readable title.
func (a *ProjectDocumentAPI) CreateProjectFolder(ctx context.Context, name, parentToken string) (ProjectFolderReceipt, error) {
	if name == "" {
		return ProjectFolderReceipt{}, fmt.Errorf("folder name is required")
	}
	const api = "drive.v1.file.create_folder"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, parentToken, false), func(ctx context.Context, c *lark.Client) (*larkdrive.CreateFolderFileResp, error) {
		r, e := c.Drive.V1.File.CreateFolder(ctx, larkdrive.NewCreateFolderFileReqBuilder().Body(larkdrive.NewCreateFolderFileReqBodyBuilder().Name(name).FolderToken(parentToken).Build()).Build())
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
	receipt := ProjectFolderReceipt{}
	if resp != nil {
		receipt.RequestID = resp.RequestId()
		if resp.Data != nil {
			receipt.Token = scopeStringValue(resp.Data.Token)
			receipt.URL = scopeStringValue(resp.Data.Url)
		}
	}
	if err != nil {
		return receipt, err
	}
	if receipt.Token == "" {
		return receipt, fmt.Errorf("missing created folder token")
	}
	return receipt, nil
}
func (a *ProjectDocumentAPI) CheckProjectFolder(ctx context.Context, token string) error {
	if err := projectBareToken(token, "folder token"); err != nil {
		return err
	}
	const api = "drive.v1.file.list"
	_, err := DoSDK(ctx, a.broker, projectResourceSpec(api, token, true), func(ctx context.Context, c *lark.Client) (*larkdrive.ListFileResp, error) {
		r, e := c.Drive.V1.File.List(ctx, larkdrive.NewListFileReqBuilder().FolderToken(token).PageSize(1).Build())
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
	return err
}
func (a *ProjectDocumentAPI) ProjectFileURL(ctx context.Context, token, kind string) (string, error) {
	if err := projectBareToken(token, "file token"); err != nil {
		return "", err
	}
	const api = "drive.v1.meta.batch_query"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, token, true), func(ctx context.Context, c *lark.Client) (*larkdrive.BatchQueryMetaResp, error) {
		r, e := c.Drive.V1.Meta.BatchQuery(ctx, larkdrive.NewBatchQueryMetaReqBuilder().MetaRequest(larkdrive.NewMetaRequestBuilder().RequestDocs([]*larkdrive.RequestDoc{larkdrive.NewRequestDocBuilder().DocToken(token).DocType(kind).Build()}).WithUrl(true).Build()).Build())
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
		return "", err
	}
	for _, meta := range resp.Data.Metas {
		if meta == nil {
			continue
		}
		id := scopeStringValue(meta.DocToken)
		if id == "" && meta.RequestDocInfo != nil {
			id = scopeStringValue(meta.RequestDocInfo.DocToken)
		}
		if id == token && scopeStringValue(meta.Url) != "" {
			return scopeStringValue(meta.Url), nil
		}
	}
	return "", fmt.Errorf("platform did not return a URL for the exact selected object")
}

// ProjectGatewayIdentity exposes only the selected credential's public identity,
// so a resolver cannot accidentally return a different/default bot gateway.
func (g *LiveGateway) ProjectGatewayIdentity() (gatewayID, appID string) {
	if g == nil {
		return "", ""
	}
	return g.config.GatewayID, g.config.AppID
}
