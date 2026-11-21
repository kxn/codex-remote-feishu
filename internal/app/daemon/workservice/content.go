package workservice

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type ContentReadRequest struct {
	ID     string `json:"id"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}
type ContentPage struct {
	ID         string `json:"id"`
	SHA256     string `json:"sha256"`
	MediaType  string `json:"media_type"`
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	NextOffset *int   `json:"next_offset,omitempty"`
	Total      int    `json:"total"`
}

func (s *Service) ReadContent(ctx context.Context, token, runRef string, req ContentReadRequest) (ContentPage, error) {
	var page ContentPage
	if req.Limit == 0 {
		req.Limit = 8000
	}
	if req.Offset < 0 || req.Limit < 1 || req.Limit > 8000 {
		return page, &workcmd.Error{Code: "invalid_argument", Message: "offset must be nonnegative; limit must be 1..8000 characters"}
	}
	err := s.withContentWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		body, err := tx.GetContent(c.Worker.ScopeID, req.ID)
		if err != nil {
			return err
		}
		if !utf8.Valid(body.Data) || (!strings.HasPrefix(body.MediaType, "text/") && !strings.HasPrefix(body.MediaType, "application/json")) {
			return fmt.Errorf("content is not readable text")
		}
		runes := []rune(string(body.Data))
		if req.Offset > len(runes) {
			return &workcmd.Error{Code: "invalid_argument", Message: "offset exceeds content length"}
		}
		end := req.Offset + req.Limit
		if end > len(runes) {
			end = len(runes)
		}
		page = ContentPage{ID: body.ID, SHA256: body.SHA256, MediaType: body.MediaType, Text: string(runes[req.Offset:end]), Offset: req.Offset, Total: len(runes)}
		if end < len(runes) {
			page.NextOffset = &end
		}
		return nil
	})
	return page, err
}
