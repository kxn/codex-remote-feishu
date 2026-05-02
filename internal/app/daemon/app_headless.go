package daemon

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	headlessruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/headlessruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func (a *App) handleDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return nil
	}
	return a.handleDaemonCommandLocked(command)
}

func (a *App) handleDaemonCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	switch command.Kind {
	case control.DaemonCommandStartHeadless:
		return a.startManagedHeadless(command)
	case control.DaemonCommandKillHeadless:
		return a.killManagedHeadless(command)
	case control.DaemonCommandDebug:
		return a.handleDebugDaemonCommand(command)
	case control.DaemonCommandCron:
		return a.handleCronDaemonCommandLocked(command)
	case control.DaemonCommandRepair:
		return a.handleRepairDaemonCommand(command)
	case control.DaemonCommandRestart:
		return a.handleRestartDaemonCommand(command)
	case control.DaemonCommandUpgrade:
		return a.handleUpgradeDaemonCommand(command)
	case control.DaemonCommandUpgradeOwnerFlow:
		return a.handleUpgradeOwnerFlowCommandLocked(command)
	case control.DaemonCommandVSCodeMigrateCommand:
		return a.handleVSCodeMigrateCommandPage(command)
	case control.DaemonCommandVSCodeMigrate:
		return a.handleVSCodeMigrateCommand(command)
	case control.DaemonCommandThreadHistoryRead:
		return a.handleThreadHistoryDaemonCommandLocked(command)
	case control.DaemonCommandSendIMFile:
		return a.handleSendIMFileCommandLocked(command)
	case control.DaemonCommandGitWorkspaceImport:
		return a.handleGitWorkspaceImportCommandLocked(command)
	case control.DaemonCommandGitWorkspaceImportCancel:
		return a.handleGitWorkspaceImportCancelCommandLocked(command)
	case control.DaemonCommandGitWorkspaceWorktreeCreate:
		return a.handleGitWorkspaceWorktreeCreateCommandLocked(command)
	case control.DaemonCommandGitWorkspaceWorktreeCancel:
		return a.handleGitWorkspaceWorktreeCancelCommandLocked(command)
	default:
		return nil
	}
}

func (a *App) startManagedHeadless(command control.DaemonCommand) []eventcontract.Event {
	cfg := a.headlessRuntime
	now := time.Now().UTC()
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		if command.AutoRestore {
			a.setHeadlessRestoreBackoffLocked(command.SurfaceSessionID, "headless_restore_start_failed", now)
		}
		return a.service.HandleHeadlessLaunchFailed(
			command.SurfaceSessionID,
			command.InstanceID,
			agentproto.ErrorInfo{
				Code:             "headless_binary_missing",
				Layer:            "daemon",
				Stage:            "headless_start",
				Operation:        "start_headless",
				Message:          "headless 启动器未配置可执行文件。",
				SurfaceSessionID: command.SurfaceSessionID,
				ThreadID:         command.ThreadID,
			},
		)
	}

	env := append([]string{}, cfg.BaseEnv...)
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+command.InstanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=headless",
		"CODEX_REMOTE_INSTANCE_MANAGED=1",
		"CODEX_REMOTE_LIFETIME=daemon-owned",
	)
	if strings.TrimSpace(command.ThreadCWD) == "" {
		env = append(env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless")
	}

	workDir := strings.TrimSpace(command.ThreadCWD)
	if workDir == "" {
		workDir = strings.TrimSpace(cfg.Paths.StateDir)
	}

	pid, err := a.startHeadless(relayruntime.HeadlessLaunchOptions{
		BinaryPath: cfg.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		Env:        env,
		Paths:      cfg.Paths,
		WorkDir:    workDir,
		InstanceID: command.InstanceID,
		Args:       cfg.LaunchArgs,
	})
	if err != nil {
		log.Printf(
			"headless start failed: surface=%s instance=%s thread=%s cwd=%s err=%v",
			command.SurfaceSessionID,
			command.InstanceID,
			command.ThreadID,
			command.ThreadCWD,
			err,
		)
		if command.AutoRestore {
			a.setHeadlessRestoreBackoffLocked(command.SurfaceSessionID, "headless_restore_start_failed", now)
		}
		return a.service.HandleHeadlessLaunchFailed(command.SurfaceSessionID, command.InstanceID, err)
	}

	a.managedHeadlessRuntime.Processes[command.InstanceID] = &headlessruntime.Process{
		InstanceID:    command.InstanceID,
		PID:           pid,
		RequestedAt:   now,
		StartedAt:     now,
		ThreadID:      command.ThreadID,
		ThreadCWD:     workDir,
		WorkspaceRoot: workDir,
		DisplayName:   "headless",
		Status:        headlessruntime.StatusStarting,
	}
	log.Printf(
		"headless start requested: surface=%s instance=%s pid=%d thread=%s cwd=%s",
		command.SurfaceSessionID,
		command.InstanceID,
		pid,
		command.ThreadID,
		workDir,
	)
	return a.service.HandleHeadlessLaunchStarted(command.SurfaceSessionID, command.InstanceID, pid)
}

func (a *App) killManagedHeadless(command control.DaemonCommand) []eventcontract.Event {
	pid := 0
	if managed := a.managedHeadlessRuntime.Processes[command.InstanceID]; managed != nil {
		pid = managed.PID
	}
	if pid == 0 {
		if inst := a.service.Instance(command.InstanceID); inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed {
			pid = inst.PID
		}
	}
	if pid == 0 {
		if strings.TrimSpace(command.SurfaceSessionID) == "" {
			return nil
		}
		return a.service.HandleProblem(command.InstanceID, agentproto.ErrorInfo{
			Code:             "headless_pid_unknown",
			Layer:            "daemon",
			Stage:            "headless_kill",
			Operation:        "kill_instance",
			Message:          "找不到可结束的 headless 进程。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		})
	}
	if err := a.stopProcess(pid, a.headlessRuntime.KillGrace); err != nil {
		log.Printf(
			"headless kill failed: surface=%s instance=%s pid=%d err=%v",
			command.SurfaceSessionID,
			command.InstanceID,
			pid,
			err,
		)
		if strings.TrimSpace(command.SurfaceSessionID) == "" {
			return nil
		}
		return a.service.HandleProblem(command.InstanceID, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:             "headless_kill_failed",
			Layer:            "daemon",
			Stage:            "headless_kill",
			Operation:        "kill_instance",
			Message:          "无法结束 headless 实例。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		}))
	}
	delete(a.managedHeadlessRuntime.Processes, command.InstanceID)
	a.service.RemoveInstance(command.InstanceID)
	log.Printf("headless kill requested: surface=%s instance=%s pid=%d", command.SurfaceSessionID, command.InstanceID, pid)
	return nil
}

func (a *App) observeManagedHeadless(inst *state.InstanceRecord) {
	if inst == nil || !strings.EqualFold(strings.TrimSpace(inst.Source), "headless") || !inst.Managed {
		return
	}
	now := time.Now().UTC()
	managed := a.managedHeadlessRuntime.Processes[inst.InstanceID]
	if managed == nil {
		managed = &headlessruntime.Process{
			InstanceID:  inst.InstanceID,
			RequestedAt: now,
			StartedAt:   now,
		}
		a.managedHeadlessRuntime.Processes[inst.InstanceID] = managed
	}
	if inst.PID > 0 {
		managed.PID = inst.PID
	}
	if strings.TrimSpace(inst.DisplayName) != "" {
		managed.DisplayName = inst.DisplayName
	}
	if strings.TrimSpace(inst.WorkspaceRoot) != "" {
		managed.WorkspaceRoot = inst.WorkspaceRoot
	}
	managed.LastHelloAt = now
	managed.LastError = ""
	a.syncManagedHeadlessLocked(now)
}

type managedHeadlessShutdownTarget struct {
	InstanceID string
	PID        int
}

func (a *App) shutdownManagedHeadless(skipStop map[string]struct{}) error {
	a.mu.Lock()
	targets := a.collectManagedHeadlessShutdownTargetsLocked()
	a.mu.Unlock()

	if len(targets) == 0 {
		return nil
	}

	var errs []error
	for _, target := range targets {
		if _, handled := skipStop[target.InstanceID]; handled {
			log.Printf("managed headless shutdown cleanup: instance=%s handled by relay drain", target.InstanceID)
		} else if target.PID > 0 {
			if err := a.stopProcess(target.PID, a.headlessRuntime.KillGrace); err != nil {
				log.Printf("managed headless shutdown cleanup failed: instance=%s pid=%d err=%v", target.InstanceID, target.PID, err)
				errs = append(errs, fmt.Errorf("stop managed headless %s (pid %d): %w", target.InstanceID, target.PID, err))
			} else {
				log.Printf("managed headless shutdown cleanup: instance=%s pid=%d", target.InstanceID, target.PID)
			}
		} else {
			log.Printf("managed headless shutdown cleanup: instance=%s pid=unknown", target.InstanceID)
		}

		a.mu.Lock()
		delete(a.managedHeadlessRuntime.Processes, target.InstanceID)
		a.service.RemoveInstance(target.InstanceID)
		a.mu.Unlock()
	}

	return errors.Join(errs...)
}

func (a *App) collectManagedHeadlessShutdownTargetsLocked() []managedHeadlessShutdownTarget {
	targets := make([]managedHeadlessShutdownTarget, 0, len(a.managedHeadlessRuntime.Processes))
	seen := make(map[string]bool, len(a.managedHeadlessRuntime.Processes))

	appendTarget := func(instanceID string, pid int) {
		instanceID = strings.TrimSpace(instanceID)
		if instanceID == "" || seen[instanceID] {
			return
		}
		seen[instanceID] = true
		targets = append(targets, managedHeadlessShutdownTarget{
			InstanceID: instanceID,
			PID:        pid,
		})
	}

	for instanceID, managed := range a.managedHeadlessRuntime.Processes {
		if managed == nil {
			appendTarget(instanceID, 0)
			continue
		}
		pid := managed.PID
		if pid == 0 {
			if inst := a.service.Instance(instanceID); headlessruntime.IsManagedInstance(inst) {
				pid = inst.PID
			}
		}
		appendTarget(instanceID, pid)
	}

	for _, inst := range a.service.Instances() {
		if !headlessruntime.IsManagedInstance(inst) {
			continue
		}
		appendTarget(inst.InstanceID, inst.PID)
	}

	return targets
}
