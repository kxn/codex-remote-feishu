package workpublish

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestHumanReanchorPreservesUnknownAndUnblocksOnlyLaterPublications(t *testing.T) {
	store, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var interaction workstore.HumanInteraction
	binding := work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		if e := tx.PutRole(work.Role{ID: "role", Name: "Lead", Description: "Coordinate", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRoleBotBinding(binding, 0); e != nil {
			return e
		}
		var e error
		interaction, _, e = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "click", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "person"}, ChatID: "oc_chat", MessageID: "om_receipt", Kind: "card", Values: map[string]any{"team_view": "recovery", "action": "publication.reanchor"}, ReceivedAt: time.Now()}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if e != nil {
			return e
		}
		if err := putFixtureTeam(tx); err != nil {
			return err
		}
		scope := work.WorkScope{ID: "scope", Kind: work.ScopeProject, TeamID: "team", ProjectID: "project", Revision: 1}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		if err := putFixtureProject(tx, scope); err != nil {
			return err
		}
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "grant-source", Tenant: "tenant", ChatID: "oc_chat", ChatType: "group", MessageID: "grant-source", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "person"}, MessageType: "text", RawContent: `{"text":"participate"}`, MessageCreatedAt: time.Now(), ReceivedAt: time.Now()}
		record, _, err := tx.AcceptRawIngress(raw, gateway)
		if err != nil {
			return err
		}
		return tx.GrantProjectAccessFromIngress(scope.ID, interaction.PrincipalID, record.ID, time.Now().Add(time.Hour))
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(store, &cardTestSender{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	queueTaskReviewPublication(t, store, "first")
	queueTaskReviewPublication(t, store, "second")
	first, err := p.claim(t.Context(), "first", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.finish(t.Context(), first, feishu.ProjectPublishReceipt{}, errors.New("lost receipt"), false); err != nil {
		t.Fatal(err)
	}
	var req ReanchorRequest
	var view workstore.UIView
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation("op_first")
		if e != nil {
			return e
		}
		item, e := tx.GetOutbox("first")
		if e != nil {
			return e
		}
		req = ReanchorRequest{InteractionID: interaction.ID, ViewID: "recovery", OutboxID: "first", OperationID: op.ID, TaskID: "task", OperationRevision: op.Revision, OutboxRevision: item.Revision}
		card, e := workui.Card("recovery", workui.Panel{Title: "恢复", Buttons: []workui.Button{{Label: "从这里继续后续进展", Action: "publication.reanchor"}}})
		if e != nil {
			return e
		}
		view, e = workui.Queue(tx, workstore.UIView{ID: "recovery", ScopeID: "scope", SurfaceKey: "recovery-first", Kind: "publication.recovery", TargetID: "first", TargetRevision: op.Revision, Binding: binding, Tenant: "tenant", ChatID: "oc_chat", PrincipalID: interaction.PrincipalID, ChannelGeneration: 1}, card)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Reanchor(t.Context(), req); err == nil {
		t.Fatal("unpublished recovery card acquired topic")
	}
	if err = p.send(t.Context(), view.ID+":outbox", false); err != nil {
		t.Fatal(err)
	}
	stale := req
	stale.OperationRevision++
	if err = p.Reanchor(t.Context(), stale); err == nil {
		t.Fatal("stale target accepted")
	}
	if err = p.Reanchor(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if err = p.Reanchor(t.Context(), req); err != nil {
		t.Fatalf("replayed click: %v", err)
	}
	if err = p.RetryUnknown(t.Context(), "first"); err == nil {
		t.Fatal("reanchored unknown send replayed")
	}
	if err = p.finish(t.Context(), first, feishu.ProjectPublishReceipt{MessageID: "late-old-root", ChatID: "oc_chat"}, nil, false); err == nil {
		t.Fatal("late original result overwrote new root")
	}
	next, err := p.claim(t.Context(), "second", false)
	if err != nil {
		t.Fatal(err)
	}
	if next.Projection.RootMessageID != "om_receipt" || next.Projection.ReplyToMessageID != "om_receipt" {
		t.Fatalf("next update ignored chosen root: %#v", next.Projection)
	}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation("op_first")
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatal("original send misrepresented")
		}
		sealed, e := tx.PublicationWasReanchored(op)
		if e != nil || !sealed {
			t.Fatalf("real reanchored publication remains unresolved for archive: %v", e)
		}
		forged := op
		forged.ID = "other-publication"
		if sealed, e = tx.PublicationWasReanchored(forged); e == nil && sealed {
			t.Fatal("another operation borrowed human reanchor audit")
		}
		forged = op
		forged.ScopeID = "another-scope"
		if sealed, e = tx.PublicationWasReanchored(forged); e == nil && sealed {
			t.Fatal("crossscope human reanchor audit accepted")
		}
		reservation, e := tx.GetTaskTopicReservation("scope", "task")
		if e != nil {
			return e
		}
		if reservation != "op_first" {
			t.Fatal("lost original reservation evidence")
		}
		mapping, e := tx.GetPlatformMessageMapping("tenant", "oc_chat", "om_receipt")
		if e != nil {
			return e
		}
		if mapping.OperationID != view.OperationID {
			t.Fatal("rewrote card receipt")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	exerciseRecalledRootRecovery(t, store, p, next)
}

func exerciseRecalledRootRecovery(t *testing.T, store *workstore.Store, p *Publisher, next publication) {
	t.Helper()
	if err := p.finish(t.Context(), next, feishu.ProjectPublishReceipt{MessageID: "second-result", ChatID: "oc_chat"}, nil, false); err != nil {
		t.Fatal(err)
	}
	queueTaskReviewPublication(t, store, "third")
	var req ReanchorRequest
	var view workstore.UIView
	err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		// The real recall can arrive for a Bot publication that never had a
		// canonical human Message row. Its authenticated tombstone still applies.
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
		if _, _, e := tx.AcceptPlatformObservation(workstore.PlatformObservation{EventID: "recall-root", Gateway: gateway, Tenant: "tenant", ChatID: "oc_chat", MessageID: "om_receipt", Kind: "message.recalled", Facts: []byte(`{"recalled":true}`), ReceivedAt: time.Now()}); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := p.claim(t.Context(), "third", false)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.TerminalReason == "" {
		t.Fatal("attempted network send to recalled root")
	}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation("op_third")
		if e != nil {
			return e
		}
		item, e := tx.GetOutbox("third")
		if e != nil {
			return e
		}
		interaction, _, e := tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "second-click", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "person"}, ChatID: "oc_chat", MessageID: "replacement-root", Kind: "card", Values: map[string]any{"team_view": "recovery2", "action": "publication.reanchor"}, ReceivedAt: time.Now()}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if e != nil {
			return e
		}
		req = ReanchorRequest{InteractionID: interaction.ID, ViewID: "recovery2", OutboxID: "third", OperationID: op.ID, TaskID: "task", OperationRevision: op.Revision, OutboxRevision: item.Revision, TopicRevision: 1}
		card, e := workui.Card("recovery2", workui.Panel{Title: "话题已撤回"})
		if e != nil {
			return e
		}
		view, e = workui.Queue(tx, workstore.UIView{ID: "recovery2", ScopeID: "scope", SurfaceKey: "recovery-third", Kind: "publication.recovery", TargetID: "third", TargetRevision: op.Revision, Binding: work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "oc_chat", PrincipalID: interaction.PrincipalID, ChannelGeneration: 1}, card)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	cardJob, err := p.claim(t.Context(), view.ID+":outbox", false)
	if err != nil {
		t.Fatal(err)
	}
	if cardJob.Projection.ReplyToMessageID != "" {
		t.Fatal("recovery attempted reply to lost root")
	}
	if err = p.finish(t.Context(), cardJob, feishu.ProjectPublishReceipt{MessageID: "replacement-root", ChatID: "oc_chat"}, nil, false); err != nil {
		t.Fatal(err)
	}
	if err = p.Reanchor(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		topic, e := tx.GetTaskConversation("scope", "task")
		if e != nil {
			return e
		}
		if topic.Revision != 2 || topic.RootMessageID != "replacement-root" {
			t.Fatalf("wrong new conversation: %#v", topic)
		}
		_, e = tx.GetContent("scope", "topic-reanchor:"+req.InteractionID+":previous-conversations")
		if e != nil {
			return e
		}
		op, e := tx.GetOperation("op_third")
		if e != nil {
			return e
		}
		if op.State != work.OperationTerminal {
			t.Fatal("failed delivery claimed sent")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queueTaskReviewPublication(t, store, "fourth")
	job, err := p.claim(t.Context(), "fourth", false)
	if err != nil {
		t.Fatal(err)
	}
	if job.Projection.RootMessageID != "replacement-root" {
		t.Fatal("new update still routed to recalled root")
	}
}
