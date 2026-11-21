package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The relay ingress stays alive until this returns. Disconnect and deadline
// leave occupied native work recoverable; they never manufacture release.
func (a *App) shutdownWorkRuntime(ctx context.Context) error {
	r := a.teamRuntime
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	r.active.Store(false)
	if r.runCancel != nil {
		r.runCancel()
	}
	if r.runDone != nil {
		select {
		case <-r.runDone:
		case <-ctx.Done():
			return fmt.Errorf("team scheduler did not stop: %w", ctx.Err())
		}
	}
	if r.engine != nil {
		if err := r.engine.DrainPreparations(ctx); err != nil {
			return fmt.Errorf("team carrier preparation did not stop: %w", err)
		}
	}
	var workers []work.WorkerSession
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		scopes, e := tx.ListScopes()
		if e != nil {
			return e
		}
		for _, scope := range scopes {
			list, e := tx.ListWorkers(scope.ID)
			if e != nil {
				return e
			}
			for _, worker := range list {
				if worker.InstanceID == "" || worker.CarrierIncarnation == "" || worker.State == work.WorkerRetired {
					continue
				}
				workers = append(workers, worker)
				if _, e = tx.GetRuntimeGate(worker.ID); errors.Is(e, workstore.ErrNotFound) {
					if e = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "daemon_shutdown", CreatedAt: time.Now().UTC()}); e != nil {
						return e
					}
				} else if e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
	slots := make(chan struct{}, 4)
	for _, worker := range workers {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				mu.Lock()
				failures = append(failures, ctx.Err())
				mu.Unlock()
				return
			}
			if e := a.shutdownWorkCarrier(ctx, worker); e != nil {
				mu.Lock()
				failures = append(failures, fmt.Errorf("worker %s: %w", worker.ID, e))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(failures...)
}

func (a *App) shutdownWorkCarrier(ctx context.Context, worker work.WorkerSession) error {
	r := a.teamRuntime
	if worker.Controller == work.ControllerHuman {
		return fmt.Errorf("human carrier and location reservation remain owned until actual human-process reconciliation")
	}
	var carrier workstore.CarrierRegistration
	var attempt *work.ExecutionAttempt
	var commands []agentproto.Command
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		carrier, e = tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(worker) {
			return workstore.ErrConflict
		}
		attempts, e := tx.ListAttempts(worker.ScopeID)
		if e != nil {
			return e
		}
		for _, candidate := range attempts {
			if candidate.RunRef.WorkerID != worker.ID || candidate.RunRef.Generation != worker.Generation {
				continue
			}
			occupied, e := tx.HasCarrierOccupancy(carrier, candidate.ID)
			if e != nil {
				return e
			}
			if occupied {
				if attempt != nil {
					return fmt.Errorf("carrier has multiple occupied attempt identities")
				}
				copy := candidate
				attempt = &copy
			}
		}
		if attempt == nil {
			return nil
		}
		binding := carrier.Owner
		binding.AttemptID = attempt.ID
		binding.RunRef = attempt.RunRef.RunID
		turns, e := tx.ListNativeTurns(attempt.ID)
		if e != nil {
			return e
		}
		sessions := map[string]bool{}
		if worker.NativeSessionID != "" {
			sessions[worker.NativeSessionID] = true
		}
		for _, turn := range turns {
			if turn.CarrierIncarnation != carrier.Incarnation || turn.Binding != binding {
				continue
			}
			sessions[turn.SessionID] = true
			if turn.TerminalStatus == "" {
				commands = append(commands, agentproto.Command{Kind: agentproto.CommandTurnInterrupt, Execution: &binding, Target: agentproto.Target{ThreadID: turn.SessionID, TurnID: turn.TurnID}})
			}
		}
		if worker.Backend == work.BackendCodex {
			for session := range sessions {
				commands = append([]agentproto.Command{{Kind: agentproto.CommandThreadGoalClear, Execution: &binding, Target: agentproto.Target{ThreadID: session}}}, commands...)
			}
		}
		if len(turns) == 0 {
			return fmt.Errorf("occupied native input has no proven turn; shutdown preserves its unknown ownership")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, command := range commands {
		if err = a.sendShutdownControl(ctx, worker, command); err != nil {
			return err
		}
	}
	if attempt != nil {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			_ = r.coordinator.Reconcile(ctx, attempt.ID)
			found := false
			err = r.store.Within(ctx, func(tx *workstore.Tx) error {
				proof, e := tx.GetQuiescenceReceipt(attempt.ID)
				if errors.Is(e, workstore.ErrNotFound) {
					return nil
				}
				if e != nil {
					return e
				}
				if proof.CarrierIncarnation != carrier.Incarnation || proof.RunRef != attempt.RunRef {
					return workstore.ErrConflict
				}
				found = true
				return nil
			})
			if err != nil {
				return err
			}
			if found {
				break
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("native stop remains unproven: %w", ctx.Err())
			case <-tick.C:
			}
		}
	}
	var gate workstore.RuntimeGate
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		worker, e = tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		gate, e = tx.GetRuntimeGate(worker.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			e = nil
		}
		return e
	})
	if err != nil {
		return err
	}
	if gate.Reason != "" && gate.Reason != "daemon_shutdown" && gate.Reason != "snapshot_rebuild_required" {
		// A missing business report or independent resource gate survives shutdown.
		observation, e := r.driver.Inspect(ctx, workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration})
		if e != nil {
			return e
		}
		request := workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
		if e = observation.ValidateStopped(request); e != nil {
			return e
		}
		return a.stopWorkCarrier(ctx, worker)
	}
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, e := tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		if fresh.Generation != worker.Generation || fresh.CarrierIncarnation != carrier.Incarnation {
			return workstore.ErrConflict
		}
		if e = tx.ClearRuntimeGate(worker.ID, "daemon_shutdown"); e != nil {
			return e
		}
		attemptID := ""
		if attempt != nil {
			attemptID = attempt.ID
		}
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attemptID, Reason: "snapshot_rebuild_required", CreatedAt: time.Now().UTC()})
	})
	if err != nil {
		return err
	}
	return r.host.Retire(ctx, worker.ID)
}

func (a *App) sendShutdownControl(ctx context.Context, worker work.WorkerSession, command agentproto.Command) error {
	r := a.teamRuntime
	identity, _ := json.Marshal([]string{worker.CarrierIncarnation, command.Execution.AttemptID, string(command.Kind), command.Target.ThreadID, command.Target.TurnID})
	sum := sha256.Sum256(identity)
	command.CommandID = "shutdown_" + hex.EncodeToString(sum[:20])
	command.IssuedAt = time.Now().UTC()
	send := false
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, e := tx.GetOperation(command.CommandID); e == nil {
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		raw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		body := work.NewContent(command.CommandID+":body", worker.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(work.Operation{ID: command.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "daemon-shutdown"}, Kind: string(command.Kind), TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, 0)
		send = e == nil
		return e
	})
	if err != nil || !send {
		return err
	}
	err = r.driver.SendCommand(ctx, worker.InstanceID, command)
	if err != nil {
		_ = r.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			op, e := tx.GetOperation(command.CommandID)
			if e != nil {
				return e
			}
			if op.State != work.OperationSending {
				return nil
			}
			before := op.Revision
			op.State = work.OperationUnknown
			op.LastError = err.Error()
			op.Revision++
			op.UpdatedAt = time.Now().UTC()
			_, _, e = tx.PutOperation(op, before)
			return e
		})
	}
	return err
}
