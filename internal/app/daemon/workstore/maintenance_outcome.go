package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"time"
)

func (t *Tx) recordNativeMaintenanceEvidence(commandID string, source work.SourceRef, terminal bool) error {
	return t.atomic(func() error {
		v, err := t.GetMaintenanceByCommand(commandID)
		if err != nil {
			return err
		}
		if v.State != MaintenanceSending && v.State != MaintenanceUnknown {
			return ErrConflict
		}
		if source.Validate() != nil {
			return fmt.Errorf("native maintenance needs durable exact evidence")
		}
		body, err := t.GetContent(v.ScopeID, source.EvidenceRef)
		if err != nil {
			return err
		}
		var event agentproto.Event
		if err = json.Unmarshal(body.Data, &event); err != nil {
			return err
		}
		expectedKind := agentproto.EventNativeMaintenanceStarted
		if terminal {
			expectedKind = agentproto.EventNativeMaintenanceTerminal
			if event.Status != "completed" || event.ItemKind != "context_compaction" || event.ItemID == "" || event.TurnID == "" {
				return fmt.Errorf("maintenance terminal requires exact completed compaction item")
			}
		} else if event.Status != "accepted" {
			return fmt.Errorf("maintenance start requires successful native compact RPC")
		}
		if event.Kind != expectedKind || event.RequestID == "" || source.Kind != string(expectedKind) || source.ID != commandID || event.Journal == nil || source.Version != event.Journal.PayloadHash {
			return ErrScopeDenied
		}
		if event.CommandID != v.CommandID || event.ThreadID != v.NativeSessionID || event.Execution == nil || event.Execution.WorkerSessionID != v.WorkerID || event.Execution.SessionGeneration != v.WorkerGeneration || event.Execution.AttemptID != "" || event.Execution.RunRef != "" || event.Journal == nil || event.Journal.CarrierIncarnation != v.CarrierIncarnation {
			return ErrScopeDenied
		}
		carrier, err := t.GetCarrier(v.CarrierIncarnation)
		if err != nil {
			return err
		}
		if *event.Execution != carrier.Owner {
			return ErrScopeDenied
		}
		persisted, err := t.GetRuntimeEvent(event.Journal.CarrierIncarnation, event.Journal.Sequence)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(persisted, event) {
			return fmt.Errorf("maintenance evidence was not committed with this receipt")
		}
		previous := v.Revision
		if terminal {
			if v.Start == nil {
				return fmt.Errorf("terminal requires explicit RPC maintenance authorization")
			}
			if v.Terminal != nil {
				if !reflect.DeepEqual(*v.Terminal, source) {
					return ErrIdempotencyConflict
				}
				return nil
			}
			v.Terminal = &source
		} else {
			if v.Start != nil {
				if !reflect.DeepEqual(*v.Start, source) {
					return ErrIdempotencyConflict
				}
				return nil
			}
			v.Start = &source
		}
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		return t.updateMaintenance(v, previous)
	})
}
func (t *Tx) RecordNativeMaintenanceStart(commandID string, source work.SourceRef) error {
	return t.recordNativeMaintenanceEvidence(commandID, source, false)
}
func (t *Tx) RecordNativeMaintenanceTerminal(commandID string, source work.SourceRef) error {
	return t.recordNativeMaintenanceEvidence(commandID, source, true)
}
func (t *Tx) CompleteMaintenance(id string, expected int64, proof MaintenanceProof) error {
	return t.atomic(func() error {
		v, err := t.GetMaintenance(id)
		if err != nil {
			return err
		}
		if v.State == MaintenanceCompleted {
			if v.Completion != nil && reflect.DeepEqual(*v.Completion, proof) {
				return nil
			}
			return ErrIdempotencyConflict
		}
		if v.Revision != expected || (v.State != MaintenanceSending && v.State != MaintenanceUnknown) || v.Start == nil || v.Terminal == nil {
			return ErrConflict
		}
		worker, err := t.GetWorker(v.WorkerID)
		if err != nil {
			return err
		}
		if err = maintenanceMatches(v, worker); err != nil {
			return err
		}
		if err = t.validateMaintenanceProof(v, proof); err != nil {
			return err
		}
		if v.Preflight == nil || proof.InspectionCommandID == v.Preflight.InspectionCommandID {
			return fmt.Errorf("maintenance completion requires a new inspection")
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
		sequence, err := t.maintenanceInspectionSequence(v, proof)
		if err != nil {
			return err
		}
		terminalContent, err := t.GetContent(v.ScopeID, v.Terminal.EvidenceRef)
		if err != nil {
			return err
		}
		var terminalEvent agentproto.Event
		if err = json.Unmarshal(terminalContent.Data, &terminalEvent); err != nil {
			return err
		}
		if terminalEvent.Journal == nil || sequence <= terminalEvent.Journal.Sequence {
			return fmt.Errorf("maintenance completion inspection must follow the actual terminal in the same native journal")
		}

		op, err := t.GetOperation(v.CommandID)
		if err != nil {
			return err
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = mustJSON(proof)
		op.UpdatedAt = time.Now().UTC()
		if _, _, err = t.PutOperation(op, old); err != nil {
			return err
		}
		if worker.State != work.WorkerMaintaining {
			return fmt.Errorf("maintenance no longer owns worker state")
		}
		worker.State = work.WorkerReady
		worker.Revision++
		if err = t.PutWorker(worker, worker.Revision-1); err != nil {
			return err
		}
		v.State = MaintenanceCompleted
		v.Completion = &proof
		v.Revision++
		v.UpdatedAt = time.Now().UTC()
		v.LastError = ""
		if err = t.updateMaintenance(v, expected); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "DELETE FROM maintenance_admission_blocks WHERE maintenance_id=?", v.ID)
		return err
	})
}
