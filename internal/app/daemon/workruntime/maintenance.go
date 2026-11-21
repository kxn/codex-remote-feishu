package workruntime

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func validateMaintenanceCommand(tx *workstore.Tx, carrier workstore.CarrierRegistration, worker work.WorkerSession, command agentproto.Command) error {
	if command.Kind != agentproto.CommandThreadCompactStart || !command.IsOwnerOnlyRuntimeCommand() {
		switch command.Kind {
		case agentproto.CommandRuntimeInspect, agentproto.CommandModelList, agentproto.CommandProcessExit, agentproto.CommandTurnInterrupt, agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalClear:
			return nil
		}
		return tx.AssertMaintenanceResourceAvailable("worker:" + worker.ID)
	}
	m, err := tx.GetMaintenanceByCommand(command.CommandID)
	if err != nil {
		return err
	}
	if carrier.Backend != work.BackendCodex || worker.State != work.WorkerMaintaining || m.State != workstore.MaintenanceSending || m.Preflight == nil || m.WorkerID != worker.ID || m.WorkerGeneration != worker.Generation || m.ContextID != worker.ContextID || m.ScopeID != worker.ScopeID || m.CarrierIncarnation != carrier.Incarnation || m.InstanceID != carrier.OriginInstanceID || m.InstanceGeneration != carrier.InstanceGeneration || m.ProcessGeneration != carrier.ProcessGeneration || m.LocationID != worker.LocationID || m.LocationGeneration != worker.LocationGeneration || m.NativeSessionID != command.Target.ThreadID || !carrier.Owner.SameOwner(*command.Execution) || command.Maintenance.PreflightInspectionCommandID != m.Preflight.InspectionCommandID || command.Maintenance.NativeWriteWatermark != m.Preflight.NativeWriteWatermark {
		return fmt.Errorf("compact requires the exact held native maintenance barrier")
	}
	return nil
}

func (c *Coordinator) applyMaintenanceEvent(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) error {
	native, err := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
	if err != nil {
		return err
	}
	if native.Kind != agentproto.CommandThreadCompactStart || event.Execution == nil || native.Binding != *event.Execution || !carrier.Owner.SameOwner(*event.Execution) || event.Execution.AttemptID != "" || event.Execution.RunRef != "" || event.RequestID == "" {
		return fmt.Errorf("maintenance receipt has no exact owner-only command")
	}
	if event.Kind == agentproto.EventNativeMaintenanceStarted && event.Status != "accepted" {
		return fmt.Errorf("maintenance RPC did not succeed")
	}
	if event.Kind == agentproto.EventNativeMaintenanceTerminal && (event.Status != "completed" || event.ItemKind != "context_compaction" || event.ItemID == "" || event.TurnID == "") {
		return fmt.Errorf("maintenance lacks a concrete completed native compaction item")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("%s:maintenance:%d", event.CommandID, event.Journal.Sequence)
	content := work.NewContent(id, native.Binding.ScopeID, "application/json", raw, c.options.Clock())
	if err = tx.PutContent(content); err != nil {
		return err
	}
	source := work.SourceRef{Kind: string(event.Kind), ID: event.CommandID, Version: event.Journal.PayloadHash, EvidenceRef: id}
	if event.Kind == agentproto.EventNativeMaintenanceStarted {
		return tx.RecordNativeMaintenanceStart(event.CommandID, source)
	}
	return tx.RecordNativeMaintenanceTerminal(event.CommandID, source)
}
