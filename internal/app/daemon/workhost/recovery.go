package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

func (h *Host) checkDesiredLocation(ctx context.Context, worker work.WorkerSession) error {
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		desired, err := tx.GetDesiredExecutionLocation(worker.ContextID)
		if errors.Is(err, workstore.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if desired.ScopeID != worker.ScopeID {
			return workstore.ErrScopeDenied
		}
		if desired.LocationID != worker.LocationID || desired.LocationGeneration != worker.LocationGeneration {
			return fmt.Errorf("%w: verified execution location changed", workengine.ErrPreparationRequired)
		}
		return nil
	})
}

type existingPreparation struct {
	replace               bool
	preserveNativeSession bool
	unsentLaunch          *work.Operation
}

func (h *Host) prepareExisting(ctx context.Context, previous *work.WorkerSession, role work.Role, binding work.RoleBotBinding, settings LaunchSettings, bundle agentproto.InstructionBundle) (existingPreparation, error) {
	var record LaunchRecord
	var operation work.Operation
	var rebuilt work.Operation
	var carrier workstore.CarrierRegistration
	var personal bool
	var rejectedResume bool
	var observedModel *agentproto.CodexEffectiveThreadContract
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(previous.ScopeID)
		if err != nil {
			return err
		}
		personal = scope.Kind == work.ScopePersonal
		if gate, e := tx.GetRuntimeGate(previous.ID); e == nil && gate.Reason == "snapshot_rebuild_required" && gate.AttemptID != "" {
			attempt, e := tx.GetAttempt(gate.AttemptID)
			if e != nil {
				return e
			}
			command, e := tx.GetNativeCommand(attempt.CommandID)
			if e != nil {
				return e
			}
			rejectedResume, e = tx.IsNativeResumeRejection(command)
			if e != nil {
				return e
			}
		} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}

		operation, err = tx.GetOperation(launchID(*previous))
		if err != nil {
			return err
		}
		rebuilt, err = tx.GetOperation(rebuildID(*previous))
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		// Reading the original record intentionally permits a newer controller
		// generation after a verified return: its immutable old owner is inspected.
		body, err := tx.GetContent(previous.ScopeID, operation.RequestRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &record); err != nil {
			return err
		}
		if record.WorkerID != previous.ID || record.Generation != previous.Generation || record.Snapshot.CarrierIncarnation != previous.CarrierIncarnation {
			return fmt.Errorf("launch recovery ownership mismatch")
		}
		if personal && previous.Backend == work.BackendCodex && previous.NativeSessionID != "" &&
			privateCodexSelectedModel(record.Overrides, record.CodexResume) != privateCodexSelectedModel(settings.Overrides, settings.CodexResume) {
			observedModel, err = tx.PrivateCodexObservedModel(previous.CarrierIncarnation, previous.NativeSessionID)
			if err != nil {
				return err
			}
		}
		carrier, err = tx.GetCarrier(previous.CarrierIncarnation)
		if errors.Is(err, workstore.ErrNotFound) && operation.State == work.OperationPrepared {
			return nil
		}
		return err
	})
	if err != nil {
		return existingPreparation{}, err
	}
	same := record.reusableFor(settings, *previous, role, binding, bundle, h.config.HostID, personal)
	privateCompatible := personal && privateHistoryCompatible(record, settings, *previous, role, binding, h.config.HostID, observedModel)
	compatibleSnapshot := !rejectedResume && (same || privateCompatible)
	if rebuilt.State == work.OperationConfirmed && rebuilt.NextAttemptAt != nil {
		if h.config.Now().Before(*rebuilt.NextAttemptAt) {
			return existingPreparation{}, nil
		}
		return existingPreparation{replace: true, preserveNativeSession: compatibleSnapshot && previous.NativeSessionID != "" && ctx.Value(freshHistoryKey{}) == nil}, nil
	}
	if operation.State == work.OperationSending || operation.State == work.OperationUnknown {
		if err = h.confirmObservedLaunch(ctx, *previous, &operation); err != nil {
			return existingPreparation{}, err
		}
	}
	switch operation.State {
	case work.OperationPrepared:
		if same {
			carrier = workstore.CarrierRegistration{Incarnation: previous.CarrierIncarnation, OriginInstanceID: previous.InstanceID, Owner: record.Snapshot.Owner, Backend: previous.Backend, InstanceGeneration: previous.InstanceGeneration, ProcessGeneration: previous.ProcessGeneration, LocationGeneration: previous.LocationGeneration}
			if err = h.config.Registry.RegisterCarrier(ctx, carrier); err != nil {
				return existingPreparation{}, err
			}
			token, err := h.issueToken(record.Snapshot.Owner)
			if err != nil {
				return existingPreparation{}, err
			}
			return existingPreparation{}, h.startPrepared(ctx, *previous, operation, record, settings, token)
		}
		// Supersede the provably unsent launch only in the transaction that
		// replaces its reservation. A failed replacement leaves it prepared.
		return existingPreparation{replace: true, unsentLaunch: &operation}, nil
	case work.OperationConfirmed:
		if previous.State == work.WorkerRecovering {
			if err = h.store.Within(ctx, func(tx *workstore.Tx) error {
				gate, e := tx.GetRuntimeGate(previous.ID)
				if e == nil && gate.Reason == "snapshot_rebuild_required" {
					same = false
				}
				if e != nil && !errors.Is(e, workstore.ErrNotFound) {
					return e
				}
				return nil
			}); err != nil {
				return existingPreparation{}, err
			}
		}
		if same {
			return existingPreparation{}, nil
		}
	default:
		return existingPreparation{}, fmt.Errorf("worker launch state cannot be recovered: %s", operation.State)
	}
	if h.config.Runtime == nil || h.config.StopCarrier == nil {
		return existingPreparation{}, fmt.Errorf("verified safe rebuild requires native inspector and exact carrier stop")
	}
	if err = h.rebuildCarrier(ctx, previous, carrier, compatibleSnapshot && previous.NativeSessionID != "" && ctx.Value(freshHistoryKey{}) == nil); err != nil {
		return existingPreparation{}, err
	}
	var preserve bool
	if compatibleSnapshot && previous.NativeSessionID != "" && ctx.Value(freshHistoryKey{}) == nil {
		err = h.store.Within(ctx, func(tx *workstore.Tx) error {
			rebuild, e := tx.GetOperation(rebuildID(*previous))
			if e != nil {
				return e
			}
			body, e := tx.GetContent(previous.ScopeID, rebuild.RequestRef)
			if e != nil {
				return e
			}
			var request carrierRebuildRequest
			if e = json.Unmarshal(body.Data, &request); e != nil {
				return e
			}
			preserve = rebuild.State == work.OperationConfirmed && request.ReuseNativeSession
			return nil
		})
	}
	return existingPreparation{replace: err == nil, preserveNativeSession: preserve}, err
}

func (h *Host) supersedeUnsentLaunch(tx *workstore.Tx, launch work.Operation) error {
	current, err := tx.GetOperation(launch.ID)
	if err != nil {
		return err
	}
	if current.Revision != launch.Revision || current.State != work.OperationPrepared {
		return workstore.ErrConflict
	}
	current.State = work.OperationTerminal
	current.Revision++
	current.LastError = "superseded before launch"
	current.UpdatedAt = h.config.Now()
	_, _, err = tx.PutOperation(current, launch.Revision)
	return err
}

func (h *Host) issueToken(owner agentproto.ExecutionBinding) (string, error) {
	return h.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: owner.RoleID, BotGeneration: owner.BotGeneration, ScopeID: owner.ScopeID, ContextID: owner.ContextID, WorkerID: owner.WorkerSessionID, SessionGeneration: owner.SessionGeneration, ControllerGeneration: owner.ControllerGeneration}, 30*24*time.Hour)
}

type freshHistoryKey struct{}

type carrierRebuildRequest struct {
	workstore.CarrierRegistration
	ReuseNativeSession bool `json:"reuse_native_session,omitempty"`
}

func rebuildID(worker work.WorkerSession) string {
	return idFor("rebuild", worker.ID, fmt.Sprint(worker.Generation))
}

func (h *Host) rebuildCarrier(ctx context.Context, worker *work.WorkerSession, carrier workstore.CarrierRegistration, reuseNativeSession bool) error {
	id := rebuildID(*worker)
	var operation work.Operation
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetWorker(worker.ID)
		if err != nil {
			return err
		}
		if current.Revision != worker.Revision || current.Controller != work.ControllerAutomatic || (current.State != work.WorkerReady && current.State != work.WorkerRecovering) {
			return fmt.Errorf("busy, human-controlled or changed worker cannot rebuild")
		}
		occupied, err := tx.HasAnyCarrierOccupancy(carrier)
		if err != nil {
			return err
		}
		if occupied {
			return fmt.Errorf("worker retains unreconciled attempt occupancy")
		}
		count, err := tx.CountControllerOccupancy(worker.ID)
		if err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("worker retains controller location occupancy")
		}
		gate, err := tx.GetRuntimeGate(worker.ID)
		if err == nil && (gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "") {
			return fmt.Errorf("worker recovery gate requires explicit resolution: %s", gate.Reason)
		}
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if current.State == work.WorkerReady {
			before := current.Revision
			current.State = work.WorkerRecovering
			current.Revision++
			current.UpdatedAt = h.config.Now()
			if err = tx.PutWorker(current, before); err != nil {
				return err
			}
			*worker = current
		}
		operation, err = tx.GetOperation(id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if err = tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: gate.AttemptID, Reason: "snapshot_rebuild_required", CreatedAt: h.config.Now()}); err != nil {
			return err
		}
		requestData, e := json.Marshal(carrierRebuildRequest{CarrierRegistration: carrier, ReuseNativeSession: reuseNativeSession})
		if e != nil {
			return e
		}
		requestBody := work.NewContent(id+":body", worker.ScopeID, "application/json", requestData, h.config.Now())
		if e = tx.PutContent(requestBody); e != nil {
			return e
		}
		operation = work.Operation{RequestRef: requestBody.ID, RequestHash: requestBody.SHA256, ID: id, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.rebuild", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: id, State: work.OperationPrepared, Revision: 1, CreatedAt: h.config.Now(), UpdatedAt: h.config.Now()}
		_, _, err = tx.PutOperation(operation, 0)
		return err
	})
	if err != nil {
		return err
	}
	if operation.State == work.OperationConfirmed {
		return nil
	}
	if operation.State != work.OperationPrepared {
		return h.resumeCarrierStop(ctx, *worker, operation, carrier)
	}
	request := workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
	observed, err := h.inspectRebuildingCarrier(ctx, *worker, request)
	if err != nil {
		return err
	}

	if observed.RetiredCarrierVerified {
		request.SessionID = observed.SessionID
		request.TurnID = observed.TurnID
	}
	if err = observed.ValidateStopped(request); err != nil {
		return err
	}
	data, err := json.Marshal(observed)
	if err != nil {
		return err
	}
	content := work.NewContent(idFor("rebuild_proof", id), worker.ScopeID, "application/json", data, h.config.Now())
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, e := tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		if fresh.Revision != worker.Revision {
			return workstore.ErrConflict
		}
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.Revision != operation.Revision {
			return workstore.ErrConflict
		}
		if e = tx.PutContent(content); e != nil {
			return e
		}
		op.ExternalReceipt = content.ID
		op.State = work.OperationSending
		op.Revision++
		op.UpdatedAt = h.config.Now()
		_, _, e = tx.PutOperation(op, operation.Revision)
		operation = op
		return e
	})
	if err != nil {
		return err
	}
	stopErr := h.config.StopCarrier(ctx, *worker)
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		if op.Revision != operation.Revision {
			return workstore.ErrConflict
		}
		op.Revision++
		op.UpdatedAt = h.config.Now()
		if stopErr != nil {
			op.State = work.OperationUnknown
			op.LastError = stopErr.Error()
		} else {
			op.State = work.OperationConfirmed
			op.LastError = ""
		}
		_, _, e = tx.PutOperation(op, operation.Revision)
		return e
	})
	if err != nil {
		return err
	}
	return stopErr
}

func (h *Host) resumeCarrierStop(ctx context.Context, worker work.WorkerSession, operation work.Operation, carrier workstore.CarrierRegistration) error {
	if operation.State != work.OperationSending && operation.State != work.OperationUnknown {
		return fmt.Errorf("carrier shutdown state requires reconciliation")
	}
	var observed workruntime.NativeObservation
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		body, e := tx.GetContent(worker.ScopeID, operation.ExternalReceipt)
		if e != nil {
			return e
		}
		return json.Unmarshal(body.Data, &observed)
	})
	request := workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
	if err != nil {
		return err
	}
	if observed.RetiredCarrierVerified {
		request.SessionID = observed.SessionID
		request.TurnID = observed.TurnID
	}
	if err = observed.ValidateStopped(request); err != nil {
		return err
	}
	// StopCarrier must itself persist a unique exit operation and never resend an
	// uncertain shutdown. Its retry only observes the exact durable OS identity.
	if err = h.config.StopCarrier(ctx, worker); err != nil {
		return err
	}
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		if op.Revision != operation.Revision {
			return workstore.ErrConflict
		}
		op.State = work.OperationConfirmed
		op.Revision++
		op.UpdatedAt = h.config.Now()
		op.LastError = ""
		_, _, e = tx.PutOperation(op, operation.Revision)
		return e
	})
}
