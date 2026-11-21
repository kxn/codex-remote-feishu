package workpublish

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

type topicReadSender struct {
	fakePublisher
	reads   int
	result  feishu.ObservedPublication
	readErr error
}

func (s *topicReadSender) Resolve(context.Context, work.RoleBotBinding) (Sender, error) {
	return s, nil
}
func (s *topicReadSender) ReadPublishedMessage(_ context.Context, id string) (feishu.ObservedPublication, error) {
	s.reads++
	return s.result, s.readErr
}
func TestTopicLossRequiresExactReadNotPermissionErrorAndNeverSendsAgain(t *testing.T) {
	store, _, _, clock := publisherFixture(t)
	sender := &topicReadSender{readErr: errors.New("permission denied")}
	p, err := New(store, sender, func() time.Time { return *clock })
	if err != nil {
		t.Fatal(err)
	}
	queueTaskReviewPublication(t, store, "initial")
	if err = p.send(t.Context(), "initial", false); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		err := store.Within(t.Context(), func(tx *workstore.Tx) error {
			topic, e := tx.GetTaskConversation("scope", "task")
			if e != nil {
				return e
			}
			lost, e := tx.TaskTopicUnavailable(topic)
			if e != nil {
				return e
			}
			if lost != want {
				t.Fatalf("topic unavailable=%v want %v", lost, want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = p.ObserveTaskTopic(t.Context(), "scope", "task"); err == nil {
		t.Fatal("read error lost")
	}
	check(false)
	// The failed read is durably throttled; no tight loop or inferred deletion.
	if err = p.ObserveTaskTopic(t.Context(), "scope", "task"); err != nil {
		t.Fatal(err)
	}
	if sender.reads != 1 {
		t.Fatal("read retry not bounded")
	}
	*clock = clock.Add(time.Minute)
	sender.readErr = nil
	sender.result = feishu.ObservedPublication{MessageID: "wrong-message", ChatID: "oc_chat", Deleted: true}
	if err = p.ObserveTaskTopic(t.Context(), "scope", "task"); err == nil {
		t.Fatal("wrong message treated as missing root")
	}
	check(false)
	*clock = clock.Add(time.Minute)
	sender.result = feishu.ObservedPublication{MessageID: "om_receipt", ChatID: "oc_chat", Deleted: true}
	if err = p.ObserveTaskTopic(t.Context(), "scope", "task"); err != nil {
		t.Fatal(err)
	}
	check(true)
	queueTaskReviewPublication(t, store, "after-loss")
	job, err := p.claim(t.Context(), "after-loss", false)
	if err != nil {
		t.Fatal(err)
	}
	if job.TerminalReason == "" {
		t.Fatal("would send to root known deleted by actual read")
	}
	if sender.calls != 1 {
		t.Fatal("read observation performed an external write")
	}
}
