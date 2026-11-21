// Package workcontrol translates persisted admission gates into exact native
// stop commands. It does not equate a sent interrupt with a stopped execution.
package workcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Driver interface {
	SendCommand(context.Context, string, agentproto.Command) error
}
type Stops struct {
	store  *workstore.Store
	driver Driver
	now    func() time.Time
}

func NewStops(store *workstore.Store, driver Driver) *Stops {
	return &Stops{store: store, driver: driver, now: time.Now}
}

type stopDispatch struct {
	instance  string
	command   agentproto.Command
	operation work.Operation
}

func (s *Stops) RunOnce(ctx context.Context) error {
	var pending []stopDispatch
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, scope := range scopes {
			if e = s.finishObservedStops(tx, scope.ID); e != nil {
				return e
			}
			gates, e := tx.ListPauseGates(scope.ID)
			if e != nil {
				return e
			}
			if len(gates) == 0 {
				continue
			}
			attempts, e := tx.ListAttempts(scope.ID)
			if e != nil {
				return e
			}
			for _, attempt := range attempts {
				if _, e := tx.GetQuiescenceReceipt(attempt.ID); e == nil {
					continue
				} else if !errors.Is(e, workstore.ErrNotFound) {
					return e
				}
				wc, e := tx.GetContext(attempt.ContextID)
				if e != nil {
					return e
				}
				allowed, e := work.GatesAllowRunForPurpose(scope.ID, attempt.TaskID, attempt.ContextID, wc.Purpose, gates)
				if e != nil {
					return e
				}
				if allowed {
					continue
				}
				worker, e := tx.GetWorker(attempt.RunRef.WorkerID)
				if e != nil {
					return e
				}
				if worker.Controller != work.ControllerAutomatic || work.ValidateAttemptBinding(attempt, worker) != nil {
					continue
				}
				carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
				if e != nil {
					return e
				}
				if !carrier.Matches(worker) {
					continue
				}
				binding := carrier.Owner
				binding.AttemptID = attempt.ID
				binding.RunRef = attempt.RunRef.RunID
				if worker.Backend == work.BackendCodex && worker.NativeSessionID != "" {
					command := agentproto.Command{Kind: agentproto.CommandThreadGoalClear, Execution: &binding, Target: agentproto.Target{ThreadID: worker.NativeSessionID}}
					dispatch, created, e := s.prepare(tx, worker, command, gates)
					if e != nil {
						return e
					}
					if created {
						pending = append(pending, dispatch)
					}
				}
				turns, e := tx.ListNativeTurns(attempt.ID)
				if e != nil {
					return e
				}
				for _, turn := range turns {
					if turn.TerminalStatus != "" || turn.CarrierIncarnation != carrier.Incarnation {
						continue
					}
					command := agentproto.Command{Kind: agentproto.CommandTurnInterrupt, Execution: &binding, Target: agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}}
					dispatch, created, e := s.prepare(tx, worker, command, gates)
					if e != nil {
						return e
					}
					if created {
						pending = append(pending, dispatch)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, dispatch := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = s.driver.SendCommand(ctx, dispatch.instance, dispatch.command)
		if err != nil {
			failures = append(failures, err)
			_ = s.store.Within(ctx, func(tx *workstore.Tx) error {
				op, e := tx.GetOperation(dispatch.operation.ID)
				if e != nil {
					return e
				}
				if op.State != work.OperationSending {
					return nil
				}
				expected := op.Revision
				op.Revision++
				op.State = work.OperationUnknown
				op.LastError = err.Error()
				op.UpdatedAt = s.now()
				_, _, e = tx.PutOperation(op, expected)
				return e
			})
		}
	}
	return errors.Join(failures...)
}

func (s *Stops) finishObservedStops(tx *workstore.Tx, scopeID string) error {
	ops, err := tx.ListOperations(scopeID)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if op.Kind != "native.gate_stop" || op.State == work.OperationConfirmed || op.State == work.OperationTerminal {
			continue
		}
		body, err := tx.GetContent(scopeID, op.RequestRef)
		if err != nil {
			return err
		}
		var command agentproto.Command
		if err = json.Unmarshal(body.Data, &command); err != nil {
			return err
		}
		if command.Execution == nil {
			return fmt.Errorf("stop command lost execution binding")
		}
		proof, err := tx.GetQuiescenceReceipt(command.Execution.AttemptID)
		if errors.Is(err, workstore.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if proof.RunRef.WorkerID != command.Execution.WorkerSessionID || proof.RunRef.RunID != command.Execution.RunRef || proof.RunRef.Generation != command.Execution.SessionGeneration {
			return fmt.Errorf("stop proof belongs to another execution")
		}
		receipt, err := json.Marshal(struct {
			Condition string         `json:"condition"`
			Evidence  work.SourceRef `json:"evidence"`
		}{"native_continuation_and_background_stopped", proof.Source})
		if err != nil {
			return err
		}
		expected := op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = string(receipt)
		op.UpdatedAt = s.now()
		if _, _, err = tx.PutOperation(op, expected); err != nil {
			return err
		}
	}
	return nil
}

func (s *Stops) prepare(tx *workstore.Tx, worker work.WorkerSession, command agentproto.Command, gates []work.PauseGate) (stopDispatch, bool, error) {
	var result stopDispatch
	identity, _ := json.Marshal([]string{worker.CarrierIncarnation, command.Execution.AttemptID, string(command.Kind), command.Target.ThreadID, command.Target.TurnID})
	hash := sha256.Sum256(identity)
	command.CommandID = "gate_stop_" + hex.EncodeToString(hash[:20])
	if _, err := tx.GetOperation(command.CommandID); err == nil {
		return result, false, nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return result, false, err
	}
	command.IssuedAt = s.now()
	raw, err := json.Marshal(command)
	if err != nil {
		return result, false, err
	}
	body := work.NewContent(command.CommandID+"_body", worker.ScopeID, "application/json", raw, s.now())
	if err = tx.PutContent(body); err != nil {
		return result, false, err
	}
	op := work.Operation{ID: command.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "admission-gate"}, Kind: "native.gate_stop", TargetID: worker.ID, TargetGeneration: worker.Generation, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: command.CommandID, State: work.OperationSending, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	for _, gate := range gates {
		if gate.State == work.GateClosed {
			op.Steps = append(op.Steps, work.OperationStep{ID: "gate:" + gate.ID, State: work.OperationConfirmed, Receipt: fmt.Sprint(gate.Revision)})
		}
	}
	if _, _, err = tx.PutOperation(op, 0); err != nil {
		return result, false, err
	}
	return stopDispatch{instance: worker.InstanceID, command: command, operation: op}, true, nil
}
