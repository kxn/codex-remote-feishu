package workstore

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func seedBootstrapAdoption(t *testing.T, s *Store) { seedBootstrapAdoptionForm(t, s, false) }
func seedBootstrapAdoptionForm(t *testing.T, s *Store, form bool) {
	t.Helper()
	requireTx(t, s, func(tx *Tx) error {
		scope := work.WorkScope{ID: "establishment", Kind: work.ScopeBootstrap, RoleID: "r", PrincipalID: "p", Tenant: "tenant", ChatID: "new-chat", BootstrapOperationID: "bootstrap-op", Revision: 1}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		now := time.Now().UTC()
		body := work.NewContent("original-human-body", scope.ID, "text/plain", []byte("Human's original request with exact details"), now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		op := testOperation("bootstrap-op")
		op.ScopeID = scope.ID
		op.ContextID = ""
		op.Kind = "project.bootstrap"
		op.TargetID = scope.ChatID
		if form {
			op.Kind = "bootstrap.form"
			op.Actor = work.Actor{Kind: work.ActorPrincipal, ID: "p"}
			op.TargetID = scope.ID
			op.RequestRef = body.ID
			op.RequestHash = body.SHA256
			op.State = work.OperationConfirmed
			op.ExternalReceipt = body.ID
		}
		if _, _, err := tx.PutOperation(op, 0); err != nil {
			return err
		}
		source := work.Message{ID: "original-human", ScopeID: scope.ID, ConversationID: "original-thread", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "new-chat", MessageID: "platform-message"}, BodyRef: body.ID, Revision: 1, CreatedAt: now}
		if form {
			source.PlatformKey = nil
		}
		if _, _, err := tx.AcceptMessage(source, nil); err != nil {
			return err
		}
		if err := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "new-chat", Purpose: "main", BindingGeneration: 1, Active: true}, 0); err != nil {
			return err
		}
		request := struct {
			bootstrapSourceRequest
			InitialRequest string `json:"initial_request"`
		}{bootstrapSourceRequest: bootstrapSourceRequest{Tenant: "tenant", ChatID: "new-chat", PrincipalID: "p", Key: "bootstrap-op", LeaderRoleID: "r", SourceMessageID: source.ID}, InitialRequest: "MODEL PARAPHRASE MUST NOT BECOME THE HUMAN MESSAGE"}
		data, err := json.Marshal(request)
		if err != nil {
			return err
		}
		content := work.NewContent("provision-request", "s", "application/json", data, now)
		if err = tx.PutContent(content); err != nil {
			return err
		}
		provision := testOperation("provision")
		provision.Kind = "project.provision"
		provision.Actor = work.Actor{Kind: work.ActorPrincipal, ID: "p"}
		provision.ContextID = ""
		provision.TargetID = "project"
		provision.RequestRef = content.ID
		provision.RequestHash = content.SHA256
		_, _, err = tx.PutOperation(provision, 0)
		return err
	})
}
func TestBootstrapAdoptionPreservesActualHumanAndImportsOnlyOnce(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedBootstrapAdoption(t, s)
	input := AdoptBootstrapInputRequest{OperationID: "provision", SourceMessageID: "original-human", TargetContextID: "c-r"}
	requireTx(t, s, func(tx *Tx) error {
		derived, created, err := tx.AdoptBootstrapInput(input)
		if err != nil {
			return err
		}
		if !created || derived.Source != (work.Actor{Kind: work.ActorPrincipal, ID: "p"}) || derived.PlatformKey != nil || derived.DerivedFrom == nil || derived.DerivedFrom.ID != "original-human" {
			t.Fatalf("false input provenance %+v created=%v", derived, created)
		}
		body, err := tx.GetContent("s", derived.BodyRef)
		if err != nil {
			return err
		}
		if string(body.Data) != "Human's original request with exact details" {
			t.Fatalf("model request replaced raw human input: %s", body.Data)
		}
		original, err := tx.GetMessage("original-human")
		if err != nil {
			return err
		}
		if original.ScopeID != "establishment" || original.PlatformKey == nil {
			t.Fatal("original platform message ownership was moved")
		}
		again, created, err := tx.AdoptBootstrapInput(input)
		if err != nil {
			return err
		}
		if created || again.ID != derived.ID {
			t.Fatal("replay duplicated initial message")
		}
		messages, err := tx.ListMessages("s")
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox("s")
		if err != nil {
			return err
		}
		if len(messages) != 1 || len(inputs) != 1 || inputs[0].MessageID != derived.ID {
			t.Fatal("initial input imported more than once")
		}
		return nil
	})
}
func TestBootstrapAdoptionRejectsUnselectedOrWrongContextWithoutPartialWrites(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedBootstrapAdoption(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if _, _, err := tx.AdoptBootstrapInput(AdoptBootstrapInputRequest{OperationID: "provision", SourceMessageID: "not-explicitly-selected"}); err == nil {
			t.Fatal("unselected source accepted")
		}
		if _, _, err := tx.AdoptBootstrapInput(AdoptBootstrapInputRequest{OperationID: "provision", SourceMessageID: "original-human", TargetContextID: "c-expert"}); err == nil {
			t.Fatal("initial input handed to a different role")
		}
		messages, err := tx.ListMessages("s")
		if err != nil {
			return err
		}
		inputs, err := tx.ListInbox("s")
		if err != nil {
			return err
		}
		if len(messages) != 0 || len(inputs) != 0 {
			t.Fatal("failed adoption leaked a partial message or inbox")
		}
		return nil
	})
}

func TestBootstrapAdoptionAcceptsVerifiedHumanFormWithoutInventingPlatformMessage(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedBootstrapAdoptionForm(t, s, true)
	requireTx(t, s, func(tx *Tx) error {
		derived, created, err := tx.AdoptBootstrapInput(AdoptBootstrapInputRequest{OperationID: "provision", SourceMessageID: "original-human"})
		if err != nil {
			return err
		}
		if !created || derived.PlatformKey != nil || derived.Source.Kind != work.ActorPrincipal {
			t.Fatalf("form source forged platform/actor %+v", derived)
		}
		return nil
	})
}
