package workruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func retiredInspectionFixture(t *testing.T, scenario string, control bool) (*fixture, InspectionRequest) {
	t.Helper()
	f := newFixture(t, work.BackendCodex, false, true, control)
	f.coordinator.options.EnqueueReconcile = func(string) {}
	f.coordinator.options.StateDir = filepath.Dir(f.path)
	f.coordinator.options.HostID = "local"
	f.start()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: f.command.CommandID, ThreadID: "session", TurnID: "turn-1", Status: "failed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime, Problem: &agentproto.ErrorInfo{Code: "modelNotFound", Message: "选定模型不支持当前账号"}}); err != nil {
		t.Fatal(err)
	}
	var request InspectionRequest
	f.tx(func(tx *workstore.Tx) error {
		attempt, e := tx.GetAttempt("attempt")
		if e != nil {
			return e
		}
		request, e = f.coordinator.inspectionRequest(tx, attempt)
		return e
	})
	snapshot := agentproto.RuntimeLaunchSnapshot{HostID: "local", Version: 1, Owner: f.carrier.Owner, CWD: t.TempDir(), InstructionBundle: agentproto.InstructionBundle{Revision: "v1", Text: "native isolated instructions"}, JournalPath: filepath.Join(f.coordinator.options.StateDir, "workers", "worker", "runtime.db"), CarrierIncarnation: f.carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	journal, err := runtimejournal.Open(t.Context(), snapshot.JournalPath, f.coordinator.options.StateDir, snapshot.CWD, snapshot.CarrierIncarnation)
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err = journal.PrepareCommand(f.command); err != nil {
		t.Fatal(err)
	}
	if _, err = journal.PrepareFrame(f.command.CommandID, []byte(`{"id":"old-input","method":"turn/start","params":{}}`)); err != nil {
		t.Fatal(err)
	}
	inspect := agentproto.Command{CommandID: "historical-inspection", Kind: agentproto.CommandRuntimeInspect, Execution: agentproto.CloneExecutionBinding(f.command.Execution), Target: agentproto.Target{ThreadID: request.SessionID, TurnID: request.TurnID}, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, SubjectCommandID: request.SubjectCommandID, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1, ExpectedLocationGeneration: 1}, IssuedAt: f.now}
	if scenario == "wrong-subject" {
		inspect.RuntimeInspection.SubjectCommandID = "different-prompt"
	}
	f.tx(func(tx *workstore.Tx) error {
		op, e := f.operation(tx, inspect)
		if e != nil {
			return e
		}
		op.Kind = "runtime.inspect"
		op.State = work.OperationSending
		op.Actor = work.Actor{Kind: work.ActorSystem, ID: "runtime-inspector"}
		_, _, e = tx.PutOperation(op, 0)
		return e
	})
	if err = f.coordinator.RegisterCommand(t.Context(), f.carrier.Incarnation, inspect); err != nil {
		t.Fatal(err)
	}
	if _, _, err = journal.PrepareCommand(inspect); err != nil {
		t.Fatal(err)
	}
	for _, frame := range []string{`{"id":"native-read","method":"thread/read","params":{"threadId":"session"}}`, `{"id":"goal-read","method":"thread/goal/get","params":{"threadId":"session"}}`} {
		if _, err = journal.PrepareFrame(inspect.CommandID, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	watermark, err := journal.NativeWriteWatermark()
	if err != nil {
		t.Fatal(err)
	}
	protocol := codexInspection(request)
	protocol.Native.Response = json.RawMessage(`{"id":"inspect-native","result":{"thread":{"id":"session","status":{"type":"systemError"},"turns":[{"id":"turn-1","status":"failed","items":[{"type":"userMessage"}],"error":{"codexErrorInfo":"modelNotFound"}}]}}}`)
	if scenario == "missing-background" {
		protocol.Background.Complete = false
	}
	result := agentproto.RuntimeInspectionResult{Version: 1, RequestCommandID: inspect.CommandID, CarrierIncarnation: f.carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, NativeWriteWatermark: watermark, Native: protocol.Native, Continuation: protocol.Continuation, Background: protocol.Background, ObservedAt: f.now}
	event := f.durable(agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: inspect.CommandID, Execution: agentproto.CloneExecutionBinding(inspect.Execution), ThreadID: request.SessionID, TurnID: request.TurnID, RuntimeInspection: &result})
	f.tx(func(tx *workstore.Tx) error {
		if _, _, e := tx.AcceptRuntimeEvent(f.carrier.OriginInstanceID, event, f.now, func(*workstore.Tx) error { return nil }); e != nil {
			return e
		}
		raw, e := json.Marshal(event)
		if e != nil {
			return e
		}
		if e = tx.PutContent(work.NewContent(inspect.CommandID+"_inspection_result", "scope", "application/json", raw, f.now)); e != nil {
			return e
		}
		op, e := tx.GetOperation(inspect.CommandID)
		if e != nil {
			return e
		}
		raw, e = json.Marshal(result)
		if e != nil {
			return e
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = string(raw)
		if _, _, e = tx.PutOperation(op, before); e != nil {
			return e
		}
		raw, e = json.Marshal(struct {
			Snapshot agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
		}{snapshot})
		if e != nil {
			return e
		}
		body := work.NewContent("retired-launch-body", "scope", "application/json", raw, f.now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: "retired-launch", ScopeID: "scope", Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "retired-launch", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, ExternalReceipt: "actual-temp-helper-launch", Revision: 1}, 0)
		return e
	})
	if scenario == "later-canonical" {
		extra := f.command
		extra.CommandID = "later-canonical-input"
		f.registerExtra(extra)
	}
	if scenario == "later-native" {
		extra := f.command
		extra.CommandID = "later-native-input"
		if _, _, err = journal.PrepareCommand(extra); err != nil {
			t.Fatal(err)
		}
		if _, err = journal.PrepareFrame(extra.CommandID, []byte(`{"id":"later-effect","method":"turn/start","params":{}}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	if scenario == "truncated-journal" {
		db, e := sql.Open("sqlite", snapshot.JournalPath)
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.Exec("DELETE FROM frames WHERE id>=?", watermark)
		db.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
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
		if e := tx.PutCarrierProcess(f.carrier.Incarnation, f.carrier.OriginInstanceID, agentproto.RuntimeProcessIdentity{HostID: "local", Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity}); e != nil {
			return e
		}
		if e := tx.SetCarrierConnected(f.carrier.Incarnation, scenario == "connected"); e != nil {
			return e
		}
		worker, e := tx.GetWorker("worker")
		if e != nil {
			return e
		}
		before := worker.Revision
		worker.Revision++
		worker.State = work.WorkerRecovering
		if e = tx.PutWorker(worker, before); e != nil {
			return e
		}
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: "worker", ScopeID: "scope", AttemptID: "attempt", Reason: "native_reconciliation", CreatedAt: f.now})
	})
	if scenario != "alive" {
		stdin.Close()
		if err = child.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "different-host" {
		f.coordinator.options.HostID = "another-host"
	}
	return f, request
}

func TestRetiredCommittedInspectionRequiresExactExitAndWriteFences(t *testing.T) {
	for _, scenario := range []string{"stopped", "alive", "different-host", "later-canonical", "later-native", "missing-background", "truncated-journal", "wrong-subject", "connected"} {
		t.Run(scenario, func(t *testing.T) {
			f, request := retiredInspectionFixture(t, scenario, false)
			observation, err := VerifyRetiredInspection(t.Context(), f.store, f.coordinator.options.StateDir, f.coordinator.options.HostID, request)
			if (err == nil) != (scenario == "stopped") {
				t.Fatalf("retirement scenario %s allowed=%v error=%v", scenario, err == nil, err)
			}
			if err == nil {
				if !observation.RetiredCarrierVerified || observation.RetirementEvidence == nil || observation.RetirementEvidence.Process.PID <= 0 || observation.RetirementEvidence.VerifiedHostID != "local" {
					t.Fatal("retirement lacks durable actual process/host evidence")
				}
				var probe RPCProbe
				if err = json.Unmarshal(observation.NativeEvidence, &probe); err != nil || len(probe.Response) == 0 {
					t.Fatal("retirement overwrote original native RPC probe")
				}
			}
			if scenario != "stopped" && scenario != "connected" {
				if e := f.coordinator.Reconcile(t.Context(), "attempt"); e == nil {
					t.Fatal("unsafe offline candidate unexpectedly reconciled")
				}
				f.tx(func(tx *workstore.Tx) error {
					input, e := tx.GetInbox("input")
					if e != nil {
						return e
					}
					if input.State != work.InboxClaimed {
						t.Fatal("rejected retirement candidate consumed original input")
					}
					return nil
				})
			}
			f.tx(func(tx *workstore.Tx) error {
				occupied, e := tx.HasCarrierOccupancy(f.carrier, "attempt")
				if !occupied {
					t.Fatal("proof verification itself released occupancy")
				}
				return e
			})
		})
	}
}

func TestRetiredLegacyPrivateModelFailureReconcilesWithoutOfflineRPC(t *testing.T) {
	for _, control := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "control"}[control], func(t *testing.T) {
			f, _ := retiredInspectionFixture(t, "stopped", control)
			calls := 0
			f.driver.inspect = func(InspectionRequest) (NativeObservation, error) {
				calls++
				return NativeObservation{}, errors.New("offline transport must not be retried")
			}
			if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatal("offline retirement sent another native RPC")
			}
			f.tx(func(tx *workstore.Tx) error {
				attempt, e := tx.GetAttempt("attempt")
				if e != nil {
					return e
				}
				input, e := tx.GetInbox("input")
				if e != nil {
					return e
				}
				if attempt.State != work.AttemptFailed || input.State != work.InboxProcessed {
					t.Fatalf("old failure remains stuck: attempt=%s input=%s", attempt.State, input.State)
				}
				if control {
					original, e := tx.GetInbox("original-control-input")
					if e != nil {
						return e
					}
					if original.State != work.InboxPending {
						t.Fatal("retired control failure consumed original business")
					}
				}
				gate, e := tx.GetRuntimeGate("worker")
				if e != nil {
					return e
				}
				worker, e := tx.GetWorker("worker")
				if e != nil {
					return e
				}
				if gate.Reason != "snapshot_rebuild_required" || gate.AttemptID != "attempt" || worker.State != work.WorkerRecovering {
					t.Fatalf("dead carrier became dispatchable: gate=%+v worker=%s", gate, worker.State)
				}
				proof, e := tx.GetQuiescenceReceipt("attempt")
				if e != nil {
					return e
				}
				content, e := tx.GetContent("scope", proof.Source.EvidenceRef)
				if e != nil {
					return e
				}
				var observation NativeObservation
				if e = json.Unmarshal(content.Data, &observation); e != nil {
					return e
				}
				if observation.RetirementEvidence == nil {
					t.Fatal("quiescence failed to preserve retirement evidence")
				}
				publications, e := tx.PrivateRunPublications("scope", "context", "run")
				if e != nil {
					return e
				}
				actualError := false
				for _, op := range publications {
					p, e := workservice.ReadMessageProjection(tx, op)
					if e != nil {
						return e
					}
					actualError = actualError || strings.Contains(string(p.Card), "选定模型不支持当前账号")
				}
				if !actualError {
					t.Fatal("actual native failure never reached private output")
				}
				return nil
			})
		})
	}
}

func TestRetiredShutdownRevocationsDoNotBlockPrivateFailure(t *testing.T) {
	for _, scenario := range []string{"clear", "interrupt", "wrong-actor", "wrong-session", "goal-set", "missing-retirement", "missing-evidence", "changed-hash"} {
		t.Run(scenario, func(t *testing.T) {
			f, request := retiredInspectionFixture(t, "stopped", true)
			command := agentproto.Command{Kind: agentproto.CommandThreadGoalClear, Execution: agentproto.CloneExecutionBinding(&request.Binding), Target: agentproto.Target{ThreadID: request.SessionID}, IssuedAt: f.now}
			if scenario == "interrupt" {
				command.Kind = agentproto.CommandTurnInterrupt
				command.Target.TurnID = request.TurnID
			}
			if scenario == "wrong-session" {
				command.Target.ThreadID = "another-session"
			}
			if scenario == "goal-set" {
				command.Kind = agentproto.CommandThreadGoalSet
			}
			identity, _ := json.Marshal([]string{request.Carrier.Incarnation, request.Binding.AttemptID, string(command.Kind), command.Target.ThreadID, command.Target.TurnID})
			sum := sha256.Sum256(identity)
			command.CommandID = "shutdown_" + hex.EncodeToString(sum[:20])
			f.tx(func(tx *workstore.Tx) error {
				op, e := f.operation(tx, command)
				if e != nil {
					return e
				}
				op.Actor = work.Actor{Kind: work.ActorSystem, ID: "daemon-shutdown"}
				op.State = work.OperationSending
				if scenario == "wrong-actor" {
					op.Actor.ID = "external-command"
				}
				if _, _, e = tx.PutOperation(op, 0); e != nil {
					return e
				}
				if _, e = tx.RegisterNativeCommand(f.carrier.Incarnation, command); e != nil {
					return e
				}
				op.Revision++
				op.State = work.OperationUnknown
				op.LastError = "instance offline"
				_, _, e = tx.PutOperation(op, 1)
				return e
			})
			observation, e := VerifyRetiredInspection(t.Context(), f.store, f.coordinator.options.StateDir, f.coordinator.options.HostID, request)
			if scenario == "goal-set" {
				if e == nil {
					t.Fatal("later goal set must invalidate proof")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "missing-retirement" {
				observation.RetiredCarrierVerified = false
			}
			if scenario == "missing-evidence" {
				observation.RetirementEvidence = nil
			}
			if scenario == "changed-hash" {
				f.tx(func(tx *workstore.Tx) error {
					op, e := tx.GetOperation(command.CommandID)
					if e != nil {
						return e
					}
					op.RequestHash = "tampered"
					allowed, e := privateOutcomeRetiredShutdown(tx, op, request, observation)
					if e == nil && allowed {
						t.Fatal("mismatched original body/hash accepted")
					}
					return nil
				})
				return
			}
			positive := scenario == "clear" || scenario == "interrupt"
			f.tx(func(tx *workstore.Tx) error {
				attempt, e := tx.GetAttempt("attempt")
				if e != nil {
					return e
				}
				settled, e := privateOutcomeEffectsSettled(tx, attempt, request, observation)
				if positive && (e != nil || !settled) {
					t.Fatalf("shutdown revocation blocks failed request: %v", e)
				}
				if !positive && e == nil && settled {
					t.Fatal("unproved effect treated as settled")
				}
				return nil
			})
			if positive {
				if e = f.coordinator.Reconcile(t.Context(), "attempt"); e != nil {
					t.Fatal(e)
				}
				f.tx(func(tx *workstore.Tx) error {
					attempt, e := tx.GetAttempt("attempt")
					if e != nil {
						return e
					}
					op, e := tx.GetOperation(command.CommandID)
					if e != nil {
						return e
					}
					if attempt.State != work.AttemptFailed || op.State != work.OperationUnknown {
						t.Fatal("settlement changed historical unknown or retained attempt")
					}
					return nil
				})
			}
		})
	}
}
