package worktransport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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

func TestModelCatalogConnectionReuseKeepsNativeProvenanceAndAuthBoundary(t *testing.T) {
	f := newTransportFixture(t)
	f.launchRecord("authenticated-connection-a")
	now := time.Now().UTC()
	f.relay.send = func(instance string, command agentproto.Command) error {
		if command.Kind != agentproto.CommandModelList || command.Execution.AttemptID != "" {
			t.Fatal("discovery started business execution")
		}
		event := f.nativeEvent(agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, CommandID: command.CommandID, RequestID: "actual-list", Execution: agentproto.CloneExecutionBinding(command.Execution), ModelCatalog: &agentproto.ModelCatalogSnapshot{RefreshedAt: now, Entries: []agentproto.ModelCatalogEntry{{ID: "account-model", Model: "account-model"}}}})
		return f.coordinator.HandleEvent(t.Context(), instance, event)
	}
	result, err := f.driver.DiscoverModels(t.Context(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding("role")
		if err != nil {
			return err
		}
		connection, err := tx.RuntimeCatalogConnection(result.Key)
		if err != nil {
			return err
		}
		if connection != "authenticated-connection-a" {
			t.Fatalf("connection=%q", connection)
		}
		// A model/reasoning preference edit changes the Bot/worker revision, not
		// authentication. The observed page keeps its original command and key.
		role, err := tx.GetRole("role")
		if err != nil {
			return err
		}
		before := role.Revision
		role.SettingsRevision++
		role.Revision++
		if err = tx.PutRole(role, before); err != nil {
			return err
		}
		cached, err := tx.RuntimeCatalogForConnection(binding, "profile", connection, "", now)
		if err != nil {
			return err
		}
		if cached.CommandID != result.CommandID || cached.Key != result.Key {
			t.Fatal("cache relabelled native provenance")
		}
		for _, tc := range []struct {
			profile, connection, cursor string
			at                          time.Time
		}{
			{"profile", "different-auth", "", now}, {"different-profile", connection, "", now}, {"profile", connection, "different-page", now}, {"profile", connection, "", now.Add(6 * time.Minute)},
		} {
			if _, err := tx.RuntimeCatalogForConnection(binding, tc.profile, tc.connection, tc.cursor, tc.at); !errors.Is(err, workstore.ErrNotFound) {
				t.Fatalf("unsafe cache reuse %+v: %v", tc, err)
			}
		}
		changed := binding
		changed.IdentityGeneration++
		if _, err := tx.RuntimeCatalogForConnection(changed, "profile", connection, "", now); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatalf("another bot reused source: %v", err)
		}
		return nil
	})
}

func TestModelCatalogConnectionChoosesObservationTimeInsteadOfArrivalRow(t *testing.T) {
	f := newTransportFixture(t)
	f.launchRecord("same-login")
	now := time.Now().UTC()
	count := 0
	f.relay.send = func(instance string, c agentproto.Command) error {
		at := now
		if count > 0 {
			at = now.Add(-time.Minute)
		}
		count++
		event := f.nativeEvent(agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, CommandID: c.CommandID, Execution: agentproto.CloneExecutionBinding(c.Execution), ModelCatalog: &agentproto.ModelCatalogSnapshot{RefreshedAt: at, Entries: []agentproto.ModelCatalogEntry{{ID: c.CommandID, Model: c.CommandID}}}})
		return f.coordinator.HandleEvent(t.Context(), instance, event)
	}
	newest, err := f.driver.DiscoverModels(t.Context(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.driver.DiscoverModels(t.Context(), "worker"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		binding, e := tx.GetRoleBotBinding("role")
		if e != nil {
			return e
		}
		got, e := tx.RuntimeCatalogForConnection(binding, "profile", "same-login", "", now)
		if e != nil {
			return e
		}
		if got.CommandID != newest.CommandID {
			t.Fatal("later persisted older observation replaced fresh model choices")
		}
		return nil
	})
}
