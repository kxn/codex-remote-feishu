package feishu

import (
	"context"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

type ProjectDocumentCommentsRequest struct {
	FileToken, FileType, CommentID, PageToken string
	PageSize                                  int
}
type ProjectDocumentCommentsPage struct {
	FileToken     string                        `json:"file_token"`
	FileType      string                        `json:"file_type"`
	CommentID     string                        `json:"comment_id,omitempty"`
	Comments      []*larkdrive.FileComment      `json:"comments,omitempty"`
	Replies       []*larkdrive.FileCommentReply `json:"replies,omitempty"`
	HasMore       bool                          `json:"has_more"`
	NextPageToken string                        `json:"next_page_token,omitempty"`
	RequestID     string                        `json:"request_id"`
}

// ReadDocumentCommentsPage preserves comment IDs, quote/anchor facts, solved
// state, actor IDs and each comment's own reply cursor. It never equates solved
// with business acceptance or claims missed notifications were streamed live.
func (a *ProjectDocumentAPI) ReadDocumentCommentsPage(ctx context.Context, req ProjectDocumentCommentsRequest) (ProjectDocumentCommentsPage, error) {
	result := ProjectDocumentCommentsPage{FileToken: req.FileToken, FileType: req.FileType, CommentID: req.CommentID}
	if err := projectBareToken(req.FileToken, "file token"); err != nil {
		return result, err
	}
	if !isSupportedDriveFileCommentFileType(req.FileType) {
		return result, fmt.Errorf("unsupported actual comment file type")
	}
	size := req.PageSize
	if size == 0 {
		size = 20
	}
	if size < 1 || size > 50 {
		return result, fmt.Errorf("comments page size must be1..50")
	}
	if req.CommentID == "" {
		const api = "drive.v1.file_comment.list"
		resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, req.FileToken, true), func(ctx context.Context, c *lark.Client) (*larkdrive.ListFileCommentResp, error) {
			b := larkdrive.NewListFileCommentReqBuilder().FileToken(req.FileToken).FileType(req.FileType).UserIdType("open_id").PageSize(size)
			if req.PageToken != "" {
				b.PageToken(req.PageToken)
			}
			resp, err := c.Drive.V1.FileComment.List(ctx, b.Build())
			if err != nil {
				return resp, err
			}
			if resp == nil {
				return nil, fmt.Errorf("comments returned nil response")
			}
			if !resp.Success() {
				return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
			}
			if resp.Data == nil {
				return resp, fmt.Errorf("comments returned no data")
			}
			return resp, nil
		})
		if resp != nil {
			result.RequestID = resp.RequestId()
			if resp.Data != nil {
				result.Comments = resp.Data.Items
				result.HasMore = resp.Data.HasMore != nil && *resp.Data.HasMore
				result.NextPageToken = scopeStringValue(resp.Data.PageToken)
			}
		}
		return result, err
	}
	if err := projectBareToken(req.CommentID, "comment id"); err != nil {
		return result, err
	}
	const api = "drive.v1.file_comment_reply.list"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, req.FileToken, true), func(ctx context.Context, c *lark.Client) (*larkdrive.ListFileCommentReplyResp, error) {
		b := larkdrive.NewListFileCommentReplyReqBuilder().FileToken(req.FileToken).FileType(req.FileType).CommentId(req.CommentID).UserIdType("open_id").PageSize(size)
		if req.PageToken != "" {
			b.PageToken(req.PageToken)
		}
		resp, err := c.Drive.V1.FileCommentReply.List(ctx, b.Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("comment replies returned nil response")
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("comment replies returned no data")
		}
		return resp, nil
	})
	if resp != nil {
		result.RequestID = resp.RequestId()
		if resp.Data != nil {
			result.Replies = resp.Data.Items
			result.HasMore = resp.Data.HasMore != nil && *resp.Data.HasMore
			result.NextPageToken = scopeStringValue(resp.Data.PageToken)
		}
	}
	return result, err
}
