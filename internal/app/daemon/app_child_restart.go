package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func (a *App) restartRelayChildCodex(instanceID string) error {
	command, err := a.newRelayChildCodexRestartCommand(instanceID)
	if err != nil {
		return err
	}
	return a.sendRelayChildRestartCommand(instanceID, command)
}

func (a *App) newRelayChildCodexRestartCommand(instanceID string) (agentproto.Command, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return agentproto.Command{}, fmt.Errorf("missing instance id for child restart")
	}
	if a.sendAgentCommand == nil {
		return agentproto.Command{}, fmt.Errorf("agent command sender is unavailable")
	}
	command := agentproto.Command{
		CommandID: a.nextCommandID(),
		Kind:      agentproto.CommandProcessChildRestart,
	}
	if a.service != nil {
		for _, inst := range a.service.Instances() {
			if inst == nil || strings.TrimSpace(inst.InstanceID) != instanceID {
				continue
			}
			command.CodexResume = orchestrator.CodexResumePolicyForThread(inst.CodexConnectionContract, inst.CodexThreadPolicy, inst.Threads[strings.TrimSpace(inst.ActiveThreadID)])
			break
		}
	}
	return command, nil
}

func (a *App) sendRelayChildRestartCommand(instanceID string, command agentproto.Command) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("missing instance id for child restart")
	}
	if strings.TrimSpace(command.CommandID) == "" {
		return fmt.Errorf("missing command id for child restart")
	}
	if command.Kind != agentproto.CommandProcessChildRestart {
		return fmt.Errorf("unexpected child restart command kind: %s", command.Kind)
	}
	if a.sendAgentCommand == nil {
		return fmt.Errorf("agent command sender is unavailable")
	}
	return a.sendAgentCommand(instanceID, command)
}
