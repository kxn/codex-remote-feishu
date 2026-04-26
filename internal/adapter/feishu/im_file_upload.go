package feishu

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type imUploadedFileSemantic string

const (
	imUploadedFileSemanticAttachment imUploadedFileSemantic = "attachment"
	imUploadedFileSemanticAudio      imUploadedFileSemantic = "audio"
	imUploadedFileSemanticVideo      imUploadedFileSemantic = "video"
)

func (g *LiveGateway) uploadFilePath(ctx context.Context, path string) (string, string, error) {
	return g.uploadFilePathForSemantic(ctx, path, imUploadedFileSemanticAttachment)
}

func (g *LiveGateway) uploadVideoPath(ctx context.Context, path string) (string, string, error) {
	return g.uploadFilePathForSemantic(ctx, path, imUploadedFileSemanticVideo)
}

func (g *LiveGateway) uploadFilePathForSemantic(ctx context.Context, path string, semantic imUploadedFileSemantic) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: missing file path"),
		}
	}
	fileName := strings.TrimSpace(filepath.Base(path))
	if fileName == "" || fileName == "." {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: invalid file name"),
		}
	}

	fileType, err := imFileTypeForSemantic(fileName, semantic)
	if err != nil {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}

	body, err := larkim.NewCreateFilePathReqBodyBuilder().
		FileType(fileType).
		FileName(fileName).
		FilePath(path).
		Build()
	if err != nil {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}
	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID:  g.config.GatewayID,
		API:        "im.v1.file.create",
		Class:      CallClassIMSend,
		Priority:   CallPriorityInteractive,
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkim.CreateFileResp, error) {
		resp, err := client.Im.V1.File.Create(callCtx, larkim.NewCreateFileReqBuilder().
			Body(body).
			Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return resp, newAPIError("im.v1.file.create", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}
	if resp.Data == nil {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: missing file key"),
		}
	}
	fileKey := strings.TrimSpace(stringPtr(resp.Data.FileKey))
	if fileKey == "" {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: missing file key"),
		}
	}
	return fileKey, fileName, nil
}

func imFileTypeForSemantic(fileName string, semantic imUploadedFileSemantic) (string, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))), ".")
	switch semantic {
	case imUploadedFileSemanticVideo:
		if ext != "mp4" {
			return "", fmt.Errorf("video messages require an .mp4 file")
		}
		return larkim.FileTypeMp4, nil
	case imUploadedFileSemanticAudio:
		if ext != "opus" {
			return "", fmt.Errorf("audio messages require an .opus file")
		}
		return larkim.FileTypeOpus, nil
	default:
		return imAttachmentFileTypeFromName(fileName), nil
	}
}

func imAttachmentFileTypeFromName(fileName string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))), ".")
	switch ext {
	case "pdf":
		return larkim.FileTypePdf
	case "doc", "docx", "docm", "dot", "dotx", "dotm", "wps":
		return larkim.FileTypeDoc
	case "xls", "xlsx", "xlsm", "xlt", "xltx", "xltm", "csv", "et":
		return larkim.FileTypeXls
	case "ppt", "pptx", "pptm", "pps", "ppsx", "ppsm", "pot", "potx", "potm", "dps", "dpt":
		return larkim.FileTypePpt
	default:
		// Attachment sends intentionally collapse media-specific extensions such as
		// .mp4/.opus into a generic file upload so the final message semantics stay
		// aligned with msg_type=file instead of implicitly turning into video/audio.
		return larkim.FileTypeStream
	}
}
