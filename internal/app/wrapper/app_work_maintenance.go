package wrapper

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// The compact RPC has an empty result. Association with its subsequent item is
// an exclusive maintenance authorization, not a claimed native causal ID.
type workMaintenance struct {
	command       agentproto.Command
	requestID     string
	accepted      bool
	turnCompleted bool
	terminal      *agentproto.Event
}

func (w *workExecutionState) prepareMaintenance(command agentproto.Command, backend agentproto.Backend) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if command.Kind == agentproto.CommandThreadCompactStart {
		if !command.IsOwnerOnlyRuntimeCommand() || backend != agentproto.BackendCodex || w.journal == nil || command.Target.ThreadID == "" {
			return fmt.Errorf("managed compact requires a journaled Codex maintenance barrier")
		}
		if w.maintenance != nil && w.maintenance.command.CommandID == command.CommandID {
			return nil
		}
		if w.maintenance != nil && (!w.maintenance.accepted || w.maintenance.terminal == nil || !w.maintenance.turnCompleted) {
			return fmt.Errorf("previous native maintenance is unresolved")
		}
		if err := w.journal.ValidateMaintenancePreflight(command); err != nil {
			return err
		}
		w.maintenance = &workMaintenance{command: command}
		return nil
	}
	if w.maintenance != nil && (!w.maintenance.accepted || w.maintenance.terminal == nil || !w.maintenance.turnCompleted) {
		switch command.Kind {
		case agentproto.CommandRuntimeInspect, agentproto.CommandModelList, agentproto.CommandProcessExit, agentproto.CommandTurnInterrupt, agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalClear:
			return nil
		}
		return fmt.Errorf("native maintenance owns this carrier until verified completion")
	}
	if w.maintenance != nil && w.maintenance.accepted && w.maintenance.terminal != nil && w.maintenance.turnCompleted {
		w.maintenance = nil
	}
	return nil
}

func (w *workExecutionState) maintenanceEvents(events []agentproto.Event) []agentproto.Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	m := w.maintenance
	if m == nil {
		return events
	}
	output := make([]agentproto.Event, 0, len(events)+1)
	for _, event := range events {
		if event.ThreadID != m.command.Target.ThreadID {
			output = append(output, event)
			continue
		}
		if event.Kind == agentproto.EventNativeMaintenanceStarted && event.CommandID == m.command.CommandID && event.Status == "accepted" {
			m.accepted = true
			m.requestID = event.RequestID
			output = append(output, event)
			if m.terminal != nil {
				terminal := *m.terminal
				terminal.RequestID = m.requestID
				output = append(output, terminal)
			}
			continue
		}
		if event.Kind == agentproto.EventItemCompleted && event.ItemKind == "context_compaction" && event.Status == "completed" && event.ItemID != "" && event.TurnID != "" {
			event.Kind = agentproto.EventNativeMaintenanceTerminal
			event.CommandID = m.command.CommandID
			event.RequestID = m.requestID
			event.Execution = agentproto.CloneExecutionBinding(m.command.Execution)
			event.Metadata = map[string]any{"association": "exclusive_maintenance_authorization", "preflightInspectionCommandId": m.command.Maintenance.PreflightInspectionCommandID}
			if m.terminal == nil {
				m.terminal = &event
				if m.accepted {
					output = append(output, event)
				}
			}
			continue
		}
		if event.Kind == agentproto.EventTurnCompleted && m.terminal != nil && event.TurnID == m.terminal.TurnID {
			m.turnCompleted = true
		}
		// Inventory has already observed these real lifecycles. They are not
		// business attempts and cannot release an unrelated attempt or inbox.
		if event.Kind == agentproto.EventTurnStarted || event.Kind == agentproto.EventTurnCompleted || event.Kind == agentproto.EventNativeTurnBindingObserved {
			continue
		}
		output = append(output, event)
	}
	return output
}
