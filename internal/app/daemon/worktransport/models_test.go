package worktransport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (f *transportFixture) launchRecord(connection ...string) {
	id := ""
	if len(connection) > 0 {
		id = connection[0]
	}
	f.tx(func(tx *workstore.Tx) error {
		record := struct {
			SettingsRevision    int64                            `json:"settings_revision"`
			PreferencesRevision int64                            `json:"preferences_revision"`
			ProfileID           string                           `json:"profile_id"`
			Backend             work.Backend                     `json:"backend"`
			Snapshot            agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
			Policy              *agentproto.CodexResumePolicy    `json:"codex_resume,omitempty"`
		}{1, 2, "profile", work.BackendCodex, agentproto.RuntimeLaunchSnapshot{Owner: f.request.Carrier.Owner, CarrierIncarnation: f.request.Carrier.Incarnation}, &agentproto.CodexResumePolicy{ConnectionContractID: id}}
		raw, _ := json.Marshal(record)
		body := work.NewContent("launch-body", "scope", "application/json", raw, f.driver.now())
		if err := tx.PutContent(body); err != nil {
			return err
		}
		_, _, err := tx.PutOperation(work.Operation{ID: "launch", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: "launch", State: work.OperationConfirmed, ExternalReceipt: "exact-test-carrier", Revision: 1}, 0)
		return err
	})
}
func TestManagedModelCatalogUsesCanonicalLaunchAndCommittedNativeResponse(t *testing.T) {
	f := newTransportFixture(t)
	f.launchRecord()
	calls := 0
	f.relay.send = func(instance string, command agentproto.Command) error {
		calls++
		if instance != "instance" || !command.IsOwnerOnlyRuntimeCommand() || command.Kind != agentproto.CommandModelList {
			t.Fatal("catalog query borrowed a business attempt or another instance")
		}
		f.tx(func(tx *workstore.Tx) error {
			native, err := tx.GetNativeCommand(command.CommandID)
			if err != nil {
				return err
			}
			if native.Binding.AttemptID != "" {
				t.Fatal("invented model discovery attempt")
			}
			return nil
		})
		event := f.nativeEvent(agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, CommandID: command.CommandID, RequestID: "native-model-list", Execution: agentproto.CloneExecutionBinding(command.Execution), ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "native-model", Model: "native-model"}}, NextCursor: "actual-next-cursor"}})
		return f.coordinator.HandleEvent(context.Background(), instance, event)
	}
	result, err := f.driver.DiscoverModels(context.Background(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.Key.ProfileID != "profile" || result.Key.SettingsRevision != 1 || result.Key.PreferencesRevision != 2 || result.Snapshot.Entries[0].Model != "native-model" || result.Snapshot.NextCursor != "actual-next-cursor" {
		t.Fatalf("lost exact native catalog provenance: %+v", result)
	}
	f.tx(func(tx *workstore.Tx) error {
		if _, err := tx.GetRuntimeModelCatalog(result.Key); err != nil {
			return err
		}
		wrong := result.Key
		wrong.SettingsRevision++
		if _, err := tx.GetRuntimeModelCatalog(wrong); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("stale settings reused model catalog")
		}
		wrong = result.Key
		wrong.CarrierIncarnation = "unrelated-carrier"
		if _, err := tx.GetRuntimeModelCatalog(wrong); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("another carrier supplied catalog authority")
		}
		op, err := tx.GetOperation(result.CommandID)
		if err != nil {
			return err
		}
		if op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
			t.Fatal("native query response not confirmed after journal commit")
		}
		return nil
	})
}
