package wrapper

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type persistedWorkInventory struct {
	ShellItemsStarted    map[string]bool             `json:"shell_items_started,omitempty"`
	ActiveNativeCommands map[string]bool             `json:"active_native_commands"`
	InputRPCMethods      map[string]string           `json:"input_rpc_methods"`
	GoalKnown            map[string]bool             `json:"goal_known"`
	GoalActive           map[string]bool             `json:"goal_active"`
	SessionIDs           map[string]bool             `json:"session_ids"`
	Owner                agentproto.ExecutionBinding `json:"owner"`
	Scopes               []hostinventory.Snapshot    `json:"scopes"`
	Turns                map[string]inspectedTurn    `json:"turns"`
	PendingInputs        map[string]string           `json:"pending_inputs"`
	ActiveTools          map[string]bool             `json:"active_tools"`
	Tasks                map[string]bool             `json:"tasks"`
	Uncertainties        map[string]bool             `json:"uncertainties"`
	TaskInventoryKnown   bool                        `json:"task_inventory_known"`
}

func (s *workInspectionState) persist(journal *runtimejournal.Store, owner agentproto.ExecutionBinding) error {
	if journal == nil {
		return nil
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	state := persistedWorkInventory{ShellItemsStarted: s.shellItemsStarted, ActiveNativeCommands: s.activeNativeCommands, InputRPCMethods: s.inputRPCMethods, GoalKnown: s.goalKnown, GoalActive: s.goalActive, SessionIDs: s.sessionIDs, Owner: owner, Turns: s.turns, PendingInputs: s.pendingInputs, ActiveTools: s.activeTools, Tasks: s.tasks, Uncertainties: s.uncertainties, TaskInventoryKnown: s.taskInventoryKnown}
	for _, scope := range s.scopes {
		state.Scopes = append(state.Scopes, scope.Inspect())
	}
	raw, err := json.Marshal(state)
	if err == nil {
		s.dirty = false
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := journal.SaveInventory(raw); err != nil {
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *workInspectionState) restore(journal *runtimejournal.Store, owner agentproto.ExecutionBinding) error {
	raw, err := journal.LoadInventory()
	if err != nil || len(raw) == 0 {
		return err
	}
	var state persistedWorkInventory
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if !state.Owner.SameOwner(owner) {
		return fmt.Errorf("durable native inventory owner changed without new incarnation")
	}
	if state.ShellItemsStarted != nil {
		s.shellItemsStarted = state.ShellItemsStarted
	}
	if state.ActiveNativeCommands != nil {
		s.activeNativeCommands = state.ActiveNativeCommands
	}
	if state.InputRPCMethods != nil {
		s.inputRPCMethods = state.InputRPCMethods
	}
	if state.GoalKnown != nil {
		s.goalKnown = state.GoalKnown
	}
	if state.GoalActive != nil {
		s.goalActive = state.GoalActive
	}
	if state.SessionIDs != nil {
		s.sessionIDs = state.SessionIDs
	}
	if state.Turns != nil {
		s.turns = state.Turns
	}
	if state.PendingInputs != nil {
		s.pendingInputs = state.PendingInputs
	}
	if state.ActiveTools != nil {
		s.activeTools = state.ActiveTools
	}
	if state.Tasks != nil {
		s.tasks = state.Tasks
	}
	if state.Uncertainties != nil {
		s.uncertainties = state.Uncertainties
	}
	s.taskInventoryKnown = state.TaskInventoryKnown
	for _, snapshot := range state.Scopes {
		scope, err := hostinventory.Restore(snapshot)
		if err != nil {
			s.uncertainties["durable_process_scope_unrecoverable"] = true
			continue
		}
		s.scopes = append(s.scopes, scope)
		if !snapshot.ControlledStop || !snapshot.WaitObserved {
			s.uncertainties["durable_scope_requires_reconciliation:"+snapshot.RootIdentity] = true
		}
	}
	return nil
}
