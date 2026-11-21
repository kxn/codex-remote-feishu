package workservice

import (
	"context"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestInstructionRevisionAndExactAttemptInput(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	first, err := s.BuildInstructions(ctx, "context")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Text, token) {
		t.Fatal("credential leaked into model instructions")
	}
	if !strings.Contains(first.Text, "--run-ref") || !strings.Contains(first.Text, "outcome.report") {
		t.Fatal("missing bounded collaboration contract")
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		role, err := tx.GetRole("leader")
		if err != nil {
			return err
		}
		role.Description = "Coordinate engineering and design"
		role.DescriptionVersion++
		role.Revision++
		return tx.PutRole(role, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.BuildInstructions(ctx, "context")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision == first.Revision {
		t.Fatal("role change did not invalidate frozen bundle")
	}
	if _, err = s.SendMessage(ctx, token, run, SendMessageRequest{Key: "late", RecipientRoleID: "leader", ConversationID: "discussion", Body: "late incoming requirement"}); err != nil {
		t.Fatal(err)
	}
	var attempt work.ExecutionAttempt
	err = s.store.Within(ctx, func(tx *workstore.Tx) error { var e error; attempt, e = tx.GetAttempt("attempt"); return e })
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := s.BuildWorkInput(ctx, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "late incoming requirement") {
		t.Fatal("late input silently inserted into old run")
	}
	view, err := s.Context(ctx, token, run)
	if err != nil {
		t.Fatal(err)
	}
	if view.PendingInputCount != 1 || len(view.Inbox) != 0 {
		t.Fatalf("pending input confused with admitted input: %+v", view)
	}
}

func TestContentPaginationUsesCharactersAndScope(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PutContent(work.NewContent("body", "scope", "text/plain", []byte("中文abc"), s.now()))
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.ReadContent(ctx, token, run, ContentReadRequest{ID: "body", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Text != "中文" || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatalf("page=%+v", page)
	}
	page, err = s.ReadContent(ctx, token, run, ContentReadRequest{ID: "body", Offset: 2, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Text != "abc" || page.NextOffset != nil {
		t.Fatalf("page=%+v", page)
	}
}
