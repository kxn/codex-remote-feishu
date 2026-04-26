package feishu

import (
	"context"
	"fmt"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

const (
	defaultDriveFileCommentPageSize      = 20
	maxDriveFileCommentPageSize          = 100
	defaultDriveFileCommentReplyPageSize = 50
	readDriveFileCommentsTimeout         = 30 * time.Second
	driveFileCommentStatsScopePage       = "returned_comments_page"
)

type DriveFileCommentReader interface {
	ReadDriveFileComments(context.Context, DriveFileCommentReadRequest) (DriveFileCommentReadResult, error)
}

type DriveFileCommentReadRequest struct {
	GatewayID string
	FileToken string
	FileType  string
	PageToken string
	PageSize  int
}

type DriveFileCommentReadResult struct {
	GatewayID        string                  `json:"gateway_id"`
	FileToken        string                  `json:"file_token"`
	FileType         string                  `json:"file_type"`
	HasMore          bool                    `json:"has_more"`
	NextPageToken    string                  `json:"next_page_token,omitempty"`
	StatsScope       string                  `json:"stats_scope"`
	CommentCount     int                     `json:"comment_count"`
	ReplyCount       int                     `json:"reply_count"`
	InteractionCount int                     `json:"interaction_count"`
	Comments         []DriveFileCommentEntry `json:"comments"`
}

type DriveFileCommentEntry struct {
	CommentID    string                      `json:"comment_id"`
	UserID       string                      `json:"user_id,omitempty"`
	CreateTime   int                         `json:"create_time,omitempty"`
	UpdateTime   int                         `json:"update_time,omitempty"`
	IsSolved     bool                        `json:"is_solved"`
	SolvedTime   int                         `json:"solved_time,omitempty"`
	SolverUserID string                      `json:"solver_user_id,omitempty"`
	IsWhole      bool                        `json:"is_whole"`
	Quote        string                      `json:"quote,omitempty"`
	Replies      []DriveFileCommentReplyItem `json:"replies,omitempty"`
}

type DriveFileCommentReplyItem struct {
	ReplyID     string                         `json:"reply_id,omitempty"`
	UserID      string                         `json:"user_id,omitempty"`
	CreateTime  int                            `json:"create_time,omitempty"`
	UpdateTime  int                            `json:"update_time,omitempty"`
	Text        string                         `json:"text,omitempty"`
	Elements    []DriveFileCommentReplyElement `json:"elements,omitempty"`
	ImageTokens []string                       `json:"image_tokens,omitempty"`
}

type DriveFileCommentReplyElement struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	URL    string `json:"url,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

type DriveFileCommentReadErrorCode string

const (
	DriveFileCommentReadErrorGatewayNotRunning DriveFileCommentReadErrorCode = "gateway_not_running"
	DriveFileCommentReadErrorInvalidFileType   DriveFileCommentReadErrorCode = "invalid_file_type"
	DriveFileCommentReadErrorListFailed        DriveFileCommentReadErrorCode = "list_failed"
	DriveFileCommentReadErrorReplyListFailed   DriveFileCommentReadErrorCode = "reply_list_failed"
)

type DriveFileCommentReadError struct {
	Code DriveFileCommentReadErrorCode
	Err  error
}

func (e *DriveFileCommentReadError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}

func (e *DriveFileCommentReadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (g *LiveGateway) ReadDriveFileComments(ctx context.Context, req DriveFileCommentReadRequest) (DriveFileCommentReadResult, error) {
	ctx, cancel := newFeishuTimeoutContext(ctx, readDriveFileCommentsTimeout)
	defer cancel()

	fileToken := strings.TrimSpace(req.FileToken)
	fileType := normalizeDriveFileCommentFileType(req.FileType)
	pageToken := strings.TrimSpace(req.PageToken)
	pageSize := normalizeDriveFileCommentPageSize(req.PageSize)
	result := DriveFileCommentReadResult{
		GatewayID:  g.config.GatewayID,
		FileToken:  fileToken,
		FileType:   fileType,
		StatsScope: driveFileCommentStatsScopePage,
	}

	if gatewayID := normalizeGatewayID(req.GatewayID); gatewayID != "" && gatewayID != g.config.GatewayID {
		return result, &DriveFileCommentReadError{
			Code: DriveFileCommentReadErrorGatewayNotRunning,
			Err:  fmt.Errorf("read drive comments failed: gateway mismatch: request=%s gateway=%s", gatewayID, g.config.GatewayID),
		}
	}
	if !isSupportedDriveFileCommentFileType(fileType) {
		return result, &DriveFileCommentReadError{
			Code: DriveFileCommentReadErrorInvalidFileType,
			Err:  fmt.Errorf("read drive comments failed: unsupported file_type %q", strings.TrimSpace(req.FileType)),
		}
	}

	resp, err := DoSDK(ctx, g.broker, CallSpec{
		GatewayID: g.config.GatewayID,
		API:       "drive.v1.file_comment.list",
		Class:     CallClassDrive,
		Priority:  CallPriorityReadAssist,
		ResourceKey: FeishuResourceKey{
			DocToken: fileToken,
		},
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.ListFileCommentResp, error) {
		reqBuilder := larkdrive.NewListFileCommentReqBuilder().
			FileToken(fileToken).
			FileType(fileType).
			PageSize(pageSize).
			UserIdType(larkdrive.UserIdTypeListFileCommentOpenId)
		if pageToken != "" {
			reqBuilder.PageToken(pageToken)
		}
		resp, err := client.Drive.V1.FileComment.List(callCtx, reqBuilder.Build())
		if err != nil {
			return resp, err
		}
		if !resp.Success() {
			return nil, newAPIError("drive.v1.file_comment.list", resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if err != nil {
		return result, &DriveFileCommentReadError{
			Code: DriveFileCommentReadErrorListFailed,
			Err:  fmt.Errorf("read drive comments failed: %w", err),
		}
	}

	if resp.Data == nil {
		return result, nil
	}
	result.HasMore = boolPtr(resp.Data.HasMore)
	result.NextPageToken = strings.TrimSpace(stringPtr(resp.Data.PageToken))

	comments, err := g.buildDriveFileCommentEntries(ctx, fileToken, fileType, resp.Data.Items)
	if err != nil {
		return result, err
	}
	result.Comments = comments
	result.CommentCount = len(comments)
	for _, item := range comments {
		replyTotal := len(item.Replies)
		result.InteractionCount += replyTotal
		if replyTotal > 1 {
			result.ReplyCount += replyTotal - 1
		}
	}
	return result, nil
}

func (g *LiveGateway) buildDriveFileCommentEntries(ctx context.Context, fileToken, fileType string, items []*larkdrive.FileComment) ([]DriveFileCommentEntry, error) {
	if len(items) == 0 {
		return nil, nil
	}
	comments := make([]DriveFileCommentEntry, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		commentID := strings.TrimSpace(stringPtr(item.CommentId))
		replies, err := g.collectDriveFileCommentReplies(ctx, fileToken, fileType, commentID, item)
		if err != nil {
			return nil, err
		}
		comments = append(comments, DriveFileCommentEntry{
			CommentID:    commentID,
			UserID:       strings.TrimSpace(stringPtr(item.UserId)),
			CreateTime:   intValue(item.CreateTime),
			UpdateTime:   intValue(item.UpdateTime),
			IsSolved:     boolPtr(item.IsSolved),
			SolvedTime:   intValue(item.SolvedTime),
			SolverUserID: strings.TrimSpace(stringPtr(item.SolverUserId)),
			IsWhole:      boolPtr(item.IsWhole),
			Quote:        strings.TrimSpace(stringPtr(item.Quote)),
			Replies:      replies,
		})
	}
	return comments, nil
}

func (g *LiveGateway) collectDriveFileCommentReplies(ctx context.Context, fileToken, fileType, commentID string, item *larkdrive.FileComment) ([]DriveFileCommentReplyItem, error) {
	replies := flattenDriveFileCommentReplies(replyListReplies(item))
	if !boolPtr(item.HasMore) {
		return replies, nil
	}
	extraReplies, err := g.listDriveFileCommentReplies(ctx, fileToken, fileType, commentID, strings.TrimSpace(stringPtr(item.PageToken)))
	if err != nil {
		return nil, err
	}
	return dedupeDriveFileCommentReplies(append(replies, extraReplies...)), nil
}

func (g *LiveGateway) listDriveFileCommentReplies(ctx context.Context, fileToken, fileType, commentID, pageToken string) ([]DriveFileCommentReplyItem, error) {
	if strings.TrimSpace(commentID) == "" {
		return nil, nil
	}
	replies := make([]DriveFileCommentReplyItem, 0)
	nextPageToken := strings.TrimSpace(pageToken)
	for {
		resp, err := DoSDK(ctx, g.broker, CallSpec{
			GatewayID: g.config.GatewayID,
			API:       "drive.v1.file_comment_reply.list",
			Class:     CallClassDrive,
			Priority:  CallPriorityReadAssist,
			ResourceKey: FeishuResourceKey{
				DocToken: fileToken,
			},
			Retry:      RetryRateLimitOnly,
			Permission: PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkdrive.ListFileCommentReplyResp, error) {
			reqBuilder := larkdrive.NewListFileCommentReplyReqBuilder().
				FileToken(fileToken).
				CommentId(commentID).
				FileType(fileType).
				PageSize(defaultDriveFileCommentReplyPageSize).
				UserIdType(larkdrive.UserIdTypeListFileCommentReplyOpenId)
			if nextPageToken != "" {
				reqBuilder.PageToken(nextPageToken)
			}
			resp, err := client.Drive.V1.FileCommentReply.List(callCtx, reqBuilder.Build())
			if err != nil {
				return resp, err
			}
			if !resp.Success() {
				return nil, newAPIError("drive.v1.file_comment_reply.list", resp.ApiResp, resp.CodeError)
			}
			return resp, nil
		})
		if err != nil {
			return nil, &DriveFileCommentReadError{
				Code: DriveFileCommentReadErrorReplyListFailed,
				Err:  fmt.Errorf("read drive comment replies failed: %w", err),
			}
		}
		if resp.Data == nil {
			return replies, nil
		}
		replies = append(replies, flattenDriveFileCommentReplies(resp.Data.Items)...)
		if !boolPtr(resp.Data.HasMore) {
			return replies, nil
		}
		nextPageToken = strings.TrimSpace(stringPtr(resp.Data.PageToken))
		if nextPageToken == "" {
			return nil, &DriveFileCommentReadError{
				Code: DriveFileCommentReadErrorReplyListFailed,
				Err:  fmt.Errorf("read drive comment replies failed: missing next page token for comment %s", commentID),
			}
		}
	}
}

func flattenDriveFileCommentReplies(items []*larkdrive.FileCommentReply) []DriveFileCommentReplyItem {
	if len(items) == 0 {
		return nil
	}
	replies := make([]DriveFileCommentReplyItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		text, elements := flattenDriveReplyContent(item.Content)
		reply := DriveFileCommentReplyItem{
			ReplyID:    strings.TrimSpace(stringPtr(item.ReplyId)),
			UserID:     strings.TrimSpace(stringPtr(item.UserId)),
			CreateTime: intValue(item.CreateTime),
			UpdateTime: intValue(item.UpdateTime),
			Text:       text,
			Elements:   elements,
		}
		if item.Extra != nil && len(item.Extra.ImageList) > 0 {
			reply.ImageTokens = append([]string(nil), item.Extra.ImageList...)
		}
		replies = append(replies, reply)
	}
	return replies
}

func flattenDriveReplyContent(content *larkdrive.ReplyContent) (string, []DriveFileCommentReplyElement) {
	if content == nil || len(content.Elements) == 0 {
		return "", nil
	}
	elements := make([]DriveFileCommentReplyElement, 0, len(content.Elements))
	var textBuilder strings.Builder
	for _, item := range content.Elements {
		if item == nil {
			continue
		}
		element := DriveFileCommentReplyElement{Type: strings.TrimSpace(stringPtr(item.Type))}
		switch element.Type {
		case "text_run":
			element.Text = strings.TrimSpace(stringPtrFromTextRun(item.TextRun))
			textBuilder.WriteString(stringPtrFromTextRun(item.TextRun))
		case "docs_link":
			element.URL = strings.TrimSpace(stringPtrFromDocsLink(item.DocsLink))
			textBuilder.WriteString(stringPtrFromDocsLink(item.DocsLink))
		case "person":
			element.UserID = strings.TrimSpace(stringPtrFromPerson(item.Person))
			if element.UserID != "" {
				textBuilder.WriteString("@")
				textBuilder.WriteString(element.UserID)
			}
		default:
			if value := strings.TrimSpace(stringPtrFromTextRun(item.TextRun)); value != "" {
				element.Text = value
				textBuilder.WriteString(value)
			}
		}
		elements = append(elements, element)
	}
	return strings.TrimSpace(textBuilder.String()), elements
}

func dedupeDriveFileCommentReplies(items []DriveFileCommentReplyItem) []DriveFileCommentReplyItem {
	if len(items) <= 1 {
		return items
	}
	seen := map[string]bool{}
	out := make([]DriveFileCommentReplyItem, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.ReplyID)
		if key == "" {
			out = append(out, item)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func replyListReplies(item *larkdrive.FileComment) []*larkdrive.FileCommentReply {
	if item == nil || item.ReplyList == nil {
		return nil
	}
	return item.ReplyList.Replies
}

func normalizeDriveFileCommentFileType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isSupportedDriveFileCommentFileType(value string) bool {
	switch normalizeDriveFileCommentFileType(value) {
	case larkdrive.FileTypeListFileCommentDoc,
		larkdrive.FileTypeListFileCommentDocx,
		larkdrive.FileTypeListFileCommentSheet,
		larkdrive.FileTypeListFileCommentFile,
		larkdrive.FileTypeListFileCommentSlides:
		return true
	default:
		return false
	}
}

func normalizeDriveFileCommentPageSize(value int) int {
	switch {
	case value <= 0:
		return defaultDriveFileCommentPageSize
	case value > maxDriveFileCommentPageSize:
		return maxDriveFileCommentPageSize
	default:
		return value
	}
}

func stringPtrFromTextRun(run *larkdrive.TextRun) string {
	if run == nil {
		return ""
	}
	return stringPtr(run.Text)
}

func stringPtrFromDocsLink(link *larkdrive.DocsLink) string {
	if link == nil {
		return ""
	}
	return stringPtr(link.Url)
}

func stringPtrFromPerson(person *larkdrive.Person) string {
	if person == nil {
		return ""
	}
	return stringPtr(person.UserId)
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
