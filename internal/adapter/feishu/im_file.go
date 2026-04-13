package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type IMFileSender interface {
	SendIMFile(context.Context, IMFileSendRequest) (IMFileSendResult, error)
}

type IMFileSendRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	Path             string
}

type IMFileSendResult struct {
	GatewayID        string
	SurfaceSessionID string
	ReceiveID        string
	ReceiveIDType    string
	FileKey          string
	FileName         string
	MessageID        string
}

type IMFileSendErrorCode string

const (
	IMFileSendErrorGatewayNotRunning    IMFileSendErrorCode = "gateway_not_running"
	IMFileSendErrorMissingReceiveTarget IMFileSendErrorCode = "missing_receive_target"
	IMFileSendErrorUploadFailed         IMFileSendErrorCode = "upload_failed"
	IMFileSendErrorSendFailed           IMFileSendErrorCode = "send_failed"
)

type IMFileSendError struct {
	Code IMFileSendErrorCode
	Err  error
}

func (e *IMFileSendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}

func (e *IMFileSendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (g *LiveGateway) SendIMFile(ctx context.Context, req IMFileSendRequest) (IMFileSendResult, error) {
	result := IMFileSendResult{
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: strings.TrimSpace(req.SurfaceSessionID),
	}
	if gatewayID := normalizeGatewayID(req.GatewayID); gatewayID != "" && gatewayID != g.config.GatewayID {
		return result, &IMFileSendError{
			Code: IMFileSendErrorGatewayNotRunning,
			Err:  fmt.Errorf("send file failed: gateway mismatch: request=%s gateway=%s", gatewayID, g.config.GatewayID),
		}
	}
	receiveID, receiveIDType := ResolveReceiveTarget(req.ChatID, req.ActorUserID)
	if receiveID == "" || receiveIDType == "" {
		return result, &IMFileSendError{
			Code: IMFileSendErrorMissingReceiveTarget,
			Err:  fmt.Errorf("send file failed: missing receive target"),
		}
	}
	fileKey, fileName, err := g.uploadFilePathFn(ctx, req.Path)
	if err != nil {
		var sendErr *IMFileSendError
		if errors.As(err, &sendErr) {
			return result, sendErr
		}
		return result, &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}
	body, _ := json.Marshal(map[string]string{"file_key": fileKey})
	resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "file", string(body))
	if err != nil {
		return result, &IMFileSendError{
			Code: IMFileSendErrorSendFailed,
			Err:  fmt.Errorf("send file failed: %w", err),
		}
	}
	if !resp.Success() {
		return result, &IMFileSendError{
			Code: IMFileSendErrorSendFailed,
			Err:  newAPIError("im.v1.message.create", resp.ApiResp, resp.CodeError),
		}
	}
	messageID := ""
	if resp.Data != nil {
		messageID = strings.TrimSpace(stringPtr(resp.Data.MessageId))
		g.recordSurfaceMessage(messageID, result.SurfaceSessionID)
	}
	result.ReceiveID = receiveID
	result.ReceiveIDType = receiveIDType
	result.FileKey = fileKey
	result.FileName = fileName
	result.MessageID = messageID
	return result, nil
}

func (g *LiveGateway) uploadFilePath(ctx context.Context, path string) (string, string, error) {
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
	body, err := larkim.NewCreateFilePathReqBodyBuilder().
		FileType(imFileTypeFromName(fileName)).
		FileName(fileName).
		FilePath(path).
		Build()
	if err != nil {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}
	resp, err := g.client.Im.V1.File.Create(ctx, larkim.NewCreateFileReqBuilder().
		Body(body).
		Build())
	if err != nil {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}
	if !resp.Success() {
		return "", "", &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  newAPIError("im.v1.file.create", resp.ApiResp, resp.CodeError),
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

func imFileTypeFromName(fileName string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))), ".")
	if ext == "" {
		return larkim.FileTypeStream
	}
	return ext
}
