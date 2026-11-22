package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type UploadResultFileRequest struct {
	FileName, FileType string
	Content            []byte
}
type PublishResultFileRequest struct {
	ChatID, ReplyToMessageID, FileKey, UUID string
	ReplyInThread                           bool
}

// UploadResultFile returns only an upload receipt, never a sent/read receipt.
// Feishu im.v1.file.create permits nonempty files up to 30 MB. No endpoint
// client_token exists here; uncertain upload must not be blindly repeated.
func (a *ProjectChatAPI) UploadResultFile(ctx context.Context, req UploadResultFileRequest) (string, error) {
	if strings.TrimSpace(req.FileName) == "" || strings.ContainsAny(req.FileName, "\r\n\x00") || len(req.FileName) > 1024 || len(req.Content) == 0 || len(req.Content) > 30_000_000 {
		return "", fmt.Errorf("file requires a name and nonempty content no larger than30 MB")
	}
	if req.FileType == "" {
		req.FileType = "stream"
	}
	switch req.FileType {
	case "opus", "mp4", "pdf", "doc", "xls", "ppt", "stream":
	default:
		return "", fmt.Errorf("unsupported platform file type")
	}
	const api = "im.v1.file.create"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, "", false), func(callCtx context.Context, client *lark.Client) (*larkim.CreateFileResp, error) {
		response, e := client.Im.V1.File.Create(callCtx, larkim.NewCreateFileReqBuilder().Body(larkim.NewCreateFileReqBodyBuilder().FileName(req.FileName).FileType(req.FileType).File(bytes.NewReader(req.Content)).Build()).Build())
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
	key := ""
	if resp != nil && resp.Data != nil {
		key = scopeStringValue(resp.Data.FileKey)
	}
	if err != nil {
		return key, err
	}
	if key == "" {
		return "", fmt.Errorf("file upload lacks actual file_key")
	}
	return key, nil
}
func (a *ProjectChatAPI) PublishResultFile(ctx context.Context, req PublishResultFileRequest) (ProjectPublishReceipt, error) {
	if req.ChatID == "" || req.FileKey == "" || req.UUID == "" || len(req.UUID) > 50 {
		return ProjectPublishReceipt{}, fmt.Errorf("chat, actual file_key and persistent UUID required")
	}
	content, err := json.Marshal(map[string]string{"file_key": req.FileKey})
	if err != nil {
		return ProjectPublishReceipt{}, err
	}
	return a.publishContent(ctx, ProjectPublishRequest{ChatID: req.ChatID, ReplyToMessageID: req.ReplyToMessageID, ReplyInThread: req.ReplyInThread, UUID: req.UUID}, "file", content)
}
