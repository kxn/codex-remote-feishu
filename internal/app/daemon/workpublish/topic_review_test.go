package workpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func queueTaskReviewPublication(t *testing.T, store *workstore.Store, id string) {
	t.Helper()
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		if _, err := tx.GetTask("task"); errors.Is(err, workstore.ErrNotFound) {
			if err = tx.PutTask(work.Task{ID: "task", ScopeID: "scope", Title: "A task", OwnerRoleID: "role", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		projection := workservice.MessageProjection{RoleID: "role", GatewayRef: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "oc_chat", ChannelGeneration: 1, TaskID: "task", Body: "Task publication " + id}
		raw, err := json.Marshal(projection)
		if err != nil {
			return err
		}
		content := work.NewContent("body_"+id, "scope", "application/json", raw, time.Now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: "op_" + id, ScopeID: "scope", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "feishu.publish", TargetID: "oc_chat", TargetGeneration: 1, IdempotencyKey: id, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationPrepared, Revision: 1}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		return tx.PutOutbox(work.Outbox{ID: id, ScopeID: "scope", OperationID: op.ID, Target: "oc_chat", EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewConcurrentFirstTaskPublicationsReserveSingleRoot(t *testing.T) {
	store, _, p, _ := publisherFixture(t)
	queueTaskReviewPublication(t, store, "a")
	queueTaskReviewPublication(t, store, "b")
	type result struct {
		job publication
		err error
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, id := range []string{"a", "b"} {
		go func() { start.Wait(); j, e := p.claim(context.Background(), id, false); results <- result{j, e} }()
	}
	start.Done()
	r1, r2 := <-results, <-results
	winner, loser := r1, r2
	if winner.err != nil {
		winner, loser = r2, r1
	}
	if winner.err != nil || !errors.Is(loser.err, workstore.ErrConflict) {
		t.Fatalf("two first publications claimed: %#v %#v", r1.err, r2.err)
	}
	if winner.job.Projection.ReplyToMessageID != "" {
		t.Fatal("first publication already replies to a guessed root")
	}
	if err := p.finish(t.Context(), winner.job, feishu.ProjectPublishReceipt{MessageID: "actual-root", ChatID: "oc_chat"}, nil, false); err != nil {
		t.Fatal(err)
	}
	loserID := "a"
	if winner.job.Outbox.ID == loserID {
		loserID = "b"
	}
	follow, err := p.claim(t.Context(), loserID, false)
	if err != nil {
		t.Fatal(err)
	}
	if follow.Projection.RootMessageID != "actual-root" || follow.Projection.ReplyToMessageID != "actual-root" {
		t.Fatalf("queued publication made second root: %#v", follow.Projection)
	}
}

func TestReviewUnknownFirstTaskPublicationRetainsReservationAndFrozenPayload(t *testing.T) {
	store, _, p, _ := publisherFixture(t)
	queueTaskReviewPublication(t, store, "first")
	queueTaskReviewPublication(t, store, "second")
	first, err := p.claim(t.Context(), "first", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.finish(t.Context(), first, feishu.ProjectPublishReceipt{}, errors.New("response lost"), false); err != nil {
		t.Fatal(err)
	}
	if _, err = p.claim(t.Context(), "second", false); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("unknown root reservation released: %v", err)
	}
	// Even an independent authoritative receipt establishing the task root must
	// not alter the earlier uncertain send's UUID/body/reply target.
	if err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: "task", Tenant: "tenant", ChatID: "oc_chat", RootMessageID: "later-known-root", ChannelGeneration: 1, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	retried, err := p.claim(t.Context(), "first", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retried.Projection, first.Projection) {
		t.Fatalf("uncertain payload changed: %#v -> %#v", first.Projection, retried.Projection)
	}
}

type reviewCancelSender struct {
	cancel   context.CancelFunc
	requests []feishu.ProjectPublishRequest
	effects  map[string]bool
}

func (s *reviewCancelSender) Resolve(context.Context, work.RoleBotBinding) (Sender, error) {
	return s, nil
}
func (s *reviewCancelSender) PublishText(_ context.Context, req feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	s.requests = append(s.requests, req)
	s.effects[req.UUID] = true
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return feishu.ProjectPublishReceipt{MessageID: "actual-root", ChatID: req.ChatID}, nil
}
func TestReviewExternalSuccessWithLocalReceiptFailureCanRecoverSameSend(t *testing.T) {
	store, _, p, _ := publisherFixture(t)
	queueTaskReviewPublication(t, store, "first")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := &reviewCancelSender{cancel: cancel, effects: map[string]bool{}}
	p.resolver = sender
	if err := p.send(ctx, "first", false); err == nil {
		t.Fatal("receipt transaction unexpectedly survived canceled database context")
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox("first")
		if err != nil {
			return err
		}
		if item.State != work.OutboxSending || item.ExternalMessageID != "" {
			t.Fatalf("failed receipt transaction leaked sent state: %#v", item)
		}
		_, err = tx.GetTaskConversation("scope", "task")
		if !errors.Is(err, workstore.ErrNotFound) {
			t.Fatalf("failed receipt transaction leaked topic: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.RetryUnknown(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	if len(sender.requests) != 2 || sender.requests[0] != sender.requests[1] || len(sender.effects) != 1 {
		t.Fatalf("recovery changed exact send: %#v", sender.requests)
	}
	expected := sha256.Sum256([]byte("op_first"))
	if sender.requests[0].UUID != hex.EncodeToString(expected[:20]) {
		t.Fatal("UUID not operation-derived")
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox("first")
		if err != nil {
			return err
		}
		topic, err := tx.GetTaskConversation("scope", "task")
		if err != nil {
			return err
		}
		if item.State != work.OutboxSent || topic.RootMessageID != "actual-root" {
			t.Fatalf("receipt/topic not atomically recovered: %#v %#v", item, topic)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
