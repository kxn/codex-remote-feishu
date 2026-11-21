package workstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

func (t *Tx) validateMaintenanceProof(v Maintenance, proof MaintenanceProof) error {
	if proof.Source.Kind != "runtime_inspection" || proof.Source.ID != proof.InspectionCommandID {
		return ErrScopeDenied
	}
	if proof.InspectionCommandID == "" || proof.Source.Validate() != nil || proof.ObservedAt.IsZero() || !proof.NativeStopped || !proof.ContinuationStopped || !proof.BackgroundWorkStopped {
		return fmt.Errorf("complete actual native, continuation and background evidence required")
	}
	op, err := t.GetOperation(proof.InspectionCommandID)
	if err != nil {
		return err
	}
	if op.Kind != string(agentproto.CommandRuntimeInspect) || op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "runtime-inspector"}) || op.ScopeID != v.ScopeID || op.TargetID != v.WorkerID || op.TargetGeneration != v.WorkerGeneration || op.State != work.OperationConfirmed {
		return fmt.Errorf("maintenance proof lacks confirmed current inspection")
	}
	native, err := t.GetNativeCommand(op.ID)
	if err != nil {
		return err
	}
	if native.Incarnation != v.CarrierIncarnation || native.Binding.WorkerSessionID != v.WorkerID || native.Binding.SessionGeneration != v.WorkerGeneration {
		return ErrScopeDenied
	}
	var result agentproto.RuntimeInspectionResult
	if err = json.Unmarshal([]byte(op.ExternalReceipt), &result); err != nil {
		return err
	}
	if result.RequestCommandID != op.ID || result.CarrierIncarnation != v.CarrierIncarnation || result.InstanceGeneration != v.InstanceGeneration || result.ProcessGeneration != v.ProcessGeneration || result.LocationGeneration != v.LocationGeneration || !result.ObservedAt.Equal(proof.ObservedAt) || result.Problem != nil || result.NativeWriteWatermark != proof.NativeWriteWatermark {
		return ErrScopeDenied
	}
	b := result.Background
	if !b.Complete || !b.NativeTaskSetComplete || !b.NativeInputsComplete || !b.ContinuationInventoryKnown || len(b.ActiveGoalSessionIDs) > 0 || len(b.ActiveNativeTurnIDs) > 0 || b.PendingNativeInputs != 0 || len(b.RunningProcessIDs) > 0 || len(b.PendingTaskIDs) > 0 || len(b.ExternalResourceUncertainties) > 0 {
		return fmt.Errorf("maintenance inspection still has work or uncertainty")
	}
	content, err := t.GetContent(v.ScopeID, proof.Source.EvidenceRef)
	if err != nil {
		return err
	}
	var event agentproto.Event
	if err = json.Unmarshal(content.Data, &event); err != nil {
		return err
	}
	var actual agentproto.RuntimeInspectionResult
	if event.RuntimeInspection != nil {
		actual = *event.RuntimeInspection
	} else if err = json.Unmarshal(content.Data, &actual); err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, result) {
		return fmt.Errorf("inspection source does not match confirmed result")
	}
	_, err = t.maintenanceInspectionSequence(v, proof)
	return err
}
func (t *Tx) PrepareMaintenance(id string, expected int64, proof MaintenanceProof, command agentproto.Command) error {
	return t.PrepareMaintenanceWithLimits(id, expected, proof, command, AdmissionLimits{})
}
func (t *Tx) PrepareMaintenanceWithLimits(id string, expected int64, proof MaintenanceProof, command agentproto.Command, limits AdmissionLimits) error {
	return t.atomic(func() error {
		if err := t.CheckAdmissionCapacity(work.PurposeBusiness, limits); err != nil {
			return err
		}
		v, err := t.GetMaintenance(id)
		if err != nil {
			return err
		}
		if v.Revision != expected || v.State != MaintenanceQueued {
			return ErrConflict
		}
		worker, err := t.GetWorker(v.WorkerID)
		if err != nil {
			return err
		}
		if err = maintenanceMatches(v, worker); err != nil {
			return err
		}
		if worker.State != work.WorkerReady || worker.Controller != work.ControllerAutomatic {
			return fmt.Errorf("maintenance waits for ready automatic worker")
		}
		occupied, err := t.WorkerHasOccupancy(worker.ID)
		if err != nil {
			return err
		}
		if occupied {
			return fmt.Errorf("business execution is not quiescent")
		}
		if count, e := t.CountControllerOccupancy(worker.ID); e != nil {
			return e
		} else if count > 0 {
			return fmt.Errorf("maintenance cannot replace human resource lease")
		}
		if _, err = t.GetRuntimeGate(worker.ID); err == nil {
			return fmt.Errorf("worker reconciliation gate is closed")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		watermark, e := t.NativeCommandWatermark(proof.InspectionCommandID)
		if e != nil {
			return e
		}
		if watermark != proof.CanonicalCommandWatermark {
			return ErrConflict
		}
		if e = t.AssertNoEffectfulCommandsAfter(v.CarrierIncarnation, watermark); e != nil {
			return e
		}
		if err = t.validateMaintenanceProof(v, proof); err != nil {
			return err
		}
		if proof.ObservedAt.Before(v.CreatedAt) {
			return fmt.Errorf("maintenance preflight predates request")
		}
		carrier, err := t.GetCarrier(v.CarrierIncarnation)
		if err != nil {
			return err
		}
		if !carrier.Matches(worker) || command.Execution == nil || *command.Execution != carrier.Owner || command.CommandID != v.CommandID || command.Kind != v.Kind || command.Target.ThreadID != v.NativeSessionID || command.Execution.AttemptID != "" || command.Execution.RunRef != "" {
			return ErrScopeDenied
		}
		keys, err := t.maintenanceKeys(v, worker)
		if err != nil {
			return err
		}
		for _, key := range keys {
			var blocked int
			if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM resource_occupancy WHERE resource_key=?", key).Scan(&blocked); err != nil {
				return err
			}
			if blocked > 0 {
				return fmt.Errorf("maintenance location is occupied")
			}
		}
		var carriers int
		if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM carrier_occupancy WHERE instance_id=? AND instance_generation=? AND process_generation=?", v.InstanceID, v.InstanceGeneration, v.ProcessGeneration).Scan(&carriers); err != nil {
			return err
		}
		if carriers > 0 {
			return fmt.Errorf("maintenance carrier is occupied")
		}
		command.Maintenance = &agentproto.NativeMaintenanceCommand{Version: 1, PreflightInspectionCommandID: proof.InspectionCommandID, NativeWriteWatermark: proof.NativeWriteWatermark}
		raw, err := json.Marshal(command)
		if err != nil {
			return err
		}
		content := work.NewContent(command.CommandID+":body", v.ScopeID, "application/json", raw, time.Now().UTC())
		if err = t.PutContent(content); err != nil {
			return err
		}
		op := work.Operation{ID: command.CommandID, ScopeID: v.ScopeID, ContextID: v.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "native-maintenance"}, Kind: "native.maintenance.compact", TargetID: v.WorkerID, TargetGeneration: v.WorkerGeneration, IdempotencyKey: command.CommandID, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		canonical, created, err := t.PutOperation(op, 0)
		if err != nil {
			return err
		}
		if !created || canonical.ID != v.CommandID {
			return ErrIdempotencyConflict
		}
		worker.State = work.WorkerMaintaining
		worker.Revision++
		if err = t.PutWorker(worker, worker.Revision-1); err != nil {
			return err
		}
		v.Preflight = &proof
		v.State = MaintenanceSending
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		return t.updateMaintenance(v, expected)
	})
}
func (t *Tx) MarkMaintenanceUnknown(id string, expected int64, reason string) error {
	v, err := t.GetMaintenance(id)
	if err != nil {
		return err
	}
	if v.Revision != expected || (v.State != MaintenanceSending && v.State != MaintenanceUnknown) {
		return ErrConflict
	}
	v.State = MaintenanceUnknown
	v.LastError = reason
	v.Revision++
	v.UpdatedAt = time.Now().UTC()
	return t.updateMaintenance(v, expected)
}
func (t *Tx) CancelQueuedMaintenance(id string, expected int64, reason string) error {
	return t.atomic(func() error {
		v, err := t.GetMaintenance(id)
		if err != nil {
			return err
		}
		if v.State != MaintenanceQueued || v.Revision != expected {
			return ErrConflict
		}
		v.State = MaintenanceFailed
		v.LastError = reason
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		if err = t.updateMaintenance(v, expected); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "DELETE FROM maintenance_admission_blocks WHERE maintenance_id=?", id)
		return err
	})
}

func (t *Tx) maintenanceInspectionSequence(v Maintenance, proof MaintenanceProof) (uint64, error) {
	var sequence uint64
	err := t.tx.QueryRowContext(t.ctx, "SELECT sequence FROM runtime_receipts WHERE carrier_incarnation=? AND payload_hash=? AND json_extract(payload,'$.commandId')=? AND json_extract(payload,'$.kind')=?", v.CarrierIncarnation, proof.Source.Version, proof.InspectionCommandID, string(agentproto.EventRuntimeInspectionCompleted)).Scan(&sequence)
	return sequence, err
}
