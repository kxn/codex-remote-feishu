package workruntime

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) deferPrivateShellBoundary(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) (bool, error) {
	if (event.Kind != agentproto.EventTurnStarted && event.Kind != agentproto.EventTurnCompleted) || event.CommandID != "" || event.Execution != nil || event.ThreadID == "" || event.TurnID == "" {
		return false, nil
	}
	if _, err := tx.GetNativeTurn(carrier.Incarnation, event.ThreadID, event.TurnID); err == nil {
		return false, nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return false, err
	}
	return tx.HasUnassignedPrivateShellWrites(carrier.Incarnation, event.ThreadID)
}

// A native turn boundary without RPC identity cannot establish ownership. Save
// it durably until the exact managed UserShell item supplies the actual tuple.
func (c *Coordinator) applyPrivateShellBoundary(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) (bool, string, error) {
	if (event.Kind != agentproto.EventTurnStarted && event.Kind != agentproto.EventTurnCompleted) || event.ThreadID == "" || event.TurnID == "" {
		return false, "", nil
	}
	if _, err := tx.GetNativeTurn(carrier.Incarnation, event.ThreadID, event.TurnID); err == nil {
		return false, "", nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return false, "", err
	}
	if event.Execution != nil && event.Execution.AttemptID != "" {
		return false, "", nil
	}
	if event.CommandID != "" {
		command, e := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
		if e != nil || command.Kind != agentproto.CommandThreadShellCommand {
			return false, "", nil
		}
	}
	operations, err := tx.PrivateShellOperationsForTurn(carrier.Incarnation, event.ThreadID, event.TurnID)
	if err != nil {
		return false, "", err
	}
	if len(operations) > 0 {
		for _, op := range operations {
			if err = tx.RecordPrivateShellTurnBoundary(op.ID, event, c.options.Clock()); err != nil {
				return true, "", err
			}
		}
		attempt, err := c.finishPrivateShellBoundaryProof(tx, carrier)
		return true, attempt, err
	}
	deferBoundary, err := c.deferPrivateShellBoundary(tx, carrier, event)
	if err != nil || !deferBoundary {
		return false, "", err
	}
	if err = tx.QuarantineRuntimeEvent(event, "native_ownership_unproven"); err != nil {
		return true, "", err
	}
	worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
	if err != nil {
		return true, "", err
	}
	if carrier.Matches(worker) {
		if _, e := tx.GetRuntimeGate(worker.ID); errors.Is(e, workstore.ErrNotFound) {
			// This provisional ownership question does not change a running worker's
			// state. Its exact proof removes only this gate, preserving other recovery.
			err = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "native_ownership_unproven", CreatedAt: c.options.Clock()})
		} else if e != nil {
			err = e
		}
	}
	return true, "", err
}
func (c *Coordinator) resolvePrivateShellBoundaries(tx *workstore.Tx, carrier workstore.CarrierRegistration, op workstore.PrivateShellOperation) (string, error) {
	if op.ActualTurnID == "" {
		return "", nil
	}
	if _, err := tx.GetNativeTurn(carrier.Incarnation, op.SessionID, op.ActualTurnID); err == nil {
		return "", nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return "", err
	}
	pending, err := tx.QuarantinedNativeEvents(carrier.Incarnation, op.SessionID, op.ActualTurnID)
	if err != nil {
		return "", err
	}
	for _, event := range pending {
		if event.Kind != agentproto.EventTurnStarted && event.Kind != agentproto.EventTurnCompleted {
			continue
		}
		if event.Execution != nil && event.Execution.AttemptID != "" {
			continue
		}
		if event.CommandID != "" && event.CommandID != op.CommandID {
			continue
		}
		if err = tx.RecordPrivateShellTurnBoundary(op.ID, event, c.options.Clock()); err != nil {
			return "", err
		}
	}
	return c.finishPrivateShellBoundaryProof(tx, carrier)
}
func (c *Coordinator) finishPrivateShellBoundaryProof(tx *workstore.Tx, carrier workstore.CarrierRegistration) (string, error) {
	count, err := tx.CountUnresolvedNativeEvents(carrier.Incarnation)
	if err != nil {
		return "", err
	}
	worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
	if err != nil {
		return "", err
	}
	if !carrier.Matches(worker) {
		return "", nil
	}
	if count == 0 {
		gate, e := tx.GetRuntimeGate(worker.ID)
		if e == nil && gate.Reason == "native_ownership_unproven" && gate.RecoveryReason == "" {
			if e = tx.ClearRuntimeGate(worker.ID, gate.Reason); e != nil {
				return "", e
			}
		} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return "", e
		}
	}
	attempt, err := tx.GetOccupiedAttemptForWorker(worker.ID)
	if errors.Is(err, workstore.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if attempt.State != work.AttemptRunning && attempt.State != work.AttemptReconciling {
		return "", nil
	}
	return attempt.ID, nil
}

// A later exact model binding supersedes provisional standalone classification.
// Replay the original durable boundaries, never synthetic replacements.
func (c *Coordinator) replayPrivateShellBoundariesForModel(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) error {
	operations, err := tx.PrivateShellOperationsForTurn(carrier.Incarnation, event.ThreadID, event.TurnID)
	if err != nil {
		return err
	}
	seen := map[uint64]bool{}
	var events []agentproto.Event
	for _, op := range operations {
		for _, source := range []*work.SourceRef{op.TurnStartedSource, op.TurnTerminalSource} {
			if source == nil {
				continue
			}
			body, e := tx.GetContent(op.ScopeID, source.EvidenceRef)
			if e != nil {
				return e
			}
			var original agentproto.Event
			if e = json.Unmarshal(body.Data, &original); e != nil {
				return e
			}
			if original.Journal == nil || original.Journal.CarrierIncarnation != carrier.Incarnation || original.ThreadID != event.ThreadID || original.TurnID != event.TurnID {
				return workstore.ErrScopeDenied
			}
			hash, e := agentproto.RuntimeEventPayloadHash(original)
			if e != nil {
				return e
			}
			if hash != source.Version || hash != original.Journal.PayloadHash {
				return workstore.ErrScopeDenied
			}
			if original.Execution != nil || original.CommandID != "" || seen[original.Journal.Sequence] {
				continue
			}
			seen[original.Journal.Sequence] = true
			events = append(events, original)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Journal.Sequence < events[j].Journal.Sequence })
	for _, original := range events {
		if _, e := c.applyEvent(tx, carrier, original, false); e != nil {
			return e
		}
	}
	return nil
}
