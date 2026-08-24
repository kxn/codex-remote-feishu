package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	headlessruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/headlessruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/app/opencodeprofile"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
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
		return a.startManagedHeadlessLocked(command)
	case control.DaemonCommandKillHeadless:
		return a.killManagedHeadless(command)
	case control.DaemonCommandAdmin:
		return a.handleAdminDaemonCommand(command)
	case control.DaemonCommandDebug:
		return a.handleDebugDaemonCommand(command)
	case control.DaemonCommandGPUStatus:
		return a.handleGPUStatusDaemonCommand(command)
	case control.DaemonCommandCron:
		return a.handleCronDaemonCommandLocked(command)
	case control.DaemonCommandMCPOAuthLogin:
		return a.handleMCPOAuthLoginDaemonCommandLocked(command)
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
	if strings.EqualFold(string(command.Backend), string(agentproto.BackendCodex)) {
		a.maybeRetryCodexRuntimeProbeIfDue(context.Background())
		a.maybeRetryCodexNativeProbeIfDue(context.Background())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startManagedHeadlessLocked(command)
}

func (a *App) startManagedHeadlessLocked(command control.DaemonCommand) []eventcontract.Event {
	cfg := a.headlessRuntime
	now := time.Now().UTC()
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfo{
			Code:             "headless_binary_missing",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "headless 启动器未配置可执行文件。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
		}, now)
	}

	env := append([]string{}, cfg.BaseEnv...)
	claudeRuntimeSettings := config.ClaudeRuntimeSettings{}
	if strings.TrimSpace(string(command.Backend)) == "" {
		errInfo := agentproto.ErrorInfo{
			Code:             "headless_backend_missing",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "headless 启动合同缺少 backend。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
		}
		return a.handleManagedHeadlessLaunchFailure(command, errInfo, now)
	}
	backend, ok := agentproto.ParseBackend(command.Backend)
	if !ok {
		rawBackend := strings.TrimSpace(string(command.Backend))
		errInfo := agentproto.ErrorInfo{
			Code:             "headless_backend_unsupported",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          fmt.Sprintf("不支持的 headless backend：%s。", rawBackend),
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
		}
		return a.handleManagedHeadlessLaunchFailure(command, errInfo, now)
	}
	if backend == agentproto.BackendCodex && a.profileCatalogMigrationErr != nil {
		return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfoFromError(a.profileCatalogMigrationErr, agentproto.ErrorInfo{
			Code:             "profile_catalog_degraded",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "Codex Profile 配置迁移未完成，当前不能启动新实例。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        false,
		}), now)
	}
	if backend == agentproto.BackendCodex && a.surfaceProfileSelectionConflict(command.SurfaceSessionID) {
		return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfo{
			Code:             surfaceresume.CodexProfileSelectionStatusConflict,
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "当前工作区的 Codex Profile 选择存在迁移冲突，请重新选择后再试。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        false,
		}, now)
	}
	launchAuthorization := a.captureHeadlessLaunchAuthorizationLocked(command)
	if !a.headlessLaunchStillAuthorizedLocked(launchAuthorization) {
		return nil
	}
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+command.InstanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=headless",
		"CODEX_REMOTE_INSTANCE_MANAGED=1",
		"CODEX_REMOTE_LIFETIME=daemon-owned",
		"CODEX_REMOTE_INSTANCE_BACKEND="+string(backend),
	)
	if strings.TrimSpace(command.ThreadID) != "" {
		env = append(env, config.ResumeThreadIDEnv+"="+strings.TrimSpace(command.ThreadID))
	}
	if backend == agentproto.BackendCodex {
		env = append(env, config.CodexRuntimeProfileIDEnv+"="+state.NormalizeCodexProfileID(command.CodexProfileID))
	}
	if backend == agentproto.BackendClaude {
		env = append(env, config.ClaudeRuntimeProfileIDEnv+"="+state.NormalizeClaudeProfileID(command.ClaudeProfileID))
	}
	if backend == agentproto.BackendOpenCode {
		env = append(env, config.OpenCodeRuntimeProfileIDEnv+"="+state.NormalizeOpenCodeProfileID(command.OpenCodeProfileID))
		if accessMode := state.NormalizeOpenCodeRuntimeAccessMode(command.OpenCodeRuntimeAccessMode); accessMode != "" {
			env = config.UpsertEnvValue(env, config.OpenCodeRuntimeAccessModeEnv, accessMode)
		}
	}
	launchArgs := append([]string{}, cfg.LaunchArgs...)
	env, launchArgs, codexProjection, err := a.applyCodexHeadlessProfileConfigLocked(env, launchArgs, backend, command.CodexProfileID, command.CodexAdmissionRef)
	if err != nil {
		return a.handleManagedHeadlessLaunchFailure(command, codexHeadlessLaunchProblem(err, agentproto.ErrorInfo{
			Code:             "codex_profile_prepare_failed",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "Codex Profile 准备失败。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		}), now)
	}
	if codexProjection != nil {
		a.service.RecordPendingHeadlessCodexRuntime(command.SurfaceSessionID, command.InstanceID, command.CodexAdmissionRef, &codexProjection.Connection, &codexProjection.Thread)
	}
	env, launchArgs, opencodeAdmissionRef, err := a.applyOpenCodeHeadlessProfileConfigLocked(env, launchArgs, backend, command)
	if err != nil {
		return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:             "opencode_profile_prepare_failed",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "OpenCode Profile 准备失败。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		}), now)
	}
	if opencodeAdmissionRef != nil {
		a.service.RecordPendingHeadlessOpenCodeRuntime(command.SurfaceSessionID, command.InstanceID, opencodeAdmissionRef)
	}
	env, claudeRuntimeSettings, err = a.applyClaudeHeadlessProfileEnv(env, backend, command.ClaudeProfileID)
	if err != nil {
		return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:             "claude_profile_prepare_failed",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          "Claude 配置准备失败。",
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        true,
		}), now)
	}
	if backend == agentproto.BackendClaude {
		if effort := state.NormalizeClaudeReasoningEffort(command.ClaudeReasoningEffort); effort != "" {
			env = config.ApplyClaudeReasoningLaunchEnv(env, effort)
			claudeRuntimeSettings = config.MergeClaudeRuntimeSettings(
				claudeRuntimeSettings,
				config.ClaudeReasoningRuntimeSettings(effort),
			)
		}
		if !claudeRuntimeSettings.Empty() {
			raw, marshalErr := config.MarshalClaudeRuntimeSettings(claudeRuntimeSettings)
			if marshalErr != nil {
				return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfoFromError(marshalErr, agentproto.ErrorInfo{
					Code:             "claude_settings_prepare_failed",
					Layer:            "daemon",
					Stage:            "headless_start",
					Operation:        "start_headless",
					Message:          "Claude 运行时配置准备失败。",
					SurfaceSessionID: command.SurfaceSessionID,
					ThreadID:         command.ThreadID,
					Retryable:        true,
				}), now)
			}
			env = config.UpsertEnvValue(env, config.ClaudeRuntimeSettingsJSONEnv, raw)
		}
	}
	if strings.TrimSpace(command.ThreadCWD) == "" {
		env = append(env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless")
	}

	commandWorkspaceKey := headlessCommandWorkspaceKey(command)
	workDir := pathcanon.Native(commandWorkspaceKey)
	if workDir == "" {
		workDir = pathcanon.Native(cfg.Paths.StateDir)
	}
	if err := validateHeadlessWorkDir(workDir); err != nil {
		return a.handleManagedHeadlessLaunchFailure(command, agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:             "headless_workspace_missing",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "start_headless",
			Message:          fmt.Sprintf("工作目录不存在或不可用：%s", workDir),
			Details:          workDir,
			SurfaceSessionID: command.SurfaceSessionID,
			ThreadID:         command.ThreadID,
			Retryable:        false,
		}), now)
	}
	if !a.headlessLaunchStillAuthorizedLocked(launchAuthorization) {
		return nil
	}

	pid, err := a.startHeadless(relayruntime.HeadlessLaunchOptions{
		BinaryPath: cfg.BinaryPath,
		ConfigPath: cfg.ConfigPath,
		Env:        env,
		Paths:      cfg.Paths,
		WorkDir:    workDir,
		InstanceID: command.InstanceID,
		LaunchMode: headlessLaunchModeForBackend(backend),
		Args:       launchArgs,
	})
	if err != nil {
		log.Printf(
			"headless start failed: surface=%s instance=%s thread=%s cwd=%s err=%v",
			command.SurfaceSessionID,
			command.InstanceID,
			command.ThreadID,
			commandWorkspaceKey,
			err,
		)
		return a.handleManagedHeadlessLaunchFailure(command, err, now)
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

type headlessLaunchAuthorization struct {
	surfaceID           string
	instanceID          string
	requirePending      bool
	authorizedAtCapture bool
}

func (a *App) captureHeadlessLaunchAuthorizationLocked(command control.DaemonCommand) headlessLaunchAuthorization {
	auth := headlessLaunchAuthorization{
		surfaceID:           strings.TrimSpace(command.SurfaceSessionID),
		instanceID:          strings.TrimSpace(command.InstanceID),
		authorizedAtCapture: true,
	}
	if auth.surfaceID == "" || auth.instanceID == "" {
		return auth
	}
	auth.requirePending = true
	auth.authorizedAtCapture = false
	surface := a.service.Surface(auth.surfaceID)
	if surface == nil || surface.PendingHeadless == nil {
		return auth
	}
	auth.authorizedAtCapture = strings.TrimSpace(surface.PendingHeadless.InstanceID) == auth.instanceID
	return auth
}

func (a *App) headlessLaunchStillAuthorizedLocked(auth headlessLaunchAuthorization) bool {
	if a.shuttingDown {
		return false
	}
	if auth.instanceID != "" && a.managedHeadlessRuntime.Processes[auth.instanceID] != nil {
		return false
	}
	if !auth.requirePending {
		return true
	}
	if !auth.authorizedAtCapture {
		return false
	}
	surface := a.service.Surface(auth.surfaceID)
	return surface != nil &&
		surface.PendingHeadless != nil &&
		strings.TrimSpace(surface.PendingHeadless.InstanceID) == auth.instanceID
}

func (a *App) surfaceProfileSelectionConflict(surfaceID string) bool {
	if a.surfaceResumeRuntime.store == nil {
		return false
	}
	entry, ok := a.surfaceResumeRuntime.store.Get(surfaceID)
	return ok && entry.CodexProfileSelectionStatus == surfaceresume.CodexProfileSelectionStatusConflict
}

func validateHeadlessWorkDir(workDir string) error {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return nil
	}
	info, err := os.Stat(workDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("headless workdir is not a directory: %s", workDir)
	}
	return nil
}

func (a *App) handleManagedHeadlessLaunchFailure(command control.DaemonCommand, err error, now time.Time) []eventcontract.Event {
	events := a.service.HandleHeadlessLaunchFailed(command.SurfaceSessionID, command.InstanceID, err)
	if _, ok := a.consumeGroupOnDemandResumeContinuationLocked(command.SurfaceSessionID); ok {
		displayCode, emit := a.recordGroupOnDemandTerminalFailureLocked(
			command.SurfaceSessionID,
			orchestrator.HeadlessRestoreLaunchFailureCode(err),
		)
		return rewriteHeadlessRestoreFailureEvents(events, displayCode, emit)
	}
	if !command.AutoRestore {
		return events
	}
	displayCode, emit := a.recordSurfaceResumeFailureLocked(
		command.SurfaceSessionID,
		orchestrator.HeadlessRestoreLaunchFailureCode(err),
		now,
	)
	return rewriteHeadlessRestoreFailureEvents(events, displayCode, emit)
}

func headlessLaunchModeForBackend(backend agentproto.Backend) string {
	switch agentproto.NormalizeBackend(backend) {
	case agentproto.BackendClaude:
		return relayruntime.HeadlessLaunchModeClaudeAppServer
	case agentproto.BackendOpenCode:
		return relayruntime.HeadlessLaunchModeOpenCodeACP
	default:
		return relayruntime.HeadlessLaunchModeAppServer
	}
}

func headlessCommandWorkspaceKey(command control.DaemonCommand) string {
	return state.ResolveHeadlessResumeWorkspaceKey(command.WorkspaceKey, command.ThreadCWD)
}

func (a *App) applyOpenCodeHeadlessProfileConfigLocked(baseEnv, baseArgs []string, backend agentproto.Backend, command control.DaemonCommand) ([]string, []string, *state.OpenCodeAdmissionRef, error) {
	env := append([]string{}, baseEnv...)
	args := append([]string{}, baseArgs...)
	if agentproto.NormalizeBackend(backend) != agentproto.BackendOpenCode {
		return env, args, nil, nil
	}
	env = applyOpenCodeHeadlessRuntimePathEnv(env, a.headlessRuntime.Paths)
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	profile, err := resolveOpenCodeLaunchProfile(loaded.Config, command.OpenCodeProfileID, command.OpenCodeAdmissionRef)
	if err != nil {
		return nil, nil, nil, err
	}
	workspaceRoot := headlessCommandWorkspaceKey(command)
	material, err := opencodeprofile.CompileLaunchMaterial(opencodeprofile.CompileInput{
		Profile:           profile,
		WorkspaceRoot:     workspaceRoot,
		RuntimeDir:        filepath.Join(a.headlessRuntime.Paths.StateDir, "opencode", strings.TrimSpace(command.InstanceID)),
		BaseEnv:           env,
		RuntimeAccessMode: command.OpenCodeRuntimeAccessMode,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return material.Env, material.Args, state.NormalizeOpenCodeAdmissionRef(material.AdmissionRef), nil
}

func applyOpenCodeHeadlessRuntimePathEnv(env []string, paths relayruntime.Paths) []string {
	if value := xdgHomeForOpenCodeHeadlessPath(paths.ConfigDir); value != "" {
		env = config.UpsertEnvValue(env, "XDG_CONFIG_HOME", value)
	}
	if value := xdgHomeForOpenCodeHeadlessPath(paths.DataDir); value != "" {
		env = config.UpsertEnvValue(env, "XDG_DATA_HOME", value)
	}
	if value := xdgHomeForOpenCodeHeadlessPath(paths.StateDir); value != "" {
		env = config.UpsertEnvValue(env, "XDG_STATE_HOME", value)
	}
	return env
}

func xdgHomeForOpenCodeHeadlessPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return dir
}

func resolveOpenCodeLaunchProfile(cfg config.AppConfig, profileID string, admissionRef *state.OpenCodeAdmissionRef) (config.OpenCodeProfile, error) {
	normalizedID := state.NormalizeOpenCodeProfileID(profileID)
	ref := state.NormalizeOpenCodeAdmissionRef(admissionRef)
	if ref != nil && state.NormalizeOpenCodeProfileID(ref.ProfileRef.ID) != normalizedID {
		// admission ref 属于另一个 profile（例如 bot 级 profile 切换后 surface
		// 缓存未刷新或恢复路径缺 ref）：以期望 profile 为准，丢弃过期 ref 并按
		// 当前 revision 解析，与 Codex 行为一致。
		ref = nil
	}
	if ref != nil {
		if normalizedID == state.DefaultOpenCodeProfileID {
			profile := config.BuiltInOpenCodeProfile()
			profile.Revision = ref.ProfileRef.Revision
			return profile, nil
		}
		for _, record := range cfg.OpenCode.Profiles {
			recordID := config.NormalizeOpenCodeProfileID(record.ID)
			if recordID != normalizedID {
				continue
			}
			for _, revision := range record.Revisions {
				if config.NormalizeOpenCodeProfileID(revision.ID) == normalizedID && revision.Revision == ref.ProfileRef.Revision {
					revision.ID = normalizedID
					return config.OpenCodeProfile{OpenCodeAPIProfileSecretConfig: revision}, nil
				}
			}
		}
		return config.OpenCodeProfile{}, fmt.Errorf("opencode profile revision %s@%d not found", normalizedID, ref.ProfileRef.Revision)
	}
	profile, ok := config.ResolveOpenCodeProfile(cfg, normalizedID)
	if !ok {
		return config.OpenCodeProfile{}, fmt.Errorf("opencode profile %q not found", normalizedID)
	}
	return profile, nil
}

func (a *App) killManagedHeadless(command control.DaemonCommand) []eventcontract.Event {
	pid := 0
	if managed := a.managedHeadlessRuntime.Processes[command.InstanceID]; managed != nil {
		pid = managed.PID
	}
	if pid == 0 {
		if inst := a.service.Instance(command.InstanceID); inst != nil && headlessruntime.IsManagedInstance(inst) {
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
	if inst == nil || !headlessruntime.IsManagedInstance(inst) {
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
