package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const childRestartOutcomeTimeout = 30 * time.Second

type childRestartWaiter struct {
	instanceID string
	commandID  string
	ch         chan error
	ack        *agentproto.CommandAck
	outcome    *agentproto.Event
}

func (a *App) restartRelayChildCodexAndWait(ctx context.Context, instanceID string) error {
	command, err := a.newRelayChildCodexRestartCommand(instanceID)
	if err != nil {
		return err
	}

	a.mu.Lock()
	waitCh := a.registerChildRestartWaitLocked(instanceID, command.CommandID)
	a.mu.Unlock()

	if err := a.sendRelayChildRestartCommand(instanceID, command); err != nil {
		a.mu.Lock()
		a.unregisterChildRestartWaitLocked(command.CommandID)
		a.mu.Unlock()
		return err
	}

	select {
	case err := <-waitCh:
		return err
	case <-ctx.Done():
		a.mu.Lock()
		waiter := a.unregisterChildRestartWaitLocked(command.CommandID)
		a.mu.Unlock()
		if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
			return ctx.Err()
		}
		return childRestartWaitTimeoutProblem(command.CommandID, waiter)
	}
}

func (a *App) registerChildRestartWaitLocked(instanceID, commandID string) <-chan error {
	waiter := &childRestartWaiter{
		instanceID: strings.TrimSpace(instanceID),
		commandID:  strings.TrimSpace(commandID),
		ch:         make(chan error, 1),
	}
	a.childRestartWaiters[waiter.commandID] = waiter
	return waiter.ch
}

func (a *App) unregisterChildRestartWaitLocked(commandID string) *childRestartWaiter {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return nil
	}
	waiter := a.childRestartWaiters[commandID]
	delete(a.childRestartWaiters, commandID)
	return waiter
}

func (a *App) completeChildRestartWaitLocked(commandID string, err error) bool {
	waiter := a.unregisterChildRestartWaitLocked(commandID)
	if waiter == nil {
		return false
	}
	waiter.ch <- err
	close(waiter.ch)
	return true
}

func (a *App) noteChildRestartCommandAckLocked(_ context.Context, instanceID string, ack agentproto.CommandAck) bool {
	waiter := a.childRestartWaiters[strings.TrimSpace(ack.CommandID)]
	if waiter == nil || waiter.instanceID != strings.TrimSpace(instanceID) {
		return false
	}
	ackCopy := ack
	waiter.ack = &ackCopy
	return a.maybeResolveChildRestartWaitLocked(waiter)
}

func (a *App) noteChildRestartOutcomeEventLocked(instanceID string, event agentproto.Event) bool {
	waiter := a.childRestartWaiters[strings.TrimSpace(event.CommandID)]
	if waiter == nil || waiter.instanceID != strings.TrimSpace(instanceID) {
		return false
	}
	eventCopy := event
	waiter.outcome = &eventCopy
	return a.maybeResolveChildRestartWaitLocked(waiter)
}

func (a *App) failChildRestartWaitersForInstanceLocked(instanceID string, problem agentproto.ErrorInfo) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	for commandID, waiter := range a.childRestartWaiters {
		if waiter == nil || waiter.instanceID != instanceID {
			continue
		}
		delete(a.childRestartWaiters, commandID)
		waiter.ch <- problem.WithDefaults(agentproto.ErrorInfo{
			Code:      "child_restart_instance_disconnected",
			Layer:     "daemon",
			Stage:     "instance_disconnect",
			Operation: string(agentproto.CommandProcessChildRestart),
			CommandID: commandID,
		})
		close(waiter.ch)
	}
}

func (a *App) maybeResolveChildRestartWaitLocked(waiter *childRestartWaiter) bool {
	if waiter == nil || waiter.ack == nil {
		return false
	}
	if !waiter.ack.Accepted {
		return a.completeChildRestartWaitLocked(waiter.commandID, childRestartAckProblem(*waiter.ack))
	}
	if waiter.outcome == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(waiter.outcome.Status), string(agentproto.ChildRestartStatusSucceeded)) {
		return a.completeChildRestartWaitLocked(waiter.commandID, nil)
	}
	return a.completeChildRestartWaitLocked(waiter.commandID, childRestartOutcomeProblem(*waiter.outcome))
}

func childRestartAckProblem(ack agentproto.CommandAck) error {
	defaults := agentproto.ErrorInfo{
		Code:      "command_rejected",
		Layer:     "wrapper",
		Stage:     "command_ack",
		Operation: string(agentproto.CommandProcessChildRestart),
		Message:   "本地 Codex 拒绝了 child restart。",
		Details:   strings.TrimSpace(ack.Error),
		CommandID: ack.CommandID,
	}
	if ack.Problem == nil {
		return defaults.Normalize()
	}
	value := ack.Problem.WithDefaults(defaults)
	return value
}

func childRestartOutcomeProblem(event agentproto.Event) error {
	defaults := agentproto.ErrorInfo{
		Code:      "child_restart_restore_failed",
		Layer:     "wrapper",
		Stage:     "restart_child_restore_response",
		Operation: string(agentproto.CommandProcessChildRestart),
		Message:   firstNonEmpty(strings.TrimSpace(event.ErrorMessage), "重启后的 Codex 子进程未能恢复先前 thread 上下文。"),
		CommandID: event.CommandID,
		ThreadID:  event.ThreadID,
	}
	if event.Problem == nil {
		return defaults.Normalize()
	}
	value := event.Problem.WithDefaults(defaults)
	return value
}

func childRestartWaitTimeoutProblem(commandID string, waiter *childRestartWaiter) error {
	if waiter != nil && waiter.ack != nil && waiter.ack.Accepted {
		return agentproto.ErrorInfo{
			Code:      "child_restart_restore_timeout",
			Layer:     "daemon",
			Stage:     "restart_child_restore_wait",
			Operation: string(agentproto.CommandProcessChildRestart),
			Message:   "等待重启后的 Codex 子进程恢复 thread 上下文时超时。",
			CommandID: commandID,
		}.Normalize()
	}
	return agentproto.ErrorInfo{
		Code:      "child_restart_ack_timeout",
		Layer:     "daemon",
		Stage:     "restart_child_launch_wait",
		Operation: string(agentproto.CommandProcessChildRestart),
		Message:   "等待本地 Codex 确认 child restart 时超时。",
		CommandID: commandID,
	}.Normalize()
}
