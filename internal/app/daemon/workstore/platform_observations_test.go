package workstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPlatformRecallCancelsOnlyUnclaimedAndKeepsImmutableSource(t *testing.T) {
	store, _ := openTestStore(t)
	seedPersonal(t, store)
	now := time.Now().UTC()
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "r", BotGeneration: 1}
	requireTx(t, store, func(tx *Tx) error {
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		message := work.Message{ID: "original", ScopeID: "s", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "chat", MessageID: "platform-message"}, BodyRef: "body", Revision: 1, CreatedAt: now}
		_, _, e := tx.AcceptMessage(message, []work.Inbox{testInput("input", "r", "c-r"), testInput("other-input", "expert", "c-expert")})
		return e
	})
	requireTx(t, store, func(tx *Tx) error { return tx.ClaimAttempt(testClaim()) })
	observation := PlatformObservation{Gateway: gateway, EventID: "recall-event", Tenant: "tenant", ChatID: "chat", MessageID: "platform-message", Kind: "message.recalled", Facts: json.RawMessage(`{"sdk":"recall","message_id":"platform-message"}`), ReceivedAt: now}
	requireTx(t, store, func(tx *Tx) error { _, _, e := tx.AcceptPlatformObservation(observation); return e })
	requireTx(t, store, func(tx *Tx) error {
		recalled, e := tx.IsPlatformMessageRecalled("tenant", "chat", "platform-message")
		if e != nil {
			return e
		}
		if !recalled {
			t.Fatal("recall missing")
		}
		input, e := tx.GetInbox("input")
		if e != nil {
			return e
		}
		if input.State != work.InboxClaimed {
			t.Fatal("in-flight execution was rewritten")
		}
		other, e := tx.GetInbox("other-input")
		if e != nil {
			return e
		}
		if other.State != work.InboxCancelled {
			t.Fatal(other.State)
		}
		message, e := tx.GetMessage("original")
		if e != nil {
			return e
		}
		if message.RecalledEvidenceRef == "" || message.Revision != 2 || message.BodyRef != "body" {
			t.Fatal(message)
		}
		all, e := tx.ListInbox("s")
		if e != nil {
			return e
		}
		if len(all) != 3 {
			t.Fatalf("expected one system recall input, got %d", len(all))
		}
		return nil
	})
	observation.ReceivedAt = now.Add(time.Second)
	requireTx(t, store, func(tx *Tx) error {
		_, replay, e := tx.AcceptPlatformObservation(observation)
		if !replay {
			t.Fatal("replay not recognized")
		}
		return e
	})
	observation.Facts = json.RawMessage(`{"changed":true}`)
	if e := store.Within(context.Background(), func(tx *Tx) error { _, _, e := tx.AcceptPlatformObservation(observation); return e }); e == nil {
		t.Fatal("SDK event ID payload changed")
	}
}

func TestRecallBeforeMessageAndAnotherTenantDoNotLoseTombstone(t *testing.T) {
	store, _ := openTestStore(t)
	seedPersonal(t, store)
	requireTx(t, store, func(tx *Tx) error {
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		_, _, e := tx.AcceptPlatformObservation(PlatformObservation{Gateway: work.IngressGateway{RoleID: "r", GatewayRef: "gateway", AppID: "app", BotGeneration: 1}, EventID: "early-recall", Tenant: "tenant", ChatID: "chat", MessageID: "message", Kind: "message.recalled", Facts: json.RawMessage(`{"sdk":"recall"}`), ReceivedAt: time.Now().UTC()})
		return e
	})
	requireTx(t, store, func(tx *Tx) error {
		yes, e := tx.IsPlatformMessageRecalled("tenant", "chat", "message")
		if e != nil {
			return e
		}
		if !yes {
			t.Fatal("early recall lost")
		}
		other, e := tx.IsPlatformMessageRecalled("another-tenant", "chat", "message")
		if e != nil {
			return e
		}
		if other {
			t.Fatal("cross-tenant recall")
		}
		return nil
	})
}
