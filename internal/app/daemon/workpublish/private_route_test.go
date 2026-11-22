package workpublish

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func queuePrivateRoute(t *testing.T, store *workstore.Store, id string, projection workservice.MessageProjection, frozen bool) {
	t.Helper()
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		contextID, authority, err := testPublicationAuthority(tx, "scope", "role", projection.TaskID, id, "tenant", "oc_chat", 1)
		if err != nil {
			return err
		}
		projection.Authority = authority
		projection.RoleID, projection.GatewayRef, projection.AppID, projection.BotGeneration = "role", "gateway", "app", 1
		projection.Tenant, projection.ChatID = "tenant", "oc_chat"
		raw, err := json.Marshal(projection)
		if err != nil {
			return err
		}
		content := work.NewContent(id+":body", "scope", "application/json", raw, time.Now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: "op_" + id, ScopeID: "scope", ContextID: contextID, Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "feishu.publish", TargetID: "oc_chat", TargetGeneration: 1, IdempotencyKey: id, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationPrepared, Revision: 1}
		if frozen {
			op.Steps = []work.OperationStep{{ID: "send_projection", State: work.OperationConfirmed, Receipt: content.ID}}
		}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		return tx.PutOutbox(work.Outbox{ID: id, ScopeID: "scope", OperationID: op.ID, Target: "oc_chat", EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrivatePublicationFreezesTopLevelRouteAndKeepsSourceEvidence(t *testing.T) {
	for _, kind := range []string{"text", "card", "media"} {
		t.Run(kind, func(t *testing.T) {
			store, _, p, _ := publisherFixture(t)
			projection := workservice.MessageProjection{ReplyToMessageID: "historical-message", RootMessageID: "historical-root", Body: "result"}
			if kind == "card" {
				projection.Card = json.RawMessage(`{"schema":"2.0"}`)
			}
			if kind == "media" {
				projection.Media = &workservice.PrivateMediaPublication{Kind: "file", FileName: "result.txt", ContentRef: "media", SHA256: "digest"}
			}
			queuePrivateRoute(t, store, "private", projection, false)
			if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
				_, _, err := tx.AcceptPlatformObservation(workstore.PlatformObservation{EventID: "recall", Gateway: work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}, Tenant: "tenant", ChatID: "oc_chat", MessageID: "historical-root", Kind: "message.recalled", Facts: []byte(`{"recalled":true}`), ReceivedAt: time.Now()})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			first, err := p.claim(t.Context(), "private", false)
			if err != nil {
				t.Fatal(err)
			}
			if first.TerminalReason != "" || first.Projection.ReplyToMessageID != "" || first.Projection.RootMessageID != "" || first.ReplyInThread {
				t.Fatalf("private output retained historical topic: %+v", first)
			}
			if err = p.revalidateClaim(t.Context(), first); err != nil {
				t.Fatalf("effective private route lost authority: %v", err)
			}
			if err = store.Within(t.Context(), func(tx *workstore.Tx) error {
				body, e := tx.GetContent("scope", first.Outbox.PayloadRef)
				if e != nil {
					return e
				}
				var original workservice.MessageProjection
				if e = json.Unmarshal(body.Data, &original); e != nil {
					return e
				}
				if original.ReplyToMessageID != projection.ReplyToMessageID || original.RootMessageID != projection.RootMessageID {
					t.Fatal("source evidence was rewritten")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err = p.finish(t.Context(), first, feishu.ProjectPublishReceipt{}, errors.New("lost receipt"), false); err != nil {
				t.Fatal(err)
			}
			retry, err := p.claim(t.Context(), "private", true)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first.Projection, retry.Projection) {
				t.Fatal("unknown send changed frozen route")
			}
			if err = p.finish(t.Context(), retry, feishu.ProjectPublishReceipt{MessageID: "fresh-message", ChatID: "oc_chat"}, nil, false); err != nil {
				t.Fatal(err)
			}
			if err = store.Within(t.Context(), func(tx *workstore.Tx) error {
				mapping, e := tx.GetPlatformMessageMapping("tenant", "oc_chat", "fresh-message")
				if e == nil && mapping.RootMessageID != "fresh-message" {
					t.Fatalf("mapping inherited old root: %+v", mapping)
				}
				return e
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrivatePublicationPreservesPreviouslyFrozenReplyRoute(t *testing.T) {
	store, _, p, _ := publisherFixture(t)
	queuePrivateRoute(t, store, "legacy", workservice.MessageProjection{Body: "legacy", ReplyToMessageID: "legacy-parent", RootMessageID: "legacy-root"}, true)
	first, err := p.claim(t.Context(), "legacy", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Projection.ReplyToMessageID != "legacy-parent" || first.Projection.RootMessageID != "legacy-root" {
		t.Fatal("legacy frozen send was rerouted")
	}
	if err = p.finish(t.Context(), first, feishu.ProjectPublishReceipt{}, errors.New("lost receipt"), false); err != nil {
		t.Fatal(err)
	}
	retry, err := p.claim(t.Context(), "legacy", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Projection, retry.Projection) {
		t.Fatal("legacy unknown send changed route")
	}
}

func TestPrivateTaskPublicationsDoNotAcquireGroupTopic(t *testing.T) {
	store, _, p, _ := publisherFixture(t)
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.PutTask(work.Task{ID: "task", ScopeID: "scope", Title: "task", OwnerRoleID: "role", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutTaskConversation(work.TaskConversation{ScopeID: "scope", TaskID: "task", Tenant: "tenant", ChatID: "oc_chat", RootMessageID: "historical-task-root", ChannelGeneration: 1, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		queuePrivateRoute(t, store, id, workservice.MessageProjection{TaskID: "task", Body: "result", ReplyToMessageID: "historical-task-root", RootMessageID: "historical-task-root"}, false)
		job, err := p.claim(t.Context(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		if job.Projection.RootMessageID != "" || job.Projection.ReplyToMessageID != "" {
			t.Fatal("personal task inherited group topic")
		}
		if err = p.finish(t.Context(), job, feishu.ProjectPublishReceipt{MessageID: id + ":receipt", ChatID: "oc_chat"}, nil, false); err != nil {
			t.Fatal(err)
		}
	}
}
