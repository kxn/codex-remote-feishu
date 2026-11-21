package feishu

import (
	"context"
	"fmt"
	"io"
	"net/http"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

type ProjectDownloadDocumentMediaRequest struct {
	FileToken string
	MaxBytes  int64
}
type ProjectDocumentMedia struct {
	Content             []byte
	MediaType, FileName string
}

// DownloadDocumentMedia downloads a Docx block's actual file/image token using
// its bound credential. Byte limits apply while streaming, never after ReadAll.
func (a *ProjectDocumentAPI) DownloadDocumentMedia(ctx context.Context, req ProjectDownloadDocumentMediaRequest) (ProjectDocumentMedia, error) {
	if err := projectBareToken(req.FileToken, "document media token"); err != nil {
		return ProjectDocumentMedia{}, err
	}
	if req.MaxBytes < 1 || req.MaxBytes > 30_000_000 {
		return ProjectDocumentMedia{}, fmt.Errorf("explicit media limit must be1..30000000 bytes")
	}
	const api = "drive.v1.media.download"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, req.FileToken, true), func(callCtx context.Context, client *lark.Client) (*larkdrive.DownloadMediaResp, error) {
		response, e := client.Drive.V1.Media.Download(callCtx, larkdrive.NewDownloadMediaReqBuilder().FileToken(req.FileToken).Build())
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
	if err != nil {
		return ProjectDocumentMedia{}, err
	}
	if resp.File == nil {
		return ProjectDocumentMedia{}, fmt.Errorf("document media missing bytes")
	}
	if closer, ok := resp.File.(io.Closer); ok {
		defer closer.Close()
	}
	data, err := io.ReadAll(io.LimitReader(resp.File, req.MaxBytes+1))
	if err != nil {
		return ProjectDocumentMedia{}, err
	}
	if int64(len(data)) > req.MaxBytes {
		return ProjectDocumentMedia{}, fmt.Errorf("document media exceeds explicit byte limit")
	}
	return ProjectDocumentMedia{Content: data, FileName: resp.FileName, MediaType: http.DetectContentType(data)}, nil
}
