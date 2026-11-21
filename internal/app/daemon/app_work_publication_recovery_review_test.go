package daemon

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func TestReviewActualRecoveryLaneOffersRootActionOnceAndSkipsSealedUnknown(t *testing.T) {
	a := teamProjectViewFixture(t)
	err := a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		if e := tx.PutTask(work.Task{ID: "task", ScopeID: "scope", Title: "Actual task", OwnerRoleID: "leader", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0); e != nil {
			return e
		}
		raw, _ := json.Marshal(workservice.MessageProjection{RoleID: "leader", GatewayRef: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", ChannelGeneration: 1, TaskID: "task", Body: "Actual progress"})
		content := work.NewContent("lost:request", "scope", "application/json", raw, time.Now())
		if e := tx.PutContent(content); e != nil {
			return e
		}
		op := work.Operation{ID: "lost", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorRole, ID: "leader"}, Kind: "feishu.publish", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: "lost", RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if _, _, e := tx.PutOperation(op, 0); e != nil {
			return e
		}
		out := work.Outbox{ID: "lost-out", ScopeID: "scope", OperationID: op.ID, Target: "chat", EventID: "lost", PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}
		if e := tx.PutOutbox(out, 0); e != nil {
			return e
		}
		if e := tx.TryReserveTaskTopic("scope", "task", op.ID); e != nil {
			return e
		}
		op.State = work.OperationSending
		op.Revision++
		op.Steps = []work.OperationStep{{ID: "send_projection", State: work.OperationConfirmed, Receipt: content.ID}}
		if _, _, e := tx.PutOperation(op, 1); e != nil {
			return e
		}
		op.State = work.OperationUnknown
		op.Revision++
		if _, _, e := tx.PutOperation(op, 2); e != nil {
			return e
		}
		out.State = work.OutboxSending
		out.Revision++
		if e := tx.PutOutbox(out, 1); e != nil {
			return e
		}
		out.State = work.OutboxUnknown
		out.Revision++
		return tx.PutOutbox(out, 2)
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err = a.runTeamPublicationRecoveryViews(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	err = a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e != nil {
			return e
		}
		if len(views) != 1 || views[0].Kind != "publication.recovery" {
			t.Fatalf("recovery projection not deduplicated: %+v", views)
		}
		content, e := tx.GetContent("scope", views[0].BodyRef)
		if e != nil {
			return e
		}
		if !strings.Contains(string(content.Data), "publication.reanchor") {
			t.Fatal("actual recovery card offers no continuation action")
		}
		outboxes, e := tx.ListOutbox("scope")
		if e != nil {
			return e
		}
		if len(outboxes) != 2 {
			t.Fatalf("recovery loop created%d outboxes", len(outboxes))
		}
		for _, out := range outboxes {
			if out.ID != "lost-out" {
				body, e := tx.GetContent("scope", out.PayloadRef)
				if e != nil {
					return e
				}
				var p workservice.MessageProjection
				if e = json.Unmarshal(body.Data, &p); e != nil {
					return e
				}
				if p.ReplyToMessageID != "" {
					t.Fatal("recovery card replies to missing original root")
				}
			}
		}
		op, e := tx.GetOperation("lost")
		if e != nil {
			return e
		}
		expected := op.Revision
		op.Revision++
		op.Steps = append(op.Steps, work.OperationStep{ID: "topic_reanchored", State: work.OperationConfirmed, Receipt: "actual-human-reanchor-audit"})
		_, _, e = tx.PutOperation(op, expected)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.runTeamPublicationRecoveryViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews("scope")
		if e == nil && len(views) != 1 {
			t.Fatal("sealed unknown produced another recovery card")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
