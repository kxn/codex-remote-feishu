package daemon

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type personalSourceStopProof struct {
	WorkerID, RebuildOperationID, RebuildEvidenceID, ExitOperationID, ExitEvidenceID string
	AttemptIDs                                                                       []string
	Isolated                                                                         *workstore.PersonalExecutionIsolation `json:",omitempty"`
}

// collectPersonalSourceStopProof derives proofs from committed operations and
// carrier identities. No externally supplied receipt is accepted here.
func collectPersonalSourceStopProof(tx *workstore.Tx, scopeID string) ([]personalSourceStopProof, error) {
	status, err := tx.PersonalMigrationStatus(scopeID)
	if err != nil {
		return nil, err
	}
	if status.Fence.State != "draining" || status.UnsettledAttempts != 0 || status.UnsettledAdmissions != 0 || status.UnsettledOperations != 0 || status.UnsettledOutbox != 0 || status.UnsettledPrivateCommands != 0 || status.UnsettledMenuDestinations != 0 || status.UnsettledPreviews != 0 || status.UnsettledReactions != 0 || status.OccupiedCarriers != 0 || status.OccupiedResources != 0 || status.OccupiedControllers != 0 {
		return nil, fmt.Errorf("source work or delivery remains unsettled")
	}
	workers, err := tx.ListWorkers(scopeID)
	if err != nil {
		return nil, err
	}
	ops, err := tx.ListOperations(scopeID)
	if err != nil {
		return nil, err
	}
	attempts, err := tx.ListAttempts(scopeID)
	if err != nil {
		return nil, err
	}
	outboxes, err := tx.ListOutbox(scopeID)
	if err != nil {
		return nil, err
	}
	for _, out := range outboxes {
		if out.State == work.OutboxSent {
			op, e := tx.GetOperation(out.OperationID)
			if e != nil {
				return nil, e
			}
			if out.ExternalMessageID == "" || op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
				return nil, fmt.Errorf("sent outbox lacks actual receipt")
			}
		}
	}
	result := make([]personalSourceStopProof, 0, len(workers))
	for _, worker := range workers {
		isolated, e := tx.GetPersonalExecutionIsolation(scopeID, worker.ID)
		if e == nil {
			result = append(result, personalSourceStopProof{WorkerID: worker.ID, Isolated: &isolated})
			continue
		}
		if !errors.Is(e, workstore.ErrNotFound) {
			return nil, e
		}
		if worker.State == work.WorkerPreparing {
			unsent, e := tx.WorkerLaunchUnsent(worker)
			if e != nil {
				return nil, e
			}
			if unsent {
				_, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
				if errors.Is(e, workstore.ErrNotFound) {
					continue
				}
				if e != nil {
					return nil, e
				}
			}
		}
		if worker.Controller != work.ControllerAutomatic || worker.CarrierIncarnation == "" {
			return nil, fmt.Errorf("worker %s has no verifiable automatic carrier", worker.ID)
		}
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return nil, e
		}
		process, e := tx.GetCarrierProcess(carrier.Incarnation)
		if e != nil {
			return nil, e
		}
		var rebuild, exit work.Operation
		var rebuildCount, exitCount int
		for _, op := range ops {
			if op.TargetID != worker.ID || op.TargetGeneration != worker.Generation {
				continue
			}
			switch op.Kind {
			case "worker.rebuild":
				rebuild = op
				rebuildCount++
			case string(agentproto.CommandProcessExit):
				exit = op
				exitCount++
			}
		}
		if rebuildCount != 1 || exitCount != 1 || rebuild.State != work.OperationConfirmed || exit.State != work.OperationConfirmed || rebuild.ExternalReceipt == "" || exit.ExternalReceipt == "" {
			return nil, fmt.Errorf("worker %s lacks verified carrier stop", worker.ID)
		}
		proof, e := tx.GetContent(scopeID, rebuild.ExternalReceipt)
		if e != nil {
			return nil, e
		}
		var observation workruntime.NativeObservation
		if e = json.Unmarshal(proof.Data, &observation); e != nil {
			return nil, e
		}
		request := workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
		if observation.RetiredCarrierVerified {
			request.SessionID = observation.SessionID
			request.TurnID = observation.TurnID
		}
		if e = observation.ValidateStopped(request); e != nil {
			return nil, e
		}
		if observation.ObservedAt.Before(status.Fence.UpdatedAt) {
			return nil, fmt.Errorf("carrier proof predates migration fence")
		}
		exitProof, e := tx.GetContent(scopeID, exit.ExternalReceipt)
		if e != nil {
			return nil, e
		}
		var actual agentproto.RuntimeProcessIdentity
		if e = json.Unmarshal(exitProof.Data, &actual); e != nil {
			return nil, e
		}
		if actual != process {
			return nil, fmt.Errorf("process exit identity mismatch")
		}
		p := personalSourceStopProof{WorkerID: worker.ID, RebuildOperationID: rebuild.ID, RebuildEvidenceID: rebuild.ExternalReceipt, ExitOperationID: exit.ID, ExitEvidenceID: exit.ExternalReceipt}
		for _, attempt := range attempts {
			if attempt.RunRef.WorkerID != worker.ID {
				continue
			}
			stopped, e := tx.PrivateAttemptStopped(attempt)
			if e != nil {
				return nil, e
			}
			if !stopped {
				continue
			}
			receipt, e := tx.GetQuiescenceReceipt(attempt.ID)
			if e != nil {
				return nil, e
			}
			if e = receipt.Validate(); e != nil {
				return nil, e
			}
			if receipt.RunRef != attempt.RunRef {
				return nil, workstore.ErrConflict
			}
			p.AttemptIDs = append(p.AttemptIDs, attempt.ID)
		}
		result = append(result, p)
	}
	return result, nil
}
