package workstore

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPublicationQueriesFilterQueueAndExactRoleStream(t *testing.T) {
	store, _ := openTestStore(t)
	seedPersonal(t, store)
	base := queryPublicationFixture{
		scope: "s", actor: work.Actor{Kind: work.ActorRole, ID: "r"},
		kind: "feishu.publish", outbox: work.OutboxPending, operation: work.OperationPrepared,
		tenant: "tenant", chat: "chat", root: "main-root", purpose: "progress",
	}
	var fixtures []queryPublicationFixture
	add := func(id string, change func(*queryPublicationFixture)) {
		value := base
		value.id = id
		if change != nil {
			change(&value)
		}
		fixtures = append(fixtures, value)
	}
	add("a-pending", nil)
	add("b-retryable-final", func(v *queryPublicationFixture) {
		v.purpose, v.outbox, v.operation = "final", work.OutboxRetryable, work.OperationRetryable
	})
	add("c-sent", func(v *queryPublicationFixture) {
		v.outbox, v.operation = work.OutboxSent, work.OperationConfirmed
	})
	add("d-task-sent-old-root", func(v *queryPublicationFixture) {
		v.task, v.root, v.outbox, v.operation = "task", "old-topic", work.OutboxSent, work.OperationConfirmed
	})
	add("e-task-unknown-new-root", func(v *queryPublicationFixture) {
		v.task, v.root, v.outbox, v.operation = "task", "new-topic", work.OutboxUnknown, work.OperationUnknown
	})
	add("f-system", func(v *queryPublicationFixture) {
		v.actor = work.Actor{Kind: work.ActorSystem, ID: "r"}
	})
	add("g-other-kind", func(v *queryPublicationFixture) { v.kind = "other.publish" })
	add("h-other-role", func(v *queryPublicationFixture) { v.actor.ID = "expert" })
	add("i-other-tenant", func(v *queryPublicationFixture) { v.tenant = "other-tenant" })
	add("j-other-chat", func(v *queryPublicationFixture) { v.chat = "other-chat" })
	add("k-other-root", func(v *queryPublicationFixture) { v.root = "other-root" })
	add("l-empty-root", func(v *queryPublicationFixture) { v.root = "" })
	add("m-reply", func(v *queryPublicationFixture) { v.purpose = "reply" })
	add("n-terminal", func(v *queryPublicationFixture) {
		v.outbox, v.operation = work.OutboxTerminal, work.OperationTerminal
	})
	add("o-other-scope", func(v *queryPublicationFixture) { v.scope = "second-scope" })
	add("p-other-payload", func(v *queryPublicationFixture) { v.otherPayload = true })
	add("q-invalid-json", func(v *queryPublicationFixture) { v.invalidJSON = true })
	requireTx(t, store, func(tx *Tx) error {
		if err := tx.PutScope(work.WorkScope{ID: "second-scope", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); err != nil {
			return err
		}
		for _, fixture := range fixtures {
			if err := fixture.store(tx); err != nil {
				return err
			}
		}
		pending, err := tx.ListPendingPublicationOutbox(base.scope)
		if err != nil {
			return err
		}
		var pendingIDs []string
		for _, item := range pending {
			pendingIDs = append(pendingIDs, item.ID)
		}
		want := []string{
			"a-pending", "b-retryable-final", "f-system", "h-other-role", "i-other-tenant",
			"j-other-chat", "k-other-root", "l-empty-root", "m-reply", "p-other-payload", "q-invalid-json",
		}
		if !slices.Equal(pendingIDs, want) {
			t.Fatalf("pending publications = %v, want %v", pendingIDs, want)
		}
		tests := []struct {
			task, root string
			want       []string
		}{
			{root: "main-root", want: []string{"a-pending", "b-retryable-final", "c-sent"}},
			{task: "task", root: "irrelevant-root", want: []string{"d-task-sent-old-root", "e-task-unknown-new-root"}},
			{want: []string{"l-empty-root"}},
		}
		for _, tt := range tests {
			rows, err := tx.ListRoleProgressPublications(base.scope, base.actor.ID, base.tenant, base.chat, tt.task, tt.root)
			if err != nil {
				return err
			}
			var ids []string
			for _, row := range rows {
				ids = append(ids, row.Outbox.ID)
				if row.Operation.ID != row.Outbox.OperationID || row.Content.ID != row.Outbox.PayloadRef ||
					row.Content.ID != row.Operation.RequestRef || row.Content.ScopeID != base.scope || row.Content.CreatedAt.IsZero() {
					t.Fatalf("query did not return a complete same-scope record: %+v", row)
				}
			}
			if !slices.Equal(ids, tt.want) {
				t.Fatalf("task=%q root=%q returned %v, want %v", tt.task, tt.root, ids, tt.want)
			}
		}
		return nil
	})
}

func TestRoleProgressPublicationRejectsMismatchedBodyDigest(t *testing.T) {
	store, _ := openTestStore(t)
	seedPersonal(t, store)
	requireTx(t, store, func(tx *Tx) error {
		fixture := queryPublicationFixture{
			id: "damaged", scope: "s", actor: work.Actor{Kind: work.ActorRole, ID: "r"},
			kind: "feishu.publish", outbox: work.OutboxPending, operation: work.OperationPrepared,
			tenant: "tenant", chat: "chat", purpose: "progress",
		}
		if err := fixture.store(tx); err != nil {
			return err
		}
		if _, err := tx.tx.ExecContext(tx.ctx, `UPDATE content SET sha256='unexpected' WHERE id='damaged-body'`); err != nil {
			return err
		}
		_, err := tx.ListRoleProgressPublications("s", "r", "tenant", "chat", "", "")
		if err == nil || !strings.Contains(err.Error(), "stored content integrity: content digest mismatch") {
			t.Fatalf("query returned unchecked publication content: %v", err)
		}
		return nil
	})
}

func TestPublicationQueryIndexInstalledOnExistingStore(t *testing.T) {
	store, path := openTestStore(t)
	requireTx(t, store, func(tx *Tx) error {
		_, err := tx.tx.ExecContext(tx.ctx, `DROP INDEX outbox_operation_scope`)
		return err
	})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	requireTx(t, reopened, func(tx *Tx) error {
		var name string
		return tx.tx.QueryRowContext(tx.ctx, `SELECT name FROM sqlite_master WHERE type='index' AND name='outbox_operation_scope'`).Scan(&name)
	})
}

type queryPublicationFixture struct {
	id, scope, kind, tenant, chat, task, root, purpose string
	actor                                              work.Actor
	outbox                                             work.OutboxState
	operation                                          work.OperationState
	otherPayload, invalidJSON                          bool
}

func (v queryPublicationFixture) store(tx *Tx) error {
	projection := map[string]string{"role_id": v.actor.ID, "purpose": v.purpose, "tenant": v.tenant, "chat_id": v.chat}
	if v.root != "" {
		projection["root_message_id"] = v.root
	}
	if v.task != "" {
		projection["task_id"] = v.task
	}
	data, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	if v.invalidJSON {
		data = []byte("not JSON")
	}
	body := work.NewContent(v.id+"-body", v.scope, "application/json", data, time.Now().UTC())
	if err = tx.PutContent(body); err != nil {
		return err
	}
	op := work.Operation{ID: v.id + "-op", ScopeID: v.scope, Actor: v.actor, Kind: v.kind, TargetID: v.chat, TargetGeneration: 1, IdempotencyKey: v.id, RequestRef: body.ID, RequestHash: body.SHA256, State: v.operation, Revision: 1}
	if op.State == work.OperationConfirmed {
		op.ExternalReceipt = "test operation receipt"
	}
	if _, _, err = tx.PutOperation(op, 0); err != nil {
		return err
	}
	item := work.Outbox{ID: v.id, ScopeID: v.scope, OperationID: op.ID, Target: v.chat, EventID: v.id, PayloadRef: body.ID, State: v.outbox, Revision: 1}
	if item.State == work.OutboxSent {
		item.ExternalMessageID = "message-" + v.id
	}
	if v.otherPayload {
		other := work.NewContent(v.id+"-other-body", v.scope, body.MediaType, data, time.Now().UTC())
		if err := tx.PutContent(other); err != nil {
			return err
		}
		item.PayloadRef = other.ID
	}
	return tx.PutOutbox(item, 0)
}
