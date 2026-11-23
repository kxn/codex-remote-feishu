package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateReactionsExcludeGroupTrafficAndBackfillAfterRestart(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		binding := work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
		if e := tx.PutRoleBotBinding(binding, 0); e != nil {
			return e
		}
		groupMessage := work.Message{ID: "group-human", ScopeID: "s", ConversationID: "group", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "group", MessageID: "om_group"}, Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: time.Now()}}, BodyRef: "body", Revision: 1, CreatedAt: time.Now()}
		if _, _, e := tx.AcceptMessage(groupMessage, []work.Inbox{{ID: "group-input", ScopeID: "s", RecipientRoleID: "r", RecipientContextID: "c-r", MessageID: groupMessage.ID, EventID: "group-event", State: work.InboxPending, Revision: 1}}); e != nil {
			return e
		}
		reactions, e := tx.ListPendingPrivateReactions(time.Now(), 32)
		if e != nil {
			return e
		}
		if len(reactions) != 0 {
			t.Fatal("group ingress registered noisy per-message reactions")
		}
		if e = tx.PutScope(work.WorkScope{ID: "private", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutContext(work.WorkContext{ID: "private-context", ScopeID: "private", RoleID: "r", Workstream: "private", Revision: 1}, 0); e != nil {
			return e
		}
		body := work.NewContent("private-body", "private", "text/plain", []byte("private input"), time.Now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		message := groupMessage
		message.ID = "private-human"
		message.ScopeID = "private"
		message.BodyRef = body.ID
		message.PlatformKey = &work.MessageKey{Tenant: "tenant", ChatID: "p2p", MessageID: "om_private"}
		if _, _, e = tx.AcceptMessage(message, []work.Inbox{{ID: "private-input", ScopeID: "private", RecipientRoleID: "r", RecipientContextID: "private-context", MessageID: message.ID, EventID: "private-event", State: work.InboxPending, Revision: 1}}); e != nil {
			return e
		}
		// Simulate an existing installation's old schema before this projection.
		_, e = tx.tx.ExecContext(tx.ctx, "DROP TABLE team_private_reactions")
		return e
	})
	reopened, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		if e := tx.BackfillPrivateInputReactions(1); e != nil {
			return e
		}
		reactions, e := tx.ListPendingPrivateReactions(time.Now(), 32)
		if e != nil {
			return e
		}
		if len(reactions) != 1 || reactions[0].InputID != "private-input" {
			t.Fatalf("group inputs starved private migration: %+v", reactions)
		}
		return nil
	})
}

func TestPlatformReactionReplayIgnoresOnlyReceiveTimeAndAcceptsLegacyHash(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	received := time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC)
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "r", BotGeneration: 1}
	event := map[string]any{"app_id": "app", "gateway_ref": "gateway", "message_id": "om_human", "tenant": "tenant", "operator_type": "user", "emoji": "ThumbsUp", "received_at": received, "operator_ids": work.SenderIDs{OpenID: "human"}}
	facts, _ := json.Marshal(event)
	observation := PlatformObservation{Gateway: gateway, EventID: "same-platform-event", Tenant: "tenant", MessageID: "om_human", Kind: "reaction.created", Facts: facts, ReceivedAt: received}
	requireTx(t, s, func(tx *Tx) error {
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		id, _, e := tx.AcceptPlatformObservation(observation)
		if e != nil {
			return e
		}
		observation.ID = id
		legacy := observation
		legacy.ReceivedAt = time.Time{}
		raw, _ := json.Marshal(legacy)
		digest := sha256.Sum256(raw)
		_, e = tx.tx.ExecContext(tx.ctx, "UPDATE team_platform_observations SET payload_hash=? WHERE id=?", hex.EncodeToString(digest[:]), id)
		return e
	})
	observation.ReceivedAt = received.Add(time.Minute)
	event["received_at"] = observation.ReceivedAt
	observation.Facts, _ = json.Marshal(event)
	requireTx(t, s, func(tx *Tx) error {
		_, replay, e := tx.AcceptPlatformObservation(observation)
		if !replay || e != nil {
			t.Fatalf("legacy receive-time replay failed: replay=%v %v", replay, e)
		}
		return e
	})
	event["emoji"] = "APPLAUSE"
	observation.Facts, _ = json.Marshal(event)
	if e := s.Within(t.Context(), func(tx *Tx) error { _, _, e := tx.AcceptPlatformObservation(observation); return e }); !errors.Is(e, ErrIdempotencyConflict) {
		t.Fatalf("changed reaction accepted as replay: %v", e)
	}
}

func TestPrivateReactionAuthorityPreservesLegacyCommandRequestBytes(t *testing.T) {
	// Existing host/menu operation fingerprints include this struct verbatim.
	// Introducing an unused reaction source must not change their persisted hash.
	legacy := struct {
		MenuInteractionID string
		PersonalScopeID   string
		CommandID         string
		InteractionID     string
		MessageID         string
		RunRef            work.RunRef
	}{PersonalScopeID: "scope", CommandID: "old-command"}
	expected, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(PrivateActionSource{PersonalScopeID: "scope", CommandID: "old-command"})
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("old source serialization changed: %s != %s", actual, expected)
	}
}
