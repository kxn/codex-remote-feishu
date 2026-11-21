package workservice

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type KnowledgeListRequest struct {
	Limit int    `json:"limit,omitempty"`
	After string `json:"after,omitempty"`
}
type KnowledgeObjectRequest struct {
	ID     string `json:"id"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type KnowledgePage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func knowledgeLimit(input int) (int, error) {
	if input == 0 {
		return 20, nil
	}
	if input < 1 || input > 100 {
		return 0, &workcmd.Error{Code: "invalid_argument", Message: "limit must be 1..100"}
	}
	return input, nil
}
func knowledgePage[T any](items []T, req KnowledgeListRequest, id func(T) string) (KnowledgePage[T], error) {
	limit, err := knowledgeLimit(req.Limit)
	page := KnowledgePage[T]{Items: []T{}}
	if err != nil {
		return page, err
	}
	sort.Slice(items, func(i, j int) bool { return id(items[i]) < id(items[j]) })
	for _, item := range items {
		if id(item) <= req.After {
			continue
		}
		if len(page.Items) == limit {
			page.NextCursor = id(page.Items[len(page.Items)-1])
			break
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}
func callerRead(c Caller) work.ReadScope {
	return work.ReadScope{ScopeID: c.Worker.ScopeID, RoleID: c.Worker.RoleID, ContextID: c.Worker.ContextID, TaskID: c.Attempt.TaskID}
}
func callerSource(c Caller) work.SourceRef {
	return work.SourceRef{Kind: "attempt", ID: c.Attempt.ID, Version: strconv.FormatInt(c.Attempt.RunRef.Generation, 10), EvidenceRef: "run:" + c.Attempt.RunRef.RunID}
}

// CurrentHumanInstruction requires the exact message to have been admitted to
// this run. A convenient old message, private note from another person, or role
// message cannot manufacture human authorization.
func currentHumanInstruction(tx *workstore.Tx, c Caller, messageID string) (work.Message, work.Inbox, error) {
	if messageID == "" {
		return work.Message{}, work.Inbox{}, workauth.ErrUnauthorized
	}
	for _, id := range c.Attempt.InputIDs {
		input, err := tx.GetInbox(id)
		if err != nil {
			return work.Message{}, work.Inbox{}, err
		}
		if input.MessageID != messageID {
			continue
		}
		if input.State != work.InboxClaimed || input.ClaimedAttemptID != c.Attempt.ID || input.ScopeID != c.Worker.ScopeID {
			return work.Message{}, work.Inbox{}, workauth.ErrUnauthorized
		}
		message, err := tx.GetMessage(messageID)
		if err != nil {
			return work.Message{}, work.Inbox{}, err
		}
		if message.RecalledEvidenceRef != "" || message.ScopeID != c.Worker.ScopeID || message.Source.Kind != work.ActorPrincipal {
			return work.Message{}, work.Inbox{}, workauth.ErrUnauthorized
		}
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return work.Message{}, work.Inbox{}, err
		}
		if scope.Kind == work.ScopePersonal && message.Source.ID != scope.PrincipalID {
			return work.Message{}, work.Inbox{}, workauth.ErrUnauthorized
		}
		return message, input, nil
	}
	return work.Message{}, work.Inbox{}, fmt.Errorf("human instruction was not admitted to this run: %w", workauth.ErrUnauthorized)
}

func knowledgeContentPage(body work.Content, offset, limit int) (ContentPage, error) {
	if limit == 0 {
		limit = 8000
	}
	if offset < 0 || limit < 1 || limit > 8000 {
		return ContentPage{}, &workcmd.Error{Code: "invalid_argument", Message: "offset must be nonnegative; limit must be 1..8000 characters"}
	}
	if !utf8.Valid(body.Data) || (!strings.HasPrefix(body.MediaType, "text/") && !strings.HasPrefix(body.MediaType, "application/json")) {
		return ContentPage{}, fmt.Errorf("content is not readable text")
	}
	runes := []rune(string(body.Data))
	if offset > len(runes) {
		return ContentPage{}, fmt.Errorf("offset exceeds content length")
	}
	end := offset + limit
	if end > len(runes) {
		end = len(runes)
	}
	page := ContentPage{ID: body.ID, SHA256: body.SHA256, MediaType: body.MediaType, Text: string(runes[offset:end]), Offset: offset, Total: len(runes)}
	if end < len(runes) {
		page.NextOffset = &end
	}
	return page, nil
}
