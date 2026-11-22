package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const MaxPrivateImageBytes = 10_000_000

func (a *ProjectChatAPI) UploadPrivateMedia(ctx context.Context, kind, filename string, data []byte) (string, error) {
	switch kind {
	case "file":
		return a.UploadResultFile(ctx, UploadResultFileRequest{FileName: filename, Content: data})
	case "video":
		return a.UploadResultFile(ctx, UploadResultFileRequest{FileName: filename, FileType: "mp4", Content: data})
	case "image":
	default:
		return "", fmt.Errorf("unsupported private media kind")
	}
	if len(data) == 0 || len(data) > MaxPrivateImageBytes {
		return "", fmt.Errorf("image must be nonempty and no larger than 10 MB")
	}
	const api = "im.v1.image.create"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, "", false), func(ctx context.Context, client *lark.Client) (*larkim.CreateImageResp, error) {
		body := larkim.NewCreateImageReqBodyBuilder().ImageType("message").Image(bytes.NewReader(data)).Build()
		response, err := client.Im.V1.Image.Create(ctx, larkim.NewCreateImageReqBuilder().Body(body).Build())
		if err != nil {
			return response, err
		}
		if response == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !response.Success() {
			return response, newAPIError(api, response.ApiResp, response.CodeError)
		}
		return response, nil
	})
	if err != nil {
		return "", err
	}
	if resp.Data == nil || scopeStringValue(resp.Data.ImageKey) == "" {
		return "", fmt.Errorf("image upload lacks actual image_key")
	}
	return scopeStringValue(resp.Data.ImageKey), nil
}

func (a *ProjectChatAPI) PublishPrivateMedia(ctx context.Context, req ProjectPublishRequest, kind, key string) (ProjectPublishReceipt, error) {
	if req.ChatID == "" || req.UUID == "" || len(req.UUID) > 50 || key == "" || req.ReplyInThread {
		return ProjectPublishReceipt{}, fmt.Errorf("private media requires a main-chat destination and a durable upload key")
	}
	msgType, field := "file", "file_key"
	switch kind {
	case "file":
	case "image":
		msgType, field = "image", "image_key"
	case "video":
		msgType = "media"
	default:
		return ProjectPublishReceipt{}, fmt.Errorf("unsupported private media kind")
	}
	content, err := json.Marshal(map[string]string{field: key})
	if err != nil {
		return ProjectPublishReceipt{}, err
	}
	return a.publishContent(ctx, req, msgType, content)
}
