package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func privateStatusNativeFixture(t *testing.T, backends ...work.Backend) (*Store, work.WorkerSession, CarrierRegistration, agentproto.ExecutionBinding, ClaimRequest) {
	t.Helper()
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	backend := work.BackendClaude
	if len(backends) > 0 {
		backend = backends[0]
	}
	worker := work.WorkerSession{ID: "status-worker", ContextID: "c-r", ScopeID: "s", RoleID: "r", Backend: backend, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", CarrierIncarnation: "status-carrier", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, NativeNamespace: "fixture", NativeSessionID: "session", Revision: 1}
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "s", ContextID: "c-r", RoleID: "r", WorkerSessionID: worker.ID, SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := CarrierRegistration{Incarnation: worker.CarrierIncarnation, OriginInstanceID: worker.InstanceID, Owner: owner, Backend: worker.Backend, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	requireTx(t, s, func(tx *Tx) error {
		old, e := tx.GetWorker("w")
		if e != nil {
			return e
		}
		old.State = work.WorkerRetired
		old.Revision++
		if e = tx.PutWorker(old, 1); e != nil {
			return e
		}
		if e = tx.PutWorker(worker, 0); e != nil {
			return e
		}
		if e = tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		if e = tx.RegisterCarrier(carrier); e != nil {
			return e
		}
		return tx.PutInbox(testInput("input", "r", "c-r"), 0)
	})
	claim := testClaim()
	claim.Attempt.RunRef.WorkerID = worker.ID
	claim.StartOperation.TargetID = worker.ID
	claim.InstanceID = worker.InstanceID
	claim.LocationGeneration = 1
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	binding := owner
	binding.AttemptID = "attempt"
	binding.RunRef = "run"
	requireTx(t, s, func(tx *Tx) error {
		return tx.PutNativeTurn(NativeTurn{CarrierIncarnation: carrier.Incarnation, SessionID: "session", TurnID: "turn", Binding: binding, Started: true, LastSequence: 1, UpdatedAt: time.Now().UTC()})
	})
	return s, worker, carrier, binding, claim
}

func TestPrivateStatusCountsOnlyCurrentlyAnswerableNativeRequests(t *testing.T) {
	for _, scenario := range []string{"current", "response-pending", "old-controller", "old-bot", "old-session", "terminal-turn", "no-occupancy", "old-attempt", "old-worker"} {
		t.Run(scenario, func(t *testing.T) {
			s, worker, carrier, binding, claim := privateStatusNativeFixture(t)
			request := NativeRequest{ID: "question", ScopeID: "s", CarrierIncarnation: carrier.Incarnation, Binding: binding, NativeRequestID: "native-question", NativeSessionID: "session", NativeTurnID: "turn", State: "open", Revision: 1, CreatedAt: time.Now().UTC()}
			if scenario == "response-pending" {
				request.State = "response_pending"
			}
			if scenario == "old-controller" {
				request.Binding.ControllerGeneration = 2
			}
			if scenario == "old-session" {
				request.NativeSessionID = "old-session"
			}
			requireTx(t, s, func(tx *Tx) error { return tx.PutNativeRequest(request, 0) })
			if scenario == "old-bot" {
				requireTx(t, s, func(tx *Tx) error {
					return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "new-app", IdentityGeneration: 2}, 1)
				})
			}
			if scenario == "terminal-turn" {
				requireTx(t, s, func(tx *Tx) error {
					turn, e := tx.GetNativeTurn(carrier.Incarnation, "session", "turn")
					if e != nil {
						return e
					}
					turn.TerminalStatus = "completed"
					turn.TerminalSequence = 2
					return tx.PutNativeTurn(turn)
				})
			}
			if scenario == "no-occupancy" || scenario == "old-attempt" || scenario == "old-worker" {
				requireTx(t, s, func(tx *Tx) error { return tx.CommitOutcome(testOutcome(claim)) })
			}
			if scenario == "old-attempt" {
				requireTx(t, s, func(tx *Tx) error {
					w, e := tx.GetWorker(worker.ID)
					if e != nil {
						return e
					}
					if e = tx.PutInbox(testInput("input2", "r", "c-r"), 0); e != nil {
						return e
					}
					next := claim
					next.Attempt.ID = "attempt2"
					next.Attempt.RunRef.RunID = "run2"
					next.Attempt.CommandID = "command2"
					next.Attempt.InputIDs = []string{"input2"}
					next.StartOperation = testOperation("command2")
					next.StartOperation.TargetID = worker.ID
					next.ExpectedWorkerRevision = w.Revision
					return tx.ClaimAttempt(next)
				})
				requireTx(t, s, func(tx *Tx) error {
					a, e := tx.GetAttempt("attempt2")
					if e != nil {
						return e
					}
					a.State = work.AttemptRunning
					a.Revision++
					return tx.PutAttempt(a, 1)
				})
			}
			if scenario == "old-worker" {
				requireTx(t, s, func(tx *Tx) error {
					w, e := tx.GetWorker(worker.ID)
					if e != nil {
						return e
					}
					before := w.Revision
					w.Revision++
					w.State = work.WorkerRetired
					return tx.PutWorker(w, before)
				})
			}
			requireTx(t, s, func(tx *Tx) error {
				wc, e := tx.GetContext("c-r")
				if e != nil {
					return e
				}
				count, e := tx.privateStatusAnswerableRequests(wc)
				if e != nil {
					return e
				}
				want := 0
				if scenario == "current" {
					want = 1
				}
				if count != want {
					t.Fatalf("answerable=%d want %d", count, want)
				}
				saved, e := tx.GetNativeRequest(request.ID)
				if e != nil {
					return e
				}
				if saved.State != request.State || saved.Revision != 1 {
					t.Fatal("status read mutated native request")
				}
				return nil
			})
		})
	}
}
