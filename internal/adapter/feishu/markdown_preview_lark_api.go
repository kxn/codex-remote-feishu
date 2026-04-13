package feishu

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

type larkDrivePreviewAPI struct {
	client *lark.Client
}

func NewLarkDrivePreviewAPI(client *lark.Client) previewDriveAPI {
	if client == nil {
		return nil
	}
	return &larkDrivePreviewAPI{client: client}
}

func (a *larkDrivePreviewAPI) CreateFolder(ctx context.Context, name, parentToken string) (previewRemoteNode, error) {
	resp, err := a.client.Drive.V1.File.CreateFolder(ctx, larkdrive.NewCreateFolderFileReqBuilder().
		Body(larkdrive.NewCreateFolderFileReqBodyBuilder().
			Name(name).
			FolderToken(parentToken).
			Build()).
		Build())
	if err != nil {
		return previewRemoteNode{}, err
	}
	if !resp.Success() {
		return previewRemoteNode{}, &driveAPIError{
			API:       "drive.v1.file.create_folder",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	if resp.Data == nil {
		return previewRemoteNode{}, fmt.Errorf("missing create folder response data")
	}
	return previewRemoteNode{
		Token: stringValue(resp.Data.Token),
		URL:   stringValue(resp.Data.Url),
		Type:  previewFolderType,
		Name:  name,
	}, nil
}

func (a *larkDrivePreviewAPI) UploadFile(ctx context.Context, parentToken, fileName string, content []byte) (string, error) {
	resp, err := a.client.Drive.V1.File.UploadAll(ctx, larkdrive.NewUploadAllFileReqBuilder().
		Body(larkdrive.NewUploadAllFileReqBodyBuilder().
			FileName(fileName).
			ParentType("explorer").
			ParentNode(parentToken).
			Size(len(content)).
			File(bytes.NewReader(content)).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", &driveAPIError{
			API:       "drive.v1.file.upload_all",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	if resp.Data == nil {
		return "", fmt.Errorf("missing upload file response data")
	}
	return stringValue(resp.Data.FileToken), nil
}

func (a *larkDrivePreviewAPI) QueryMetaURL(ctx context.Context, token, docType string) (string, error) {
	resp, err := a.client.Drive.V1.Meta.BatchQuery(ctx, larkdrive.NewBatchQueryMetaReqBuilder().
		MetaRequest(larkdrive.NewMetaRequestBuilder().
			RequestDocs([]*larkdrive.RequestDoc{
				larkdrive.NewRequestDocBuilder().
					DocToken(token).
					DocType(docType).
					Build(),
			}).
			WithUrl(true).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", &driveAPIError{
			API:       "drive.v1.meta.batch_query",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	if resp.Data == nil || len(resp.Data.Metas) == 0 || resp.Data.Metas[0] == nil {
		return "", fmt.Errorf("missing meta url for token %s", token)
	}
	return stringValue(resp.Data.Metas[0].Url), nil
}

func (a *larkDrivePreviewAPI) GrantPermission(ctx context.Context, token, docType string, principal previewPrincipal) error {
	resp, err := a.client.Drive.V1.PermissionMember.Create(ctx, larkdrive.NewCreatePermissionMemberReqBuilder().
		Token(token).
		Type(docType).
		BaseMember(larkdrive.NewBaseMemberBuilder().
			MemberType(principal.MemberType).
			MemberId(principal.MemberID).
			Perm(previewPermissionView).
			Type(principal.Type).
			Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return &driveAPIError{
			API:       "drive.v1.permission_member.create",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	return nil
}

func (a *larkDrivePreviewAPI) DeleteFile(ctx context.Context, token, docType string) error {
	resp, err := a.client.Drive.V1.File.Delete(ctx, larkdrive.NewDeleteFileReqBuilder().
		FileToken(token).
		Type(docType).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return &driveAPIError{
			API:       "drive.v1.file.delete",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	return nil
}

func (a *larkDrivePreviewAPI) ListFiles(ctx context.Context, folderToken string) ([]previewRemoteNode, error) {
	values := []previewRemoteNode{}
	pageToken := ""
	for {
		req := larkdrive.NewListFileReqBuilder().
			FolderToken(folderToken).
			PageSize(200)
		if strings.TrimSpace(pageToken) != "" {
			req.PageToken(pageToken)
		}
		resp, err := a.client.Drive.V1.File.List(ctx, req.Build())
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, &driveAPIError{
				API:       "drive.v1.file.list",
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: strings.TrimSpace(resp.RequestId()),
				LogID:     strings.TrimSpace(resp.RequestId()),
			}
		}
		if resp.Data != nil {
			for _, file := range resp.Data.Files {
				if file == nil {
					continue
				}
				values = append(values, previewRemoteNode{
					Token:        stringValue(file.Token),
					URL:          stringValue(file.Url),
					Type:         strings.TrimSpace(stringValue(file.Type)),
					Name:         strings.TrimSpace(stringValue(file.Name)),
					CreatedTime:  parsePreviewRemoteTime(stringValue(file.CreatedTime)),
					ModifiedTime: parsePreviewRemoteTime(stringValue(file.ModifiedTime)),
				})
			}
			if resp.Data.HasMore != nil && *resp.Data.HasMore && strings.TrimSpace(stringValue(resp.Data.NextPageToken)) != "" {
				pageToken = stringValue(resp.Data.NextPageToken)
				continue
			}
		}
		break
	}
	return values, nil
}

func (a *larkDrivePreviewAPI) ListPermissionMembers(ctx context.Context, token, docType string) (map[string]bool, error) {
	resp, err := a.client.Drive.V1.PermissionMember.List(ctx, larkdrive.NewListPermissionMemberReqBuilder().
		Token(token).
		Type(docType).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, &driveAPIError{
			API:       "drive.v1.permission_member.list",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	values := map[string]bool{}
	if resp.Data == nil {
		return values, nil
	}
	for _, item := range resp.Data.Items {
		if item == nil {
			continue
		}
		memberType := strings.TrimSpace(stringValue(item.MemberType))
		memberID := strings.TrimSpace(stringValue(item.MemberId))
		if memberType == "" || memberID == "" {
			continue
		}
		values[memberType+":"+memberID] = true
	}
	return values, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
