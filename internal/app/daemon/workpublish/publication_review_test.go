package workpublish

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestReviewUnknownPublicationDoesNotBlockOtherJobs(t *testing.T) {
	store, sender, publisher, _ := publisherFixture(t)
	sender.failFirst = true
	queuePublication(t, store, "a_unknown", 1)
	queuePublication(t, store, "b_valid", 1)
	if _, err := publisher.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 2 || sender.effects != 2 {
		t.Fatalf("independent job blocked by uncertain send: calls=%d effects=%d", sender.calls, sender.effects)
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		unknown, err := tx.GetOutbox("a_unknown")
		if err != nil {
			return err
		}
		valid, err := tx.GetOutbox("b_valid")
		if err != nil {
			return err
		}
		if unknown.State != work.OutboxUnknown || valid.State != work.OutboxSent {
			t.Fatalf("unknown/sent facts collapsed: %s %s", unknown.State, valid.State)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 2 {
		t.Fatal("uncertain job automatically replayed")
	}
}
