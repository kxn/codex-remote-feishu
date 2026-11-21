package workstore

import (
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// MaintenanceRejectedWithoutWrite is deliberately narrower than transport
// failure. Lost ACKs and any prepared native frame remain uncertain.
func (t *Tx) MaintenanceRejectedWithoutWrite(id string) (bool, error) {
	v, err := t.GetMaintenance(id)
	if err != nil {
		return false, err
	}
	if v.Start != nil || v.Terminal != nil {
		return false, nil
	}
	native, err := t.GetNativeCommand(v.CommandID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if native.Incarnation != v.CarrierIncarnation || native.Binding.WorkerSessionID != v.WorkerID || native.Binding.SessionGeneration != v.WorkerGeneration {
		return false, ErrScopeDenied
	}
	ack := native.Ack
	return ack != nil && !ack.Accepted && ack.RuntimeReceipt != nil && ack.RuntimeReceipt.CarrierIncarnation == v.CarrierIncarnation && ack.RuntimeReceipt.CommandID == v.CommandID && ack.RuntimeReceipt.PreparedFrames == 0 && ack.RuntimeReceipt.WrittenFrames == 0 && len(ack.RuntimeReceipt.Frames) == 0, nil
}

func (t *Tx) FailMaintenance(id string, expected int64, proof MaintenanceProof, reason string) error {
	return t.atomic(func() error {
		v, err := t.GetMaintenance(id)
		if err != nil {
			return err
		}
		if v.State != MaintenanceSending && v.State != MaintenanceUnknown {
			return ErrConflict
		}
		if v.Revision != expected || reason == "" {
			return ErrConflict
		}
		rejected, err := t.MaintenanceRejectedWithoutWrite(id)
		if err != nil {
			return err
		}
		if !rejected {
			return fmt.Errorf("maintenance native effects remain uncertain")
		}
		if v.Preflight == nil || v.Preflight.InspectionCommandID == proof.InspectionCommandID {
			return fmt.Errorf("failed maintenance requires new postflight inspection")
		}
		if err = t.validateMaintenanceProof(v, proof); err != nil {
			return err
		}
		watermark, err := t.NativeCommandWatermark(proof.InspectionCommandID)
		if err != nil {
			return err
		}
		if watermark != proof.CanonicalCommandWatermark {
			return ErrConflict
		}
		if err = t.AssertNoEffectfulCommandsAfter(v.CarrierIncarnation, watermark); err != nil {
			return err
		}
		sequence, err := t.maintenanceInspectionSequence(v, proof)
		if err != nil {
			return err
		}
		before, err := t.maintenanceInspectionSequence(v, *v.Preflight)
		if err != nil {
			return err
		}
		if sequence <= before {
			return ErrConflict
		}
		worker, err := t.GetWorker(v.WorkerID)
		if err != nil {
			return err
		}
		if worker.Generation != v.WorkerGeneration || worker.CarrierIncarnation != v.CarrierIncarnation || worker.NativeSessionID != v.NativeSessionID || worker.State != work.WorkerMaintaining {
			return ErrConflict
		}
		op, err := t.GetOperation(v.CommandID)
		if err != nil {
			return err
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationTerminal
		op.LastError = reason
		op.ExternalReceipt = mustJSON(proof)
		op.UpdatedAt = time.Now().UTC()
		if _, _, err = t.PutOperation(op, old); err != nil {
			return err
		}
		worker.State = work.WorkerReady
		worker.Revision++
		if err = t.PutWorker(worker, worker.Revision-1); err != nil {
			return err
		}
		v.State = MaintenanceFailed
		v.LastError = reason
		v.Completion = &proof
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		if err = t.updateMaintenance(v, expected); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "DELETE FROM maintenance_admission_blocks WHERE maintenance_id=?", id)
		return err
	})
}
