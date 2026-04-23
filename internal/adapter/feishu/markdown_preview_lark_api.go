package feishu

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
)

type larkDrivePreviewAPI struct {
	client *lark.Client
	broker *FeishuCallBroker
}

func NewLarkDrivePreviewAPI(gatewayID string, client *lark.Client) previewpkg.DriveAPI {
	if client == nil {
		return nil
	}
	return &larkDrivePreviewAPI{
		client: client,
		broker: NewFeishuCallBroker(gatewayID, client),
	}
}

func (a *larkDrivePreviewAPI) CreateFolder(ctx context.Context, name, parentToken string) (previewpkg.RemoteNode, error) {
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:      "drive.v1.file.create_folder",
		Class:    CallClassDrive,
		Priority: CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			DocToken: parentToken,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.CreateFolderFileResp, error) {
		resp, err := client.Drive.V1.File.CreateFolder(callCtx, larkdrive.NewCreateFolderFileReqBuilder().
			Body(larkdrive.NewCreateFolderFileReqBodyBuilder().
				Name(name).
				FolderToken(parentToken).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
	if err != nil {
		return previewpkg.RemoteNode{}, err
	}
	if !resp.Success() {
		return previewpkg.RemoteNode{}, &driveAPIError{
			API:       "drive.v1.file.create_folder",
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: strings.TrimSpace(resp.RequestId()),
			LogID:     strings.TrimSpace(resp.RequestId()),
		}
	}
	if resp.Data == nil {
		return previewpkg.RemoteNode{}, fmt.Errorf("missing create folder response data")
	}
	return previewpkg.RemoteNode{
		Token: stringValue(resp.Data.Token),
		URL:   stringValue(resp.Data.Url),
		Type:  previewpkg.FolderType,
		Name:  name,
	}, nil
}

func (a *larkDrivePreviewAPI) UploadFile(ctx context.Context, parentToken, fileName string, content []byte) (string, error) {
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:      "drive.v1.file.upload_all",
		Class:    CallClassDrive,
		Priority: CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			DocToken: parentToken,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.UploadAllFileResp, error) {
		resp, err := client.Drive.V1.File.UploadAll(callCtx, larkdrive.NewUploadAllFileReqBuilder().
			Body(larkdrive.NewUploadAllFileReqBodyBuilder().
				FileName(fileName).
				ParentType("explorer").
				ParentNode(parentToken).
				Size(len(content)).
				File(bytes.NewReader(content)).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:      "drive.v1.meta.batch_query",
		Class:    CallClassDrive,
		Priority: CallPriorityReadAssist,
		ResourceKey: FeishuResourceKey{
			DocToken: token,
		},
		Retry:      RetrySafe,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.BatchQueryMetaResp, error) {
		resp, err := client.Drive.V1.Meta.BatchQuery(callCtx, larkdrive.NewBatchQueryMetaReqBuilder().
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
			return resp, err
		}
		return resp, nil
	})
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

func (a *larkDrivePreviewAPI) GrantPermission(ctx context.Context, token, docType string, principal previewpkg.Principal) error {
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:      "drive.v1.permission_member.create",
		Class:    CallClassDrive,
		Priority: CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			DocToken: token,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.CreatePermissionMemberResp, error) {
		resp, err := client.Drive.V1.PermissionMember.Create(callCtx, larkdrive.NewCreatePermissionMemberReqBuilder().
			Token(token).
			Type(docType).
			BaseMember(larkdrive.NewBaseMemberBuilder().
				MemberType(principal.MemberType).
				MemberId(principal.MemberID).
				Perm(previewpkg.PermissionView).
				Type(principal.Type).
				Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:      "drive.v1.file.delete",
		Class:    CallClassDrive,
		Priority: CallPriorityBackground,
		ResourceKey: FeishuResourceKey{
			DocToken: token,
		},
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.DeleteFileResp, error) {
		resp, err := client.Drive.V1.File.Delete(callCtx, larkdrive.NewDeleteFileReqBuilder().
			FileToken(token).
			Type(docType).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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

func (a *larkDrivePreviewAPI) ListFiles(ctx context.Context, folderToken string) ([]previewpkg.RemoteNode, error) {
	values := []previewpkg.RemoteNode{}
	pageToken := ""
	for {
		req := larkdrive.NewListFileReqBuilder().
			FolderToken(folderToken).
			PageSize(200)
		if strings.TrimSpace(pageToken) != "" {
			req.PageToken(pageToken)
		}
		resp, err := DoSDK(ctx, a.broker, CallSpec{
			API:      "drive.v1.file.list",
			Class:    CallClassDrive,
			Priority: CallPriorityReadAssist,
			ResourceKey: FeishuResourceKey{
				DocToken: folderToken,
			},
			Retry:      RetrySafe,
			Permission: PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkdrive.ListFileResp, error) {
			resp, err := client.Drive.V1.File.List(callCtx, req.Build())
			if err != nil {
				return resp, err
			}
			return resp, nil
		})
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
				values = append(values, previewpkg.RemoteNode{
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:      "drive.v1.permission_member.list",
		Class:    CallClassDrive,
		Priority: CallPriorityReadAssist,
		ResourceKey: FeishuResourceKey{
			DocToken: token,
		},
		Retry:      RetrySafe,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.ListPermissionMemberResp, error) {
		resp, err := client.Drive.V1.PermissionMember.List(callCtx, larkdrive.NewListPermissionMemberReqBuilder().
			Token(token).
			Type(docType).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
