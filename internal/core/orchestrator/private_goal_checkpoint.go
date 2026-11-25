package orchestrator

import (
	"fmt"
	"sort"
	"strings"
)

// PrivateGoalCommand keeps the exact card and native target while the result
// remains pending. It does not claim that the native operation was sent.
type PrivateGoalCommand struct {
	CommandID, SurfaceID, InstanceID, ThreadID, Action, MessageID string
}

type PrivateGoalCheckpoint struct {
	Commands   []PrivateGoalCommand
	Interlocks []GoalInterlockLease
}

func (s *Service) exportPrivateGoalCheckpoint(surfaceID string) *PrivateGoalCheckpoint {
	var out PrivateGoalCheckpoint
	for id, command := range s.goalUserCommands {
		if command.SurfaceID == surfaceID {
			out.Commands = append(out.Commands, PrivateGoalCommand{id, command.SurfaceID, command.InstanceID, command.ThreadID, command.Action, command.MessageID})
		}
	}
	for _, lease := range s.GoalInterlockLeases() {
		if lease.TriggerSurfaceID == surfaceID {
			out.Interlocks = append(out.Interlocks, lease)
		}
	}
	if len(out.Commands) == 0 && len(out.Interlocks) == 0 {
		return nil
	}
	sort.Slice(out.Commands, func(i, j int) bool { return out.Commands[i].CommandID < out.Commands[j].CommandID })
	sort.Slice(out.Interlocks, func(i, j int) bool {
		return goalInterlockKey(out.Interlocks[i].InstanceID, out.Interlocks[i].ThreadID) < goalInterlockKey(out.Interlocks[j].InstanceID, out.Interlocks[j].ThreadID)
	})
	return &out
}

func (s *Service) validatePrivateGoalCheckpoint(surface string, c *PrivateGoalCheckpoint) error {
	if c == nil {
		return nil
	}
	commands := make(map[string]bool)
	for _, v := range c.Commands {
		if v.SurfaceID != surface || strings.TrimSpace(v.CommandID) == "" || strings.TrimSpace(v.InstanceID) == "" || strings.TrimSpace(v.ThreadID) == "" || strings.TrimSpace(v.Action) == "" || commands[v.CommandID] {
			return fmt.Errorf("invalid private goal command")
		}
		commands[v.CommandID] = true
		if _, exists := s.goalUserCommands[v.CommandID]; exists {
			return fmt.Errorf("private goal command already registered")
		}
		if s.goalInterlockByCommand[v.CommandID] != "" || s.goalProbeByCommand[v.CommandID] != "" || s.goalGetByCommand[v.CommandID] != "" {
			return fmt.Errorf("private goal command already registered")
		}
	}
	keys := make(map[string]bool)
	for _, v := range c.Interlocks {
		key := goalInterlockKey(v.InstanceID, v.ThreadID)
		if v.TriggerSurfaceID != surface || strings.TrimSpace(v.InstanceID) == "" || strings.TrimSpace(v.ThreadID) == "" || keys[key] {
			return fmt.Errorf("invalid private goal interlock")
		}
		switch v.Phase {
		case GoalInterlockPausePending, GoalInterlockQuiescing, GoalInterlockDraining, GoalInterlockResumePending, GoalInterlockOwnershipLost, GoalInterlockObservationTimeout, GoalInterlockVerificationRequired, GoalInterlockVerificationGetPending:
		default:
			return fmt.Errorf("invalid private goal phase")
		}
		keys[key] = true
		if _, exists := s.goalInterlocks[key]; exists {
			return fmt.Errorf("private goal interlock already registered")
		}
		for _, id := range []string{v.PauseCommandID, v.ResumeCommandID, v.ProbeCommandID, v.GetCommandID} {
			if id != "" && commands[id] {
				return fmt.Errorf("duplicate private goal command")
			}
			if id != "" {
				commands[id] = true
			}
			if id != "" && (s.goalInterlockByCommand[id] != "" || s.goalProbeByCommand[id] != "" || s.goalGetByCommand[id] != "") {
				return fmt.Errorf("private goal command already registered")
			}
			if _, exists := s.goalUserCommands[id]; id != "" && exists {
				return fmt.Errorf("private goal command already registered")
			}
		}
	}
	return nil
}

// restorePrivateGoalCheckpoint adds only this owner's exact associations and
// never reissues commands. Durable native receipts drive subsequent changes.
func (s *Service) restorePrivateGoalCheckpoint(surface string, c *PrivateGoalCheckpoint) error {
	if err := s.validatePrivateGoalCheckpoint(surface, c); err != nil || c == nil {
		return err
	}
	if s.goalUserCommands == nil {
		s.goalUserCommands = make(map[string]goalUserCommand)
	}
	if s.goalInterlockByCommand == nil {
		s.goalInterlockByCommand = make(map[string]string)
	}
	if s.goalProbeByCommand == nil {
		s.goalProbeByCommand = make(map[string]string)
	}
	if s.goalGetByCommand == nil {
		s.goalGetByCommand = make(map[string]string)
	}
	for _, v := range c.Commands {
		if _, exists := s.goalUserCommands[v.CommandID]; exists {
			return fmt.Errorf("private goal command already registered")
		}
		s.goalUserCommands[v.CommandID] = goalUserCommand{SurfaceID: v.SurfaceID, InstanceID: v.InstanceID, ThreadID: v.ThreadID, Action: v.Action, MessageID: v.MessageID}
	}
	for _, v := range c.Interlocks {
		key := goalInterlockKey(v.InstanceID, v.ThreadID)
		if _, exists := s.goalInterlocks[key]; exists {
			return fmt.Errorf("private goal interlock already registered")
		}
		lease := v
		s.setGoalInterlockLease(&lease)
		for _, id := range []string{lease.PauseCommandID, lease.ResumeCommandID} {
			if id != "" {
				s.goalInterlockByCommand[id] = key
			}
		}
		if lease.ProbeCommandID != "" {
			s.goalProbeByCommand[lease.ProbeCommandID] = key
		}
		if lease.GetCommandID != "" {
			s.goalGetByCommand[lease.GetCommandID] = key
		}
	}
	return nil
}
