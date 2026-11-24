package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestModelCatalogColdPageWithoutWorkerOrHostQueriesWithoutBusinessPrompt(t *testing.T) {
	f, key, _ := catalogAppFixtureWithWorker(t, false)
	calls := make(chan codexprofile.ModelCatalogProbeOptions, 1)
	f.a.runCodexModelCatalogProbe = func(_ context.Context, o codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		calls <- o
		return catalogProbeResult(o, "cold-model"), nil
	}
	if f.a.teamRuntime.host != nil || f.a.teamRuntime.driver != nil {
		t.Fatal("cold fixture unexpectedly has business runtime")
	}
	if _, e := f.a.queueTeamSettings(t.Context(), f.source, "model", ""); e != nil {
		t.Fatal(e)
	}
	f.a.profileCatalogWorkers.Wait()
	select {
	case o := <-calls:
		if o.ExpectedAccountFingerprint == "" {
			t.Fatal("cold query lost login proof")
		}
	default:
		t.Fatal("cold page did not independently query model list")
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		record, e := tx.GetProfileModelCatalog(key)
		if e != nil {
			return e
		}
		if record.Snapshot.Entries[0].Model != "cold-model" {
			t.Fatal("cold result not saved")
		}
		workers, e := tx.ListWorkers(f.source.ScopeID)
		if e != nil {
			return e
		}
		if len(workers) != 0 {
			t.Fatal("model lookup created business worker")
		}
		inputs, e := tx.ListInbox(f.source.ScopeID)
		if len(inputs) != 0 {
			t.Fatal("model lookup created business input")
		}
		return e
	})
}

func TestModelCatalogOfflineGenerationThreeRecoveryGateDoesNotBlockIndependentRefresh(t *testing.T) {
	f, key, binding := catalogAppFixture(t)
	if e := f.coordinator.Disconnect(t.Context(), f.carrier.OriginInstanceID); e != nil {
		t.Fatal(e)
	}
	f.carrier.Owner.SessionGeneration = 3
	f.carrier.Incarnation = "historical-generation-three"
	f.carrier.OriginInstanceID = "historical-instance-three"
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(f.carrier.Owner.WorkerSessionID)
		if e != nil {
			return e
		}
		if e = tx.ClearRuntimeGate(worker.ID, "carrier_disconnected_reconcile"); e != nil {
			return e
		}
		before := worker.Revision
		worker.Revision++
		worker.Generation = 2
		if e = tx.PutWorker(worker, before); e != nil {
			return e
		}
		before = worker.Revision
		worker.Revision++
		worker.Generation = 3
		worker.State = work.WorkerReady
		worker.CarrierIncarnation = f.carrier.Incarnation
		worker.InstanceID = f.carrier.OriginInstanceID
		worker.NativeSessionID = ""
		worker.NativeNamespace = ""
		if e = tx.PutWorker(worker, before); e != nil {
			return e
		}
		// An authentic old runtime catalog retains its canonical launch provenance.
		raw, _ := json.Marshal(map[string]any{"settings_revision": 1, "profile_id": state.OAuthCodexProfileID, "backend": work.BackendCodex, "snapshot": agentproto.RuntimeLaunchSnapshot{Owner: f.carrier.Owner, CarrierIncarnation: f.carrier.Incarnation}})
		body := work.NewContent("historical-launch-body", worker.ScopeID, "application/json", raw, time.Now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: "historical-launch", ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.launch", TargetID: worker.ID, TargetGeneration: 3, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: "historical-launch", State: work.OperationConfirmed, ExternalReceipt: "historical-carrier", Revision: 1}, 0)
		return e
	})
	if e := f.coordinator.RegisterCarrier(t.Context(), f.carrier); e != nil {
		t.Fatal(e)
	}
	if e := f.coordinator.HandleHello(t.Context(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: f.carrier.OriginInstanceID, Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: f.carrier.Incarnation}); e != nil {
		t.Fatal(e)
	}
	f.relay.send = func(instance string, c agentproto.Command) error {
		f.sequence++
		event := agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, CommandID: c.CommandID, Execution: agentproto.CloneExecutionBinding(c.Execution), Seq: f.sequence, ModelCatalog: &agentproto.ModelCatalogSnapshot{RefreshedAt: time.Now().Add(-48 * time.Hour), Entries: []agentproto.ModelCatalogEntry{{ID: "legacy-model", Model: "legacy-model"}}}}
		hash, e := agentproto.RuntimeEventPayloadHash(event)
		if e != nil {
			return e
		}
		event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: f.carrier.Incarnation, Sequence: event.Seq, PayloadHash: hash}
		return f.coordinator.HandleEvent(t.Context(), instance, event)
	}
	legacy, e := f.a.teamRuntime.driver.DiscoverModels(t.Context(), f.carrier.Owner.WorkerSessionID)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.coordinator.Disconnect(t.Context(), f.carrier.OriginInstanceID); e != nil {
		t.Fatal(e)
	}
	var workerBefore work.WorkerSession
	var gateBefore workstore.RuntimeGate
	var carrierBefore workstore.CarrierRegistration
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		var e error
		workerBefore, e = tx.GetWorker(f.carrier.Owner.WorkerSessionID)
		if e != nil {
			return e
		}
		gateBefore, e = tx.GetRuntimeGate(workerBefore.ID)
		if e != nil {
			return e
		}
		carrierBefore, e = tx.GetCarrier(f.carrier.Incarnation)
		if e != nil {
			return e
		}
		connected, e := tx.CarrierConnected(f.carrier.Incarnation)
		if e != nil {
			return e
		}
		if workerBefore.Generation != 3 || workerBefore.NativeSessionID != "" || workerBefore.State != work.WorkerRecovering || gateBefore.Reason != "carrier_disconnected_reconcile" || connected {
			t.Fatal("fixture does not reproduce deployed disconnected recovery state")
		}
		if _, e = tx.GetRuntimeModelCatalog(legacy.Key); e != nil {
			return e
		}
		if _, e = tx.GetProfileModelCatalog(key); e != workstore.ErrNotFound {
			t.Fatal("profile cache should start empty")
		}
		return nil
	})
	f.relay.send = func(_ string, c agentproto.Command) error {
		t.Errorf("independent refresh touched old business carrier: %s", c.Kind)
		return fmt.Errorf("old carrier must not be touched")
	}
	f.a.runCodexModelCatalogProbe = func(_ context.Context, o codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		return catalogProbeResult(o, "independent-model"), nil
	}
	snapshot, e := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, teamSettingsState{ContextID: workerBefore.ContextID, Fingerprint: snapshot.Fingerprint}); e != nil {
		t.Fatal(e)
	}
	f.a.profileCatalogWorkers.Wait()
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		record, e := tx.GetProfileModelCatalog(key)
		if e != nil {
			return e
		}
		if record.EvidenceID == "" || record.Snapshot.Entries[0].Model != "independent-model" {
			t.Fatal("independent model query did not finish")
		}
		worker, e := tx.GetWorker(workerBefore.ID)
		if e != nil {
			return e
		}
		gate, e := tx.GetRuntimeGate(worker.ID)
		if e != nil {
			return e
		}
		carrier, e := tx.GetCarrier(f.carrier.Incarnation)
		if e != nil {
			return e
		}
		connected, e := tx.CarrierConnected(f.carrier.Incarnation)
		if e != nil {
			return e
		}
		if connected {
			t.Fatal("catalog reconnected business carrier")
		}
		if !reflect.DeepEqual(worker, workerBefore) || !reflect.DeepEqual(gate, gateBefore) || !reflect.DeepEqual(carrier, carrierBefore) {
			t.Fatal("catalog lookup mutated business worker/recovery gate/carrier")
		}
		inputs, e := tx.ListInbox(f.source.ScopeID)
		if len(inputs) != 0 {
			t.Fatal("catalog created business input")
		}
		return e
	})
}
