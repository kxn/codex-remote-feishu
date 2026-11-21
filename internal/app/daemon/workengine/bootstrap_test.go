package workengine

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestBootstrapUsesItsOwnLeaderContextWithoutProjectMembership(t *testing.T) {
	s, _ := fixture(t, "existing")
	transaction(t, s, func(tx *workstore.Tx) error {
		scope := work.WorkScope{ID: "bootstrap", Kind: work.ScopeBootstrap, RoleID: "leader", PrincipalID: "human", Tenant: "tenant", ChatID: "new-chat", BootstrapOperationID: "bootstrap-op", Revision: 1}
		if err := tx.PutScope(scope, 0); err != nil {
			return err
		}
		operation := work.Operation{ID: "bootstrap-op", ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, Kind: "project.bootstrap", TargetID: "new-chat", TargetGeneration: 1, IdempotencyKey: "bootstrap", RequestHash: "bootstrap-hash", State: work.OperationPrepared, Revision: 1}
		if _, _, err := tx.PutOperation(operation, 0); err != nil {
			return err
		}
		return putWorker(tx, "bootstrap", "leader", "", "")
	})
	enqueue(t, s, "bootstrap-input", "bootstrap", "leader", "", "")
	r, err := engine(t, s, 1, &testPlanner{}, &testDispatcher{}).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 1 {
		t.Fatalf("bootstrap input not scheduled %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(r.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if attempt.ScopeID != "bootstrap" || attempt.TaskID != "" || attempt.ContextID != "context:bootstrap:leader" {
			t.Fatalf("bootstrap borrowed another scope: %+v", attempt)
		}
		return nil
	})
}
