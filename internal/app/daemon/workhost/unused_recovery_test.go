package workhost

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"testing"
)

func TestDisconnectedUnusedCarrierRecoversWithoutInventingTurnReceipt(t *testing.T) {
	for _, scenario := range []string{"exited-catalog-only", "still-alive", "journal-has-turn", "canonical-has-effect"} {
		t.Run(scenario, func(t *testing.T) {
			var pipe *retiredNativePipe
			starts := 0
			h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) {
				starts++
				pipe = startRetiredNativePipe(t)
				return pipe.cmd.Process.Pid, nil
			})
			need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
			if err := h.Prepare(t.Context(), need); err != nil {
				t.Fatal(err)
			}
			worker := hostWorker(t, store)
			worker = readyLifecycleCandidate(t, store, worker.ContextID).Worker
			var record LaunchRecord
			var carrier workstore.CarrierRegistration
			identity, err := hostinventory.CaptureProcess(pipe.cmd.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
				var err error
				record, err = h.readLaunch(tx, worker)
				if err != nil {
					return err
				}
				carrier, err = tx.GetCarrier(worker.CarrierIncarnation)
				if err != nil {
					return err
				}
				return tx.PutCarrierProcess(worker.CarrierIncarnation, worker.InstanceID, agentproto.RuntimeProcessIdentity{HostID: "local", PID: identity.PID, Platform: identity.Platform, StartIdentity: identity.StartIdentity})
			}); err != nil {
				t.Fatal(err)
			}
			journal, err := runtimejournal.Open(t.Context(), record.Snapshot.JournalPath, h.config.Paths.StateDir, record.Snapshot.CWD, worker.CarrierIncarnation)
			if err != nil {
				t.Fatal(err)
			}
			if err = journal.BindLaunchSnapshot(record.Snapshot); err != nil {
				t.Fatal(err)
			}
			cmd := agentproto.Command{CommandID: "catalog", Kind: agentproto.CommandModelList, Execution: &carrier.Owner}
			if _, _, err = journal.PrepareCommand(cmd); err != nil {
				t.Fatal(err)
			}
			if _, err = journal.PrepareFrame(cmd.CommandID, []byte(`{"id":1,"method":"model/list","params":{}}`)); err != nil {
				t.Fatal(err)
			}
			seedStartupNativeCommand(t, store, worker, agentproto.CommandModelList)
			if scenario == "journal-has-turn" {
				if _, err = journal.PrepareFrame(cmd.CommandID, []byte(`{"id":2,"method":"turn/start","params":{}}`)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "canonical-has-effect" {
				seedStartupNativeCommand(t, store, worker, agentproto.CommandRuntimeMCPRead)
			}
			if err = journal.Close(); err != nil {
				t.Fatal(err)
			}
			if scenario != "still-alive" {
				pipe.input.Close()
				if err = pipe.cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			coordinator := workruntime.New(store, workruntime.Options{})
			if err = coordinator.Disconnect(t.Context(), worker.InstanceID); err != nil {
				t.Fatal(err)
			}
			worker = hostWorker(t, store)
			recovered, recoveryErr := h.reconcileExitedIdleCarrier(t.Context(), worker)
			if scenario != "exited-catalog-only" {
				if recovered {
					t.Fatal("unproven retirement released carrier")
				}
				return
			}
			if recoveryErr != nil || !recovered {
				t.Fatalf("recovered=%v error=%v", recovered, recoveryErr)
			}
			if err = h.Prepare(t.Context(), need); err != nil {
				t.Fatal(err)
			}
			next := hostWorker(t, store)
			if starts != 2 || next.Generation != worker.Generation+1 || next.ContextID != worker.ContextID || next.NativeSessionID != "" {
				t.Fatalf("replacement changed conversation: %+v", next)
			}
		})
	}
}
