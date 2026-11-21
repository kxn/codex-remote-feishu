package workruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RecoveryRequest struct {
	ControlRequest
	GateReason           string         `json:"gate_reason"`
	ExpectedGateRevision int64          `json:"expected_gate_revision"`
	Source               work.SourceRef `json:"source"`
}
type RecoveryResult struct {
	OperationID, WorkerID, State   string
	WorkerGeneration, GateRevision int64
}

func (c *Coordinator) Recover(ctx context.Context, request RecoveryRequest) (RecoveryResult, error) {
	var result RecoveryResult
	if err := c.authorizeControl(ctx, request.ControlRequest); err != nil {
		return result, err
	}
	if request.Source.Validate() != nil || request.ExpectedGateRevision <= 0 || request.GateReason == "" {
		return result, fmt.Errorf("recovery requires an exact gate condition and durable human source")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	id := "runtime_recovery_" + hex.EncodeToString(sum[:20])
	var worker work.WorkerSession
	var gate workstore.RuntimeGate
	var inspection InspectionRequest
	var replay bool
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		if previous, e := tx.GetOperation(id); e == nil {
			body, e := tx.GetContent(previous.ScopeID, previous.RequestRef)
			if e != nil {
				return e
			}
			if !bytes.Equal(body.Data, raw) {
				return workstore.ErrIdempotencyConflict
			}
			if previous.State == work.OperationConfirmed {
				replay = true
				result = RecoveryResult{OperationID: id, WorkerID: request.WorkerID, State: "snapshot_rebuild_required"}
				return nil
			}
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		worker, gate, inspection, err = c.recoveryCondition(tx, request)
		if err != nil {
			return err
		}
		_, err = tx.GetContent(worker.ScopeID, request.Source.EvidenceRef)
		return err
	})
	if err != nil || replay {
		return result, err
	}
	if c.options.Driver == nil {
		return result, fmt.Errorf("recovery requires a trusted native inspection driver")
	}
	observation, inspectErr := c.options.Driver.Inspect(ctx, inspection)
	if inspectErr != nil {
		observation, err = VerifyRetiredQuiescence(ctx, c.store, c.options.StateDir, c.options.HostID, worker, gate.AttemptID)
		if err != nil {
			return result, errors.Join(inspectErr, err)
		}
	}
	validation := inspection
	if observation.RetiredCarrierVerified {
		validation.SessionID = observation.SessionID
		validation.TurnID = observation.TurnID
	}
	if err = observation.ValidateStopped(validation); err != nil {
		return result, err
	}
	proofData, err := json.Marshal(observation)
	if err != nil {
		return result, err
	}
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, currentGate, _, e := c.recoveryCondition(tx, request)
		if e != nil {
			return e
		}
		if fresh.Revision != worker.Revision || currentGate.Revision != gate.Revision {
			return workstore.ErrConflict
		}
		if observation.RetiredCarrierVerified {
			if e = tx.AssertNoEffectfulCommandsAfter(worker.CarrierIncarnation, observation.CanonicalCommandWatermark); e != nil {
				return e
			}
		}
		body := work.NewContent(id+":request", worker.ScopeID, "application/json", raw, c.options.Clock())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		proof := work.NewContent(id+":proof", worker.ScopeID, "application/json", proofData, c.options.Clock())
		if e = tx.PutContent(proof); e != nil {
			return e
		}
		operation := work.Operation{ID: id, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: request.Actor, Kind: "worker.recovery", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationConfirmed, ExternalReceipt: proof.ID, Revision: 1, CreatedAt: c.options.Clock(), UpdatedAt: c.options.Clock()}
		if _, _, e = tx.PutOperation(operation, 0); e != nil {
			return e
		}
		if e = c.recoveryInput(tx, id, worker, request.Source, "用户已明确授权恢复当前工作上下文。原收件箱输入保持未处理，先核查此前已经完成的操作和副作用，不要盲目重跑；用明确的处理报告说明实际采用及处理的输入。此前原生历史只读保留，新执行使用新的隔离会话。"); e != nil {
			return e
		}
		if e = tx.AcknowledgeRuntimeRecovery(worker.ID, gate.Reason, gate.Revision); e != nil {
			return e
		}
		if e = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: gate.AttemptID, Reason: "snapshot_rebuild_required", ResponsibleRoleID: worker.RoleID, CreatedAt: c.options.Clock()}); e != nil {
			return e
		}
		expected := fresh.Revision
		fresh.Revision++
		fresh.State = work.WorkerRecovering
		fresh.UpdatedAt = c.options.Clock()
		if e = tx.PutWorker(fresh, expected); e != nil {
			return e
		}
		nextGate, e := tx.GetRuntimeGate(worker.ID)
		if e != nil {
			return e
		}
		result = RecoveryResult{OperationID: id, WorkerID: worker.ID, State: "snapshot_rebuild_required", WorkerGeneration: fresh.Generation, GateRevision: nextGate.Revision}
		return nil
	})
	return result, err
}

func (c *Coordinator) recoveryCondition(tx *workstore.Tx, request RecoveryRequest) (work.WorkerSession, workstore.RuntimeGate, InspectionRequest, error) {
	var gate workstore.RuntimeGate
	var inspection InspectionRequest
	worker, err := tx.GetWorker(request.WorkerID)
	if err != nil {
		return worker, gate, inspection, err
	}
	if worker.ScopeID != request.ScopeID || worker.ControllerGeneration != request.ExpectedControllerGeneration || worker.Controller != work.ControllerAutomatic || (worker.State != work.WorkerRecovering && worker.State != work.WorkerUnavailable && worker.State != work.WorkerReady) {
		return worker, gate, inspection, fmt.Errorf("recovery targets a changed, occupied or human-controlled worker")
	}
	gate, err = tx.GetRuntimeGate(worker.ID)
	if err != nil {
		return worker, gate, inspection, err
	}
	if gate.Reason != request.GateReason || gate.Revision != request.ExpectedGateRevision || gate.AttemptID == "" || (request.AttemptID != "" && request.AttemptID != gate.AttemptID) {
		return worker, gate, inspection, workstore.ErrConflict
	}
	carrier, err := tx.GetCarrier(worker.CarrierIncarnation)
	if err != nil {
		return worker, gate, inspection, err
	}
	if !carrier.MatchesPhysical(worker) {
		return worker, gate, inspection, workstore.ErrConflict
	}
	occupied, err := tx.HasAnyCarrierOccupancy(carrier)
	if err != nil {
		return worker, gate, inspection, err
	}
	if occupied {
		return worker, gate, inspection, fmt.Errorf("native work remains occupied; observe or stop native work before recovery")
	}
	if count, e := tx.CountControllerOccupancy(worker.ID); e != nil {
		return worker, gate, inspection, e
	} else if count > 0 {
		return worker, gate, inspection, fmt.Errorf("human handoff retains resource ownership")
	}
	proof, err := tx.GetQuiescenceReceipt(gate.AttemptID)
	if err != nil {
		return worker, gate, inspection, err
	}
	if err = proof.Validate(); err != nil {
		return worker, gate, inspection, err
	}
	if proof.RunRef.WorkerID != worker.ID || proof.RunRef.Generation != worker.Generation || proof.CarrierIncarnation != carrier.Incarnation || proof.InstanceID != worker.InstanceID || proof.InstanceGeneration != worker.InstanceGeneration || proof.ProcessGeneration != worker.ProcessGeneration {
		return worker, gate, inspection, fmt.Errorf("recovery receipt belongs to an older execution")
	}
	inspection = InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
	return worker, gate, inspection, nil
}
func (c *Coordinator) recoveryInput(tx *workstore.Tx, id string, worker work.WorkerSession, source work.SourceRef, text string) error {
	wc, err := tx.GetContext(worker.ContextID)
	if err != nil {
		return err
	}
	body := work.NewContent(id+":instructions", worker.ScopeID, "text/plain", []byte(text), c.options.Clock())
	if err := tx.PutContent(body); err != nil {
		return err
	}
	message := work.Message{ID: id + ":message", ScopeID: worker.ScopeID, ConversationID: "runtime-recovery:" + worker.ContextID, Source: work.Actor{Kind: work.ActorSystem, ID: "runtime-recovery"}, BodyRef: body.ID, DerivedFrom: &source, Revision: 1, CreatedAt: c.options.Clock()}
	_, _, err = tx.AcceptMessage(message, []work.Inbox{{ID: id + ":inbox", ScopeID: worker.ScopeID, RecipientContextID: worker.ContextID, RecipientRoleID: worker.RoleID, TaskID: wc.TaskID, EventID: id, State: work.InboxPending, AvailableAt: c.options.Clock(), Revision: 1}})
	return err
}
