package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestBootstrapScopeIsDurableAndCannotBecomePersonalOrProjectMemory(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		scope := work.WorkScope{ID: "bootstrap", Kind: work.ScopeBootstrap, RoleID: "r", PrincipalID: "p", Tenant: "tenant", ChatID: "new-chat", BootstrapOperationID: "bootstrap-op", BindingGeneration: 0, Revision: 1}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		operation := testOperation("bootstrap-op")
		operation.ScopeID = scope.ID
		operation.Kind = "project.bootstrap"
		operation.ContextID = ""
		operation.TargetID = "new-chat"
		if _, _, err := tx.PutOperation(operation, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "bootstrap-context", ScopeID: scope.ID, RoleID: "r", Workstream: "establish", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "foreign-bootstrap", ScopeID: scope.ID, RoleID: "expert", Workstream: "establish", Revision: 1}, 0); err == nil {
			t.Fatal("bootstrap accepted foreign role context")
		}
		task := testTask("bootstrap-task", "r")
		task.ScopeID = scope.ID
		if err := tx.PutTask(task, 0); err == nil {
			t.Fatal("bootstrap prematurely accepted task")
		}
		if _, _, err := tx.ReadMemory(work.ReadScope{ScopeID: scope.ID, RoleID: "r"}, "private-memory"); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("bootstrap gained knowledge access: %v", err)
		}
		resource := work.Resource{ID: "bootstrap-resource", ScopeID: scope.ID, CollectionID: "resources", ConnectorIdentity: "bot", SourceIdentity: "doc", CanonicalURI: "https://example.test/doc", Purpose: "bootstrap", Revision: 1}
		if err := tx.PutResource(resource, 0); !errors.Is(err, ErrScopeDenied) {
			t.Fatalf("bootstrap gained project resource registry: %v", err)
		}
		return nil
	})
	requireTx(t, s, func(tx *Tx) error {
		scope, err := tx.GetScope("bootstrap")
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopeBootstrap || scope.BindingGeneration != 0 || scope.ChatID != "new-chat" {
			t.Fatalf("bootstrap facts changed %+v", scope)
		}
		scope.Revision++
		scope.PrincipalID = "different-human"
		if err = tx.PutScope(scope, 1); err == nil {
			t.Fatal("bootstrap owner replaced by last speaker")
		}
		return nil
	})
}
