package workruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func codexInspection(request InspectionRequest) ProtocolInspection {
	return ProtocolInspection{
		Native:       RPCProbe{Request: json.RawMessage(fmt.Sprintf(`{"id":"inspect-native","method":"thread/read","params":{"threadId":%q,"includeTurns":true}}`, request.SessionID)), Response: json.RawMessage(fmt.Sprintf(`{"id":"inspect-native","result":{"thread":{"id":%q,"status":{"type":"idle"}}}}`, request.SessionID))},
		Continuation: RPCProbe{Request: json.RawMessage(fmt.Sprintf(`{"id":"inspect-goal","method":"thread/goal/get","params":{"threadId":%q}}`, request.SessionID)), Response: json.RawMessage(`{"id":"inspect-goal","result":{"goal":null}}`)},
		Background:   BackgroundInspection{CarrierIncarnation: request.Carrier.Incarnation, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, Complete: true, ContinuationInventoryKnown: true, NativeTaskSetComplete: true, NativeInputsComplete: true, Evidence: json.RawMessage(`{"source":"owned-host-process-and-native-task-inventory","scan_complete":true}`)}, ObservedAt: time.Now().UTC(),
	}
}

func TestProtocolInspectionRequiresCorrelatedNativeAndIndependentHostEvidence(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	var request InspectionRequest
	f.tx(func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		request, err = f.coordinator.inspectionRequest(tx, attempt)
		return err
	})
	input := codexInspection(request)
	observation, err := ObserveProtocol(request, input)
	if err != nil || !observation.provesStopped(request) {
		t.Fatalf("normal exact RPC and host observations failed: %v", err)
	}
	input.Native.Response = json.RawMessage(`{"id":"unrelated-request","result":{"thread":{"id":"session","status":{"type":"idle"}}}}`)
	if _, err := ObserveProtocol(request, input); err == nil {
		t.Fatal("unrelated native RPC response was accepted")
	}
	input = codexInspection(request)
	input.Background.Complete = false
	observation, err = ObserveProtocol(request, input)
	if err != nil {
		t.Fatal(err)
	}
	if observation.BackgroundWorkStopped || observation.provesStopped(request) {
		t.Fatal("unknown background was treated as empty")
	}
	input = codexInspection(request)
	input.Background.NativeInputsComplete = false
	observation, err = ObserveProtocol(request, input)
	if err != nil {
		t.Fatal(err)
	}
	if observation.NativeStopped {
		t.Fatal("unknown queued native input set was assumed empty")
	}
	input = codexInspection(request)
	input.Continuation.Response = json.RawMessage(`{"id":"inspect-goal","result":{"goal":{"status":"active","updatedAt":42}}}`)
	observation, err = ObserveProtocol(request, input)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ContinuationStopped || !observation.GoalActive || observation.GoalVersion != 42 {
		t.Fatal("active native goal was treated as stopped")
	}
	input = codexInspection(request)
	input.Background.ProcessGeneration++
	if _, err := ObserveProtocol(request, input); err == nil {
		t.Fatal("host observation from another process generation was accepted")
	}
}

func TestNativeBackgroundProcessHelper(t *testing.T) {
	if os.Getenv("WORKRUNTIME_BACKGROUND_HELPER") != "1" {
		return
	}
	fmt.Println("background-ready")
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		os.Exit(2)
	}
}

func TestRealBackgroundChildRetainsOccupancyUntilObservedExit(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	f.report()
	child := exec.Command(os.Args[0], "-test.run=^TestNativeBackgroundProcessHelper$")
	child.Env = append(os.Environ(), "WORKRUNTIME_BACKGROUND_HELPER=1")
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = child.Process.Kill() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "background-ready\n" {
		t.Fatalf("child readiness: %q %v", line, err)
	}
	childExited := false
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		input := codexInspection(request)
		if !childExited {
			input.Background.RunningProcessIDs = []string{strconv.Itoa(child.Process.Pid)}
			input.Background.Evidence = json.RawMessage(fmt.Sprintf(`{"source":"owned-test-child","pid":%d,"wait_completed":false}`, child.Process.Pid))
		} else {
			input.Background.Evidence = json.RawMessage(fmt.Sprintf(`{"source":"owned-test-child","pid":%d,"wait_completed":true,"exit_code":%d}`, child.Process.Pid, child.ProcessState.ExitCode()))
		}
		return ObserveProtocol(request, input)
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err == nil {
		t.Fatal("live native background process was released")
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		if !occupied {
			t.Fatal("background process lost resource occupancy")
		}
		return nil
	})
	if _, err := stdin.Write([]byte("stop\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	childExited = true
	if !child.ProcessState.Exited() {
		t.Fatal("process exit was not observed")
	}
	if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		if occupied {
			t.Fatal("verified native/background quiescence did not release carrier")
		}
		return nil
	})
}

func TestReopenDisconnectKeepsCarrierAndLocationOccupied(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	if err := f.coordinator.Disconnect(context.Background(), "instance-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workstore.Open(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.store = reopened
	f.coordinator = New(reopened, Options{Driver: f.driver, Clock: func() time.Time { return f.now }})
	if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if !occupied || worker.State != work.WorkerRecovering {
			t.Fatal("restart inferred quiescence from lost transport")
		}
		_, err = tx.GetQuiescenceReceipt("attempt")
		if !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("reopen fabricated a quiescence receipt")
		}
		return nil
	})
	if f.driver.inspections != 0 {
		t.Fatal("active known native turn was treated as eligible for terminal reconciliation")
	}
}
