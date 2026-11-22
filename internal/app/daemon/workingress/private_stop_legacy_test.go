package workingress

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestLegacyPrivateStopUpgradeAdmitsNewSDKInput(t *testing.T) {
	for _, settled := range []bool{false, true} {
		t.Run(fmt.Sprintf("previously_settled_%t", settled), func(t *testing.T) {
			ingress, store, _ := ingressFixture(t, false)
			receive(t, ingress, rawMessage("leader", "legacy-old-human", "dm", "p2p", "open-person", "person", "旧的执行"))
			process(t, ingress)
			stop := rawMessage("leader", "legacy-old-stop", "dm", "p2p", "open-person", "person", "/stop")
			stop.ReceivedAt = ingressNow.Add(3 * time.Second)
			source, nav := privateCommandSourceFromIngress(t, ingress, store, stop)
			var worker work.WorkerSession
			var old work.ExecutionAttempt
			var oldInputID string
			gateID := workstore.LegacyPrivateStopGateID(nav.PersonalScopeID, nav.FrontContextID)
			txRun(t, store, func(tx *workstore.Tx) error {
				scope, proof, err := tx.ValidatePrivateAction(source)
				if err != nil {
					return err
				}
				worker = work.WorkerSession{ID: "legacy-worker", ScopeID: scope.ID, ContextID: nav.FrontContextID, RoleID: scope.RoleID, Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "carrier", LocationID: "legacy-location", LocationGeneration: 1, Revision: 1}
				if err = tx.PutLocation(work.ExecutionLocation{ID: worker.LocationID, HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); err != nil {
					return err
				}
				if err = tx.PutWorker(worker, 0); err != nil {
					return err
				}
				inputs, err := tx.ListInbox(scope.ID)
				if err != nil || len(inputs) != 1 {
					t.Fatalf("old actual private input: %+v %v", inputs, err)
				}
				oldInputID = inputs[0].ID
				old = legacyStopClaim(worker, oldInputID, "old", ingressNow.Add(time.Second))
				if err = tx.ClaimAttempt(legacyStopClaimRequest(worker, old)); err != nil {
					return err
				}
				old.Revision++
				old.State = work.AttemptRunning
				if err = tx.PutAttempt(old, old.Revision-1); err != nil {
					return err
				}

				gate := work.PauseGate{ID: gateID, ScopeID: scope.ID, ContextID: nav.FrontContextID, Source: work.GateContext, SourceID: scope.PrincipalID, State: work.GateClosed, Revision: 1}
				if err = tx.PutPauseGate(gate, 0); err != nil {
					return err
				}
				receipt, err := json.Marshal(map[string]any{"status": "completed", "object_id": gate.ContextID, "revision": gate.Revision})
				if err != nil {
					return err
				}
				request := work.NewContent("legacy-stop-request", scope.ID, "text/plain", []byte(proof.EvidenceRef), stop.ReceivedAt)
				if err = tx.PutContent(request); err != nil {
					return err
				}
				op := work.Operation{ID: "legacy-stop-op", ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: "private.stop", TargetID: scope.ID, TargetGeneration: 1, IdempotencyKey: "legacy-stop", RequestRef: request.ID, RequestHash: request.SHA256, State: work.OperationConfirmed, ExternalReceipt: string(receipt), Revision: 1, CreatedAt: stop.ReceivedAt, UpdatedAt: stop.ReceivedAt}
				if _, _, err = tx.PutOperation(op, 0); err != nil {
					return err
				}
				if err = tx.PutContext(work.WorkContext{ID: "other", Workstream: "other", ScopeID: scope.ID, RoleID: scope.RoleID, Revision: 1}, 0); err != nil {
					return err
				}
				if err = tx.PutPauseGate(work.PauseGate{ID: "unrelated-intentional-control", ScopeID: scope.ID, ContextID: "other", Source: work.GateHumanControl, SourceID: scope.PrincipalID, State: work.GateClosed, Revision: 1}, 0); err != nil {
					return err
				}
				if settled {
					return settleLegacyPrivateStop(tx, old, stop.ReceivedAt.Add(time.Second), true)
				}
				return nil
			})

			newInput := rawMessage("leader", "new-after-legacy-stop", "dm", "p2p", "open-person", "person", "开始新的工作")
			newInput.ReceivedAt = stop.ReceivedAt.Add(2 * time.Second)
			receive(t, ingress, newInput)
			process(t, ingress)

			txRun(t, store, func(tx *workstore.Tx) error {
				gate, err := tx.GetPauseGate(gateID)
				if err != nil || gate.State != work.GateOpen || gate.Revision != 2 {
					t.Fatalf("exact migrated private gate was not released on new SDK ingress: %+v %v", gate, err)
				}
				other, err := tx.GetPauseGate("unrelated-intentional-control")
				if err != nil || other.State != work.GateClosed {
					t.Fatalf("independent human gate was reopened: %+v %v", other, err)
				}
				marked, err := tx.PrivateAttemptStopped(old)
				if err != nil || !marked {
					t.Fatalf("old attempt has no persistent stop fence: %t %v", marked, err)
				}
				if !settled {
					if err = settleLegacyPrivateStop(tx, old, newInput.ReceivedAt.Add(time.Second), false); err != nil {
						return err
					}
				}
				previous, err := tx.GetInbox(oldInputID)
				if err != nil || previous.State != work.InboxCancelled {
					t.Fatalf("old input could start again: %+v %v", previous, err)
				}
				pending, err := tx.ListPendingInputs(nav.PersonalScopeID, worker.RoleID)
				if err != nil || len(pending) != 1 {
					t.Fatalf("new input is the only runnable private input: %+v %v", pending, err)
				}
				msg, err := tx.GetMessage(pending[0].MessageID)
				if err != nil || msg.PlatformKey == nil || msg.PlatformKey.MessageID != newInput.MessageID {
					t.Fatalf("unexpected surviving private input: %+v %v", msg, err)
				}
				worker, err = tx.GetWorker(worker.ID)
				if err != nil {
					return err
				}
				next := legacyStopClaim(worker, pending[0].ID, "new", newInput.ReceivedAt.Add(2*time.Second))
				return tx.ClaimAttempt(legacyStopClaimRequest(worker, next))
			})
		})
	}
}

func legacyStopClaim(worker work.WorkerSession, inputID, tag string, at time.Time) work.ExecutionAttempt {
	return work.ExecutionAttempt{ID: tag + "-attempt", ScopeID: worker.ScopeID, ContextID: worker.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: worker.Generation, RunID: tag + "-run"}, InputIDs: []string{inputID}, CommandID: tag + "-native-command", State: work.AttemptDispatchPending, Revision: 1, CreatedAt: at, UpdatedAt: at}
}

func legacyStopClaimRequest(worker work.WorkerSession, attempt work.ExecutionAttempt) workstore.ClaimRequest {
	op := work.Operation{ID: attempt.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "test-scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: attempt.CommandID, RequestHash: "prompt", State: work.OperationPrepared, Revision: 1}
	return workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: worker.ControllerGeneration, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration, StartOperation: op, Now: attempt.CreatedAt}
}

func settleLegacyPrivateStop(tx *workstore.Tx, attempt work.ExecutionAttempt, at time.Time, oldGate bool) error {
	worker, err := tx.GetWorker(attempt.RunRef.WorkerID)
	if err != nil {
		return err
	}
	content := work.NewContent("legacy-stop-native-proof", attempt.ScopeID, "text/plain", []byte("exact native and background cessation"), at)
	if err = tx.PutContent(content); err != nil {
		return err
	}
	proof := work.SourceRef{Kind: "native_terminal", ID: content.ID, Version: "1", EvidenceRef: content.ID}
	if err = tx.CommitOutcome(workstore.OutcomeRequest{AttemptID: attempt.ID, RunRef: attempt.RunRef, ExpectedAttemptRevision: attempt.Revision, OutcomeRef: content.ID, TerminalState: work.AttemptInterrupted, TerminalEvidence: []work.SourceRef{proof}, Now: at}); err != nil {
		return err
	}
	worker, err = tx.GetWorker(worker.ID)
	if err != nil {
		return err
	}
	stopped := work.QuiescenceEvidence{RunRef: attempt.RunRef, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, CarrierIncarnation: worker.CarrierIncarnation, NativeStopped: true, ContinuationStopped: true, BackgroundWorkStopped: true, Source: proof, ObservedAt: at}
	if err = tx.ReconcileQuiescence(workstore.ReconcileQuiescenceRequest{Evidence: stopped, ExpectedWorkerRevision: worker.Revision}); err != nil || !oldGate {
		return err
	}
	if err = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attempt.ID, Reason: "missing_processing_report", CreatedAt: at}); err != nil {
		return err
	}
	worker, err = tx.GetWorker(worker.ID)
	if err != nil {
		return err
	}
	worker.State = work.WorkerUnavailable
	worker.Revision++
	return tx.PutWorker(worker, worker.Revision-1)
}
