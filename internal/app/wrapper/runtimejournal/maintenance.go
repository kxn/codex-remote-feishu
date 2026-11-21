package runtimejournal

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (s *Store) ValidateMaintenancePreflight(command agentproto.Command) error {
	contract := command.Maintenance
	if contract == nil {
		return ErrConflict
	}
	var raw []byte
	err := s.db.QueryRow(`SELECT payload FROM events WHERE incarnation=? AND json_extract(payload,'$.kind')=? AND json_extract(payload,'$.commandId')=? ORDER BY sequence DESC LIMIT 1`, s.incarnation, agentproto.EventRuntimeInspectionCompleted, contract.PreflightInspectionCommandID).Scan(&raw)
	if err != nil {
		return err
	}
	var event agentproto.Event
	if err = json.Unmarshal(raw, &event); err != nil {
		return err
	}
	r := event.RuntimeInspection
	if r == nil || r.Problem != nil || r.NativeWriteWatermark != contract.NativeWriteWatermark || event.Execution == nil || !event.Execution.SameOwner(*command.Execution) {
		return ErrConflict
	}
	b := r.Background
	if !b.Complete || !b.NativeTaskSetComplete || !b.NativeInputsComplete || !b.ContinuationInventoryKnown || len(b.ActiveGoalSessionIDs) > 0 || len(b.ActiveNativeTurnIDs) > 0 || b.PendingNativeInputs != 0 || len(b.RunningProcessIDs) > 0 || len(b.PendingTaskIDs) > 0 || len(b.ExternalResourceUncertainties) > 0 {
		return fmt.Errorf("maintenance preflight is not quiescent")
	}
	watermark, err := s.NativeWriteWatermark()
	if err != nil {
		return err
	}
	if watermark != contract.NativeWriteWatermark {
		return fmt.Errorf("native writes changed after maintenance inspection")
	}
	return nil
}
