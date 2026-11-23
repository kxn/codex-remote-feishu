package privatechat

import "testing"

func TestProjectShareDraftIsolationIdempotencyAndRestart(t *testing.T) {
	ctx := t.Context()
	j, owner, path := memoryJournalFixture(t)
	draft, err := j.PrepareProjectShare(ctx, owner, 1, "one", ProjectShareDraft{ProjectID: "project", Text: "specific text"})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := j.PrepareProjectShare(ctx, owner, 1, "one", ProjectShareDraft{ProjectID: "project", Text: "specific text"})
	if err != nil || draft.ID != replay.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if _, err = j.PrepareProjectShare(ctx, owner, 1, "one", ProjectShareDraft{ProjectID: "project", Text: "changed"}); err == nil {
		t.Fatal("operation accepted changed draft")
	}
	foreign := owner
	foreign.UserID = "other"
	if _, err = j.ProjectShareDraft(ctx, foreign, 1, draft.ID); err == nil {
		t.Fatal("foreign owner read draft")
	}
	if _, err = j.ProjectShareDraft(ctx, owner, 2, draft.ID); err == nil {
		t.Fatal("foreign epoch read draft")
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if _, err = j.ProjectShareDraft(ctx, owner, 1, draft.ID); err != nil {
		t.Fatal(err)
	}
	if err = j.RecordProjectShareReceipt(ctx, owner, 1, draft.ID, []byte(`{"id":"receipt"}`)); err != nil {
		t.Fatal(err)
	}
	if err = j.RecordProjectShareReceipt(ctx, owner, 1, draft.ID, []byte(`{"id":"receipt"}`)); err != nil {
		t.Fatal(err)
	}
	if err = j.RecordProjectShareReceipt(ctx, owner, 1, draft.ID, []byte(`{"id":"different"}`)); err == nil {
		t.Fatal("receipt changed on replay")
	}
	drafts, _, err := j.ListProjectShareDrafts(ctx, owner, 1, "", 20)
	if err != nil || len(drafts) != 0 {
		t.Fatalf("completed draft remains: %+v %v", drafts, err)
	}
}
