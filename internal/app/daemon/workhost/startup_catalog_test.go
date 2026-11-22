package workhost

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func seedStartupNativeCommand(t *testing.T, store *workstore.Store, worker work.WorkerSession, kind agentproto.CommandKind) {
	t.Helper()
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		carrier, err := tx.GetCarrier(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		cmd := agentproto.Command{CommandID: "startup-query-" + string(kind), Kind: kind, Execution: &carrier.Owner}
		raw, err := json.Marshal(cmd)
		if err != nil {
			return err
		}
		content := work.NewContent(cmd.CommandID+":body", worker.ScopeID, "application/json", raw, worker.UpdatedAt)
		if err = tx.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: cmd.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "catalog-test"}, Kind: string(kind), TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: cmd.CommandID, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		_, err = tx.RegisterNativeCommand(worker.CarrierIncarnation, cmd)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStartupRecoveryOnlyExemptsModelCatalogQueries(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 1234, nil })
	if err := h.Prepare(t.Context(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}); err != nil {
		t.Fatal(err)
	}
	worker := hostWorker(t, store)
	seedStartupNativeCommand(t, store, worker, agentproto.CommandModelList)
	assertBlocked := func(want bool) {
		t.Helper()
		if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
			blocked, err := tx.CarrierHasStartupBlockingCommands(worker.CarrierIncarnation)
			if err == nil && blocked != want {
				t.Fatalf("blocked=%v want %v", blocked, want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertBlocked(false)
	seedStartupNativeCommand(t, store, worker, agentproto.CommandRuntimeMCPRead)
	assertBlocked(true)
}
