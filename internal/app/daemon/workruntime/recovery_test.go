package workruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func recoveryRequest(t *testing.T, f *fixture) RecoveryRequest {
	t.Helper()
	var gate workstore.RuntimeGate
	f.tx(func(tx *workstore.Tx) error {
		var err error
		gate, err = tx.GetRuntimeGate("worker")
		if err != nil {
			return err
		}
		return tx.PutContent(work.NewContent("human-recovery-source", "scope", "application/json", []byte(`{"authenticated_principal":"principal","action":"recover"}`), f.now))
	})
	return RecoveryRequest{ControlRequest: ControlRequest{ScopeID: "scope", WorkerID: "worker", AttemptID: "attempt", ExpectedControllerGeneration: 1, Actor: work.Actor{Kind: work.ActorPrincipal, ID: "principal"}}, GateReason: gate.Reason, ExpectedGateRevision: gate.Revision, Source: work.SourceRef{Kind: "human_request", ID: "recover-click", Version: "1", EvidenceRef: "human-recovery-source"}}
}
func completeWithoutReport(t *testing.T, f *fixture) {
	t.Helper()
	f.start()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "command", ThreadID: "session", TurnID: "turn-1", Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime}); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitRecoveryPreservesOriginalInputsAndQueuesOneRecoveryInstruction(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	completeWithoutReport(t, f)
	request := recoveryRequest(t, f)
	for i := 0; i < 2; i++ {
		if _, err := f.coordinator.Recover(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerRecovering || worker.Generation != 1 {
			t.Fatal("recovery hot-swapped or prematurely readied native owner")
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		if len(inputs) != 2 {
			t.Fatalf("recovery input duplicated: %d", len(inputs))
		}
		for _, input := range inputs {
			if input.State != work.InboxPending {
				t.Fatal("recovery claimed original input as processed")
			}
		}
		gate, err := tx.GetRuntimeGate("worker")
		if err != nil {
			return err
		}
		if gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "" || gate.Revision <= request.ExpectedGateRevision {
			t.Fatal("explicit recovery did not replace exact gate generation")
		}
		return nil
	})
}

func TestRecoveryRejectsStaleGateAndUnknownNativeWithoutReleasingOccupancy(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	f.start()
	f.tx(func(tx *workstore.Tx) error {
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: "worker", ScopeID: "scope", AttemptID: "attempt", Reason: "native_command_unknown", CreatedAt: f.now})
	})
	request := recoveryRequest(t, f)
	if _, err := f.coordinator.Recover(context.Background(), request); err == nil {
		t.Fatal("unknown native work was force-recovered")
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasAnyCarrierOccupancy(f.carrier)
		if err != nil {
			return err
		}
		if !occupied {
			t.Fatal("recovery released unknown native occupancy")
		}
		return nil
	})
}

func TestRetiredCarrierOSHelper(t *testing.T) {
	if os.Getenv("RETIRED_CARRIER_HELPER") == "1" {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		os.Exit(0)
	}
}

func retiredRecoveryFixture(t *testing.T, extraFrame bool, alive bool) (*fixture, RecoveryRequest) {
	t.Helper()
	f := newFixture(t, work.BackendClaude)
	stateDir := filepath.Dir(f.path)
	f.coordinator.options.StateDir = stateDir
	f.coordinator.options.HostID = "local"
	snapshot := agentproto.RuntimeLaunchSnapshot{HostID: "local", Version: 1, Owner: f.carrier.Owner, CWD: t.TempDir(), InstructionBundle: agentproto.InstructionBundle{Revision: "v1", Text: "isolated native instructions"}, JournalPath: filepath.Join(stateDir, "workers", "worker", "runtime.db"), CarrierIncarnation: f.carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	journal, err := runtimejournal.Open(context.Background(), snapshot.JournalPath, stateDir, snapshot.CWD, snapshot.CarrierIncarnation)
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err = journal.PrepareCommand(f.command); err != nil {
		t.Fatal(err)
	}
	if _, err = journal.PrepareFrame(f.command.CommandID, []byte(`{"id":"native-input","method":"turn/start","params":{}}`)); err != nil {
		t.Fatal(err)
	}
	watermark, err := journal.NativeWriteWatermark()
	if err != nil {
		t.Fatal(err)
	}
	inspect := f.command
	inspect.CommandID = "original-quiescence-inspection"
	inspect.Kind = agentproto.CommandRuntimeInspect
	f.registerExtra(inspect)
	var index int64
	f.tx(func(tx *workstore.Tx) error {
		var e error
		index, e = tx.NativeCommandWatermark(inspect.CommandID)
		if e != nil {
			return e
		}
		return nil
	})
	launchRaw := struct {
		Snapshot agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
	}{snapshot}
	f.tx(func(tx *workstore.Tx) error {
		data, e := json.Marshal(launchRaw)
		if e != nil {
			return e
		}
		body := work.NewContent("launch-body", "scope", "application/json", data, f.now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: "launch", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "launch", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, ExternalReceipt: "launched", Revision: 1}, 0)
		return e
	})
	child := exec.Command(os.Args[0], "-test.run=^TestRetiredCarrierOSHelper$")
	child.Env = append(os.Environ(), "RETIRED_CARRIER_HELPER=1")
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close(); child.Process.Kill() })
	identity, err := hostinventory.CaptureProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		return tx.PutCarrierProcess(f.carrier.Incarnation, f.carrier.OriginInstanceID, agentproto.RuntimeProcessIdentity{HostID: "local", Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity})
	})
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		observation := stoppedObservation(request)
		observation.InspectionCommandID = inspect.CommandID
		observation.CanonicalCommandWatermark = index
		observation.NativeWriteWatermark = watermark
		return observation, nil
	}
	completeWithoutReport(t, f)
	if extraFrame {
		extra := f.command
		extra.CommandID = "unplanned-native-input"
		if _, _, err = journal.PrepareCommand(extra); err != nil {
			t.Fatal(err)
		}
		if _, err = journal.PrepareFrame(extra.CommandID, []byte(`{"id":"later","method":"turn/start","params":{}}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	if !alive {
		if err = stdin.Close(); err != nil {
			t.Fatal(err)
		}
		if err = child.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	f.driver.inspect = func(InspectionRequest) (NativeObservation, error) {
		return NativeObservation{}, errors.New("exact carrier transport disconnected")
	}
	return f, recoveryRequest(t, f)
}

func TestRetiredRecoveryRequiresBothWatermarksAndExactProcessExit(t *testing.T) {
	for _, scenario := range []struct {
		name                  string
		extra, alive, allowed bool
	}{{"quiescent-exited", false, false, true}, {"new-native-frame", true, false, false}, {"same-process-still-alive", false, true, false}} {
		t.Run(scenario.name, func(t *testing.T) {
			f, request := retiredRecoveryFixture(t, scenario.extra, scenario.alive)
			_, err := f.coordinator.Recover(context.Background(), request)
			if (err == nil) != scenario.allowed {
				t.Fatalf("retired recovery allowed=%t error=%v", scenario.allowed, err)
			}
		})
	}
}
