package workpublish

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type fakePublisher struct {
	calls, effects int
	failFirst      bool
	uuids          map[string]bool
}

func (f *fakePublisher) Resolve(context.Context, work.RoleBotBinding) (Sender, error) { return f, nil }
func (f *fakePublisher) PublishText(_ context.Context, req feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	f.calls++
	if f.uuids == nil {
		f.uuids = map[string]bool{}
	}
	if !f.uuids[req.UUID] {
		f.effects++
		f.uuids[req.UUID] = true
	}
	if f.failFirst && f.calls == 1 {
		return feishu.ProjectPublishReceipt{}, errors.New("response lost after external send")
	}
	return feishu.ProjectPublishReceipt{MessageID: "om_receipt", ChatID: req.ChatID}, nil
}

func publisherFixture(t *testing.T) (*workstore.Store, *fakePublisher, *Publisher, *time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC)
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "role", Name: "Leader", Description: "Coordinate", Revision: 1, DescriptionVersion: 1, SettingsRevision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		return tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: "human", Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakePublisher{}
	p, err := New(store, sender, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return store, sender, p, &now
}
func queuePublication(t *testing.T, store *workstore.Store, id string, generation int64) {
	t.Helper()
	err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		contextID, authority, err := testPublicationAuthority(tx, "scope", "role", "", id, "tenant", "oc_chat", generation)
		if err != nil {
			return err
		}
		projection := workservice.MessageProjection{Authority: authority, RoleID: "role", GatewayRef: "gateway", AppID: "app", BotGeneration: generation, Tenant: "tenant", ChatID: "oc_chat", Body: "A useful result"}
		data, err := json.Marshal(projection)
		if err != nil {
			return err
		}
		content := work.NewContent("body_"+id, "scope", "application/json", data, time.Now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: "op_" + id, ScopeID: "scope", ContextID: contextID, Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "feishu.publish", TargetID: "oc_chat", TargetGeneration: generation, IdempotencyKey: id, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationPrepared, Revision: 1}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		return tx.PutOutbox(work.Outbox{ID: id, ScopeID: "scope", OperationID: op.ID, Target: "oc_chat", EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnknownPublicationIsNotAutomaticallyRepeatedAndRecoveryKeepsUUID(t *testing.T) {
	store, sender, p, now := publisherFixture(t)
	sender.failFirst = true
	queuePublication(t, store, "a", 1)
	if _, err := p.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatalf("unknown send automatically retried %d times", sender.calls)
	}
	*now = now.Add(time.Minute)
	if err := p.RetryUnknown(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 2 || sender.effects != 1 {
		t.Fatalf("calls=%d effects=%d", sender.calls, sender.effects)
	}
	err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox("a")
		if err != nil {
			return err
		}
		if item.State != work.OutboxSent || item.ExternalMessageID != "om_receipt" {
			t.Fatalf("no real receipt: %+v", item)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOldUnknownPublicationCannotEscapePlatformDedupWindow(t *testing.T) {
	store, sender, p, now := publisherFixture(t)
	sender.failFirst = true
	queuePublication(t, store, "a", 1)
	if _, err := p.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	if err := p.RetryUnknown(context.Background(), "a"); err == nil {
		t.Fatal("uncertain effect blindly resent after UUID expiry")
	}
	if sender.calls != 1 {
		t.Fatalf("external calls=%d", sender.calls)
	}
}

func TestStaleIdentityStopsItsPublicationWithoutBlockingAnother(t *testing.T) {
	store, sender, p, _ := publisherFixture(t)
	queuePublication(t, store, "a_stale", 1)
	err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 2}, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	queuePublication(t, store, "b_valid", 2)
	if _, err = p.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatalf("wrong identity sent or valid work stalled: calls=%d", sender.calls)
	}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox("a_stale")
		if err != nil {
			return err
		}
		if item.State != work.OutboxTerminal {
			t.Fatalf("stale publication not stopped: %+v", item)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
