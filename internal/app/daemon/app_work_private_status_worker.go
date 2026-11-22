package daemon

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func appendTeamPrivateWorkerStatus(tx *workstore.Tx, wc work.WorkContext, result *teamPrivateStatus, location *work.ExecutionLocation) error {
	worker, err := tx.CurrentContextWorker(wc.ID)
	if errors.Is(err, workstore.ErrNotFound) {
		result.Lines = append(result.Lines, "执行状态：等待消息；需要执行时自动准备")
		return nil
	}
	if err != nil {
		return err
	}
	label, err := teamWorkerStatusLabel(tx, worker)
	if err != nil {
		return err
	}
	result.Lines = append(result.Lines, "执行状态："+label, "当前执行后端："+string(worker.Backend))
	if worker.LocationID != "" {
		actual, err := tx.GetLocation(worker.LocationID)
		if err != nil {
			return err
		}
		if actual.Generation != worker.LocationGeneration {
			return workstore.ErrConflict
		}
		if actual.Cwd != location.Cwd {
			result.Lines = append(result.Lines, "当前执行目录："+actual.Cwd)
		}
		*location = actual
	}
	if worker.CarrierIncarnation != "" {
		connected, err := tx.CarrierConnected(worker.CarrierIncarnation)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if connected {
			result.Lines = append(result.Lines, "执行器连接：在线")
			process, err := tx.GetCarrierProcess(worker.CarrierIncarnation)
			if err == nil {
				result.Lines = append(result.Lines, fmt.Sprintf("进程 PID：%d", process.PID))
			} else if !errors.Is(err, workstore.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		snapshot, err := tx.GetNativeLaunchSnapshot(worker.ID, worker.Generation)
		if err == nil && snapshot.NativeBinaryVersion != "" {
			result.Lines = append(result.Lines, "执行器版本："+snapshot.NativeBinaryVersion)
		} else if err != nil && !errors.Is(err, workstore.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if gate, err := tx.GetRuntimeGate(worker.ID); err == nil {
		if gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "" {
			result.Lines = append(result.Lines, "需要处理："+teamRuntimeGateDescription(gate))
		}
	} else if !errors.Is(err, workstore.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if attempt, err := tx.GetOccupiedAttemptForWorker(worker.ID); err == nil {
		command, err := tx.NativeCommandPayload(attempt.CommandID, worker.CarrierIncarnation)
		if err == nil {
			var values []string
			for _, field := range []struct{ label, value string }{{"模型", command.Overrides.Model}, {"推理强度", command.Overrides.ReasoningEffort}, {"权限", command.Overrides.AccessMode}, {"计划模式", command.Overrides.PlanMode}} {
				if field.value != "" {
					values = append(values, field.label+"="+field.value)
				}
			}
			if len(values) > 0 {
				result.Lines = append(result.Lines, "本轮已发送参数："+strings.Join(values, "；"))
			}
		} else if !errors.Is(err, workstore.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	} else if !errors.Is(err, workstore.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return appendTeamPrivateObservedSettings(tx, worker, result)
}
