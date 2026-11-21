package workstore

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestReviewPrivateKnowledgeCannotPromoteWithoutHumanSharingEvidence(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "other-person", DisplayName: "Other", Revision: 1}, 0); err != nil {
			return err
		}
		for _, scope := range []work.WorkScope{{ID: "private-one", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, {ID: "private-two", Kind: work.ScopePersonal, PrincipalID: "other-person", RoleID: "r", Revision: 1}} {
			if err := tx.PutScope(scope, 0); err != nil {
				return err
			}
		}
		return tx.PutContent(work.NewContent("private-secret", "private-one", "text/plain", []byte("private conversation"), time.Now().UTC()))
	})
	requireTx(t, s, func(tx *Tx) error {
		entry := memory("promoted-private", work.MemoryScope{Kind: work.MemoryRole, RoleID: "r"})
		entry.StorageScopeID = "private-one"
		entry.ContentRef = "private-secret"
		if err := tx.PutMemory(entry, 0); err == nil {
			_, body, err := tx.ReadMemory(work.ReadScope{ScopeID: "private-two", RoleID: "r"}, entry.ID)
			if err == nil && string(body.Data) == "private conversation" {
				t.Error("private content promoted to all role conversations without any human sharing record")
			}
		} // Rejecting memory promotion must not skip the independent practice check.

		definition := practice(work.KnowledgeOwner{Kind: work.KnowledgeGlobal}, "private-version")
		definition.StorageScopeID = "private-one"
		definition.ContentRef = "private-secret"
		if err := tx.PutPractice(definition, 0); err != nil {
			return nil
		}
		_, body, err := tx.GetPracticeFor(work.ReadScope{ScopeID: "s", RoleID: "expert"}, definition.Owner, definition.PracticeRef)
		if err == nil && string(body.Data) == "private conversation" {
			t.Error("private content published to global practice library without human sharing record")
		}
		return nil
	})
}

func TestReviewRoleAuthoredMessageCannotSelfDelegateHumanDecision(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedDecision(t, s)
	requireTx(t, s, func(tx *Tx) error {
		_, _, err := tx.AcceptMessage(work.Message{ID: "role-request", ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorRole, ID: "r"}, BodyRef: "body", Revision: 1}, nil)
		return err
	})
	requireTx(t, s, func(tx *Tx) error {
		next := agreement(work.SourceRef{Kind: "message", ID: "role-request", Version: "1", EvidenceRef: "body"})
		next.Initiator = work.Actor{Kind: work.ActorRole, ID: "r"}
		next.DecisionOwner = next.Initiator
		next.Revision = 2
		replacement := decisionRequest()
		replacement.ID = "self-delegated"
		replacement.Owner = next.DecisionOwner
		replacement.AllowedActors = []work.Actor{next.DecisionOwner}
		replacement.AgreementRevision = 2
		replacement.RequestGeneration = 2
		queued := testInput("self-inbox", "r", "c-r")
		queued.EventID = "decision:self-delegated"
		change := ApplyAgreementRequest{Agreement: next, ExpectedRevision: 1, Replacements: []DecisionReplacement{{OldRequestID: "request", ExpectedRevision: 1, Replacement: replacement}}, Inbox: []work.Inbox{queued}}
		if err := tx.ApplyAgreement(change); err == nil {
			t.Error("role-authored message transferred a human-owned pending decision to that role without human instruction")
		} else {
			t.Logf("rejected authority change: %v", err)
		}
		return nil
	})
}
