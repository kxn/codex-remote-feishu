package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

func (a *App) queueTeamNativeMaintenance(ctx context.Context, source teamHumanSource, kind agentproto.CommandKind) (string, error) {
	if kind != agentproto.CommandThreadCompactStart {
		return "", fmt.Errorf("这个入口只处理上下文整理；代码评审请使用 /review 指定范围")
	}
	var maintenance workstore.Maintenance
	var rebuildWorker string
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, _, e := a.teamNativeTarget(tx, source)
		if e != nil {
			return e
		}
		if worker.Backend != work.BackendCodex {
			rebuildWorker = worker.ID
			return nil
		}
		id := teamID("native-maintenance", source.Key)
		maintenance = workstore.Maintenance{ID: id, ScopeID: worker.ScopeID, ContextID: worker.ContextID, WorkerID: worker.ID, WorkerGeneration: worker.Generation, CarrierIncarnation: worker.CarrierIncarnation, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration, NativeSessionID: worker.NativeSessionID, Kind: kind, Source: source.Authority, State: workstore.MaintenanceQueued, CommandID: id + ":command", Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		maintenance, _, e = tx.RequestMaintenance(maintenance)
		return e
	})
	if err != nil {
		return "", err
	}
	if rebuildWorker != "" {
		if a.teamRuntime.host == nil {
			return "", fmt.Errorf("执行宿主尚未连接")
		}
		if _, err = a.teamRuntime.host.RequestCompact(ctx, rebuildWorker, source.Key, source.Authority); err != nil {
			return "", err
		}
		return a.queuePrivatePanel(ctx, source, "status", "已排队整理上下文。这个后端采用安全重建：确认当前执行和后台工作已停止后，新建原生会话，保留长期记忆、工作检查点和旧会话记录；不会把它称作原生压缩。")
	}
	return a.queuePrivatePanel(ctx, source, "status", "已排队压缩当前原生会话。当前轮次和后台工作实际停止后才会发送；完成前，新的输入会保留排队。")
}

// Run this lane independently of ingress. Inspection may wait for a native RPC;
// it must never block SDK persistence, teammate scheduling or event receipts.
func (a *App) runTeamNativeCommands(ctx context.Context) error {
	var pending []workstore.Maintenance
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; pending, e = tx.ListPendingMaintenance(); return e }); err != nil {
		return err
	}
	var failures []error
	if err := a.advanceTeamGoalUI(ctx, time.Now().UTC()); err != nil {
		failures = append(failures, err)
	}
	if err := a.advanceTeamMCPReads(ctx, time.Now().UTC()); err != nil {
		failures = append(failures, err)
	}
	if a.teamRuntime.host != nil {
		if err := a.teamRuntime.host.RunCompactions(ctx, 16); err != nil {
			failures = append(failures, err)
		}
	}
	if err := a.dispatchTeamPendingGoals(ctx); err != nil {
		failures = append(failures, err)
	}
	for _, maintenance := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := a.advanceTeamMaintenance(ctx, maintenance); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (a *App) advanceTeamMaintenance(ctx context.Context, maintenance workstore.Maintenance) error {
	rejected := false
	registered := false
	if maintenance.State != workstore.MaintenanceQueued && maintenance.Terminal == nil {
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			rejected, e = tx.MaintenanceRejectedWithoutWrite(maintenance.ID)
			if e != nil {
				return e
			}
			_, e = tx.GetNativeCommand(maintenance.CommandID)
			registered = e == nil
			if e != nil && e != workstore.ErrNotFound {
				return e
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !rejected {
			return nil
		}
	}
	var request workruntime.InspectionRequest
	ready := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(maintenance.WorkerID)
		if e != nil {
			return e
		}
		if worker.Generation != maintenance.WorkerGeneration || worker.CarrierIncarnation != maintenance.CarrierIncarnation || worker.NativeSessionID != maintenance.NativeSessionID {
			return workstore.ErrConflict
		}
		if (worker.State != work.WorkerReady && !((maintenance.Terminal != nil || rejected) && worker.State == work.WorkerMaintaining)) || worker.Controller != work.ControllerAutomatic {
			return nil
		}
		occupied, e := tx.WorkerHasOccupancy(worker.ID)
		if e != nil {
			return e
		}
		if occupied {
			return nil
		}
		if _, e = tx.GetRuntimeGate(worker.ID); e == nil {
			return nil
		} else if e != workstore.ErrNotFound {
			return e
		}
		carrier, e := tx.GetCarrier(maintenance.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(worker) {
			return workstore.ErrConflict
		}
		request = workruntime.InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: maintenance.NativeSessionID, LocationID: maintenance.LocationID, LocationGeneration: maintenance.LocationGeneration, SubjectCommandID: ""}
		if maintenance.State != workstore.MaintenanceQueued && (!rejected || registered) {
			request.SubjectCommandID = maintenance.CommandID
		}
		ready = true
		return nil
	})
	if err != nil || !ready {
		return err
	}
	observation, err := a.teamRuntime.driver.Inspect(ctx, request)
	if err != nil {
		return err
	}
	if err = observation.ValidateStopped(request); err != nil {
		return err
	}
	proof := workstore.MaintenanceProof{NativeWriteWatermark: observation.NativeWriteWatermark, CanonicalCommandWatermark: observation.CanonicalCommandWatermark, InspectionCommandID: observation.InspectionCommandID, Source: observation.Source, ObservedAt: observation.ObservedAt, NativeStopped: observation.NativeStopped, ContinuationStopped: observation.ContinuationStopped, BackgroundWorkStopped: observation.BackgroundWorkStopped}
	if rejected {
		return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			return tx.FailMaintenance(maintenance.ID, maintenance.Revision, proof, "压缩请求已明确拒绝，未写入原生会话")
		})
	}
	if maintenance.Terminal != nil {
		return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			return tx.CompleteMaintenance(maintenance.ID, maintenance.Revision, proof)
		})
	}
	command := agentproto.Command{CommandID: maintenance.CommandID, IssuedAt: time.Now().UTC(), Kind: maintenance.Kind, Execution: &request.Binding, Target: agentproto.Target{ThreadID: maintenance.NativeSessionID}}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		limits := workstore.AdmissionLimits{}
		if a.teamRuntime.engine != nil {
			limits = a.teamRuntime.engine.AdmissionLimits()
		}
		return tx.PrepareMaintenanceWithLimits(maintenance.ID, maintenance.Revision, proof, command, limits)
	})
	if err != nil {
		return err
	}
	// Prepare freezes the watermark-bound maintenance frame into the canonical
	// request. Send those exact bytes, including the wrapper preflight contract.
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(maintenance.CommandID)
		if e != nil {
			return e
		}
		body, e := tx.GetContent(maintenance.ScopeID, op.RequestRef)
		if e != nil {
			return e
		}
		return json.Unmarshal(body.Data, &command)
	})
	if err != nil {
		return err
	}
	if err = a.teamRuntime.driver.SendCommand(ctx, maintenance.InstanceID, command); err != nil {
		_ = a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			current, e := tx.GetMaintenance(maintenance.ID)
			if e != nil {
				return e
			}
			return tx.MarkMaintenanceUnknown(current.ID, current.Revision, err.Error())
		})
	}
	return err
}
