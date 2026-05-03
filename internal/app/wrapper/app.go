package wrapper

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type App struct {
	config  Config
	runtime backendRuntime
}

const (
	steerCommandResponseTimeout = 5 * time.Second
	wrapperChildStopGrace       = 2 * time.Second
	wrapperChildWaitTimeout     = 5 * time.Second
)

type shutdownRequest struct {
	CommandID string
}

type restartRequest struct {
	CommandID string
	Target    *agentproto.Target
	EmitEvent bool
	ResultCh  chan error
}

type Config struct {
	RelayServerURL  string
	CodexRealBinary string
	NameMode        string
	Args            []string
	ConfigPath      string

	InstanceID            string
	DisplayName           string
	WorkspaceRoot         string
	WorkspaceKey          string
	ShortName             string
	Backend               agentproto.Backend
	CodexProviderID       string
	ClaudeProfileID       string
	ClaudeReasoningEffort string
	ResumeThreadID        string
	Source                string
	Managed               bool
	Lifetime              string
	ParentPID             int
	Version               string
	Branch                string
	BuildFingerprint      string
	BinaryPath            string
	ChildProxyEnv         []string
	DaemonBinaryPath      string
	DaemonUseSystemProxy  bool
	RuntimePaths          relayruntime.Paths
	DebugRelayFlow        bool
	DebugRelayRaw         bool
	RawLogPath            string
}

func LoadConfig(args []string, version, branch string) (Config, error) {
	loaded, err := config.LoadWrapperConfig()
	if err != nil {
		return Config{}, err
	}
	loaded.CodexRealBinary, err = resolveNormalCodexBinary(loaded.ConfigPath, loaded.CodexRealBinary)
	if err != nil {
		return Config{}, err
	}
	services, err := config.LoadServicesConfig()
	if err != nil {
		return Config{}, err
	}
	workspaceRoot, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	workspaceRoot, err = state.ResolveWorkspaceRootOnHost(workspaceRoot)
	if err != nil {
		return Config{}, err
	}
	instanceID := strings.TrimSpace(os.Getenv("CODEX_REMOTE_INSTANCE_ID"))
	if instanceID == "" {
		instanceID, err = generateInstanceID()
		if err != nil {
			return Config{}, err
		}
	}
	shortName := state.WorkspaceShortName(workspaceRoot)
	displayName := shortName
	if displayName == "." || displayName == "/" {
		displayName = workspaceRoot
	}
	if override := strings.TrimSpace(os.Getenv("CODEX_REMOTE_INSTANCE_DISPLAY_NAME")); override != "" {
		displayName = override
	}
	source := strings.TrimSpace(os.Getenv("CODEX_REMOTE_INSTANCE_SOURCE"))
	if source == "" {
		source = "vscode"
	}
	managed := parseBoolEnv("CODEX_REMOTE_INSTANCE_MANAGED")
	lifetime, parentPID, err := resolveInstanceLifetime(
		source,
		managed,
		os.Getenv("CODEX_REMOTE_LIFETIME"),
		os.Getenv("CODEX_REMOTE_PARENT_PID"),
		os.Getppid(),
	)
	if err != nil {
		return Config{}, err
	}
	paths, err := relayruntime.DefaultPaths()
	if err != nil {
		return Config{}, err
	}
	binaryIdentity, err := relayruntime.CurrentBinaryIdentityWithBranch(version, branch)
	if err != nil {
		return Config{}, err
	}
	return Config{
		RelayServerURL:        loaded.RelayServerURL,
		CodexRealBinary:       loaded.CodexRealBinary,
		NameMode:              loaded.NameMode,
		Args:                  args,
		ConfigPath:            firstNonEmpty(services.ConfigPath, loaded.ConfigPath, paths.ConfigFile),
		InstanceID:            instanceID,
		DisplayName:           displayName,
		WorkspaceRoot:         workspaceRoot,
		WorkspaceKey:          state.ResolveWorkspaceKey(workspaceRoot),
		ShortName:             shortName,
		Backend:               agentproto.NormalizeBackend(agentproto.Backend(os.Getenv("CODEX_REMOTE_INSTANCE_BACKEND"))),
		CodexProviderID:       state.NormalizeCodexProviderID(os.Getenv(config.CodexRuntimeProviderIDEnv)),
		ClaudeProfileID:       state.NormalizeClaudeProfileID(os.Getenv(config.ClaudeRuntimeProfileIDEnv)),
		ClaudeReasoningEffort: state.NormalizeReasoningEffort(os.Getenv(config.ClaudeEffortLevelEnv)),
		ResumeThreadID:        strings.TrimSpace(os.Getenv(config.ResumeThreadIDEnv)),
		Source:                source,
		Managed:               managed,
		Lifetime:              string(lifetime),
		ParentPID:             parentPID,
		Version:               firstNonEmpty(strings.TrimSpace(version), "dev"),
		Branch:                firstNonEmpty(strings.TrimSpace(branch), "dev"),
		BuildFingerprint:      binaryIdentity.BuildFingerprint,
		BinaryPath:            binaryIdentity.BinaryPath,
		ChildProxyEnv:         config.CaptureAndClearProxyEnv(),
		DaemonBinaryPath:      binaryIdentity.BinaryPath,
		DaemonUseSystemProxy:  services.FeishuUseSystemProxy,
		RuntimePaths:          paths,
		DebugRelayFlow:        loaded.DebugRelayFlow || services.DebugRelayFlow,
		DebugRelayRaw:         loaded.DebugRelayRaw || services.DebugRelayRaw,
		RawLogPath:            relayruntime.WrapperRawLogFile(paths.LogsDir, os.Getpid()),
	}, nil
}

func New(cfg Config) *App {
	cfg.Backend = agentproto.NormalizeBackend(cfg.Backend)
	runtime := newBackendRuntime(cfg)
	if cfg.DebugRelayFlow {
		if debuggable, ok := runtime.(runtimeDebugLogger); ok {
			debuggable.SetDebugLogger(func(format string, args ...any) {
				log.Printf("relay flow translator: "+format, args...)
			})
		}
	}
	return &App{config: cfg, runtime: runtime}
}

func (a *App) debugf(format string, args ...any) {
	if a.config.DebugRelayFlow {
		log.Printf("relay flow wrapper: "+format, args...)
	}
}

func (a *App) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	rawLogger, err := a.openRawLogger()
	if err != nil {
		log.Printf("relay raw wrapper log disabled: %v", err)
	}
	if rawLogger != nil {
		defer rawLogger.Close()
	}

	manager := relayruntime.NewManager(relayruntime.ManagerConfig{
		RelayServerURL: a.config.RelayServerURL,
		Identity: agentproto.BinaryIdentity{
			Product:          relayruntime.ProductName,
			Version:          a.config.Version,
			Branch:           a.config.Branch,
			BuildFingerprint: a.config.BuildFingerprint,
			BinaryPath:       a.config.BinaryPath,
		},
		ConfigPath:           a.config.ConfigPath,
		Paths:                a.config.RuntimePaths,
		DaemonBinaryPath:     firstNonEmpty(a.config.DaemonBinaryPath, a.config.BinaryPath),
		DaemonUseSystemProxy: a.config.DaemonUseSystemProxy,
		CapturedProxyEnv:     a.config.ChildProxyEnv,
		MismatchAction:       relayruntime.ProbeMismatchRefuseReplace,
	})
	if err := manager.EnsureReady(ctx); err != nil {
		return 1, err
	}
	a.debugf("runtime ready: relay=%s instance=%s workspace=%s", a.config.RelayServerURL, a.config.InstanceID, a.config.WorkspaceRoot)

	writeCh := make(chan []byte, 128)
	errCh := make(chan error, 8)
	problems := &problemReporter{}
	commandResponses := newCommandResponseTracker()
	turnTracker := newRuntimeTurnTracker()
	var activeChildGeneration int64 = 1
	shutdownCh := make(chan shutdownRequest, 1)
	restartCh := make(chan restartRequest, 1)
	hostExitCh := make(chan struct{}, 1)

	if err := startHostLifetimeWatcher(ctx, a.config, func() {
		select {
		case hostExitCh <- struct{}{}:
		default:
		}
	}); err != nil {
		return 1, err
	}

	var client *relayws.Client
	var activeChild *childSession
	connectedOnce := false
	client = relayws.NewClient(a.config.RelayServerURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID:            a.config.InstanceID,
			DisplayName:           a.config.DisplayName,
			WorkspaceRoot:         a.config.WorkspaceRoot,
			WorkspaceKey:          a.config.WorkspaceKey,
			ShortName:             a.config.ShortName,
			Backend:               a.runtime.Backend(),
			CodexProviderID:       strings.TrimSpace(a.config.CodexProviderID),
			ClaudeProfileID:       strings.TrimSpace(a.config.ClaudeProfileID),
			ClaudeReasoningEffort: strings.TrimSpace(a.config.ClaudeReasoningEffort),
			Source:                a.config.Source,
			Managed:               a.config.Managed,
			Version:               a.config.Version,
			Branch:                a.config.Branch,
			BuildFingerprint:      a.config.BuildFingerprint,
			BinaryPath:            a.config.BinaryPath,
			PID:                   os.Getpid(),
		},
		Capabilities:         a.runtime.Capabilities(),
		CapabilitiesDeclared: true,
	}, relayws.ClientCallbacks{
		OnWelcome: func(_ context.Context, welcome agentproto.Welcome) error {
			a.debugf("relay welcome: connectedOnce=%t server=%s", connectedOnce, relayWelcomeSummary(welcome))
			if manager.WelcomeCompatible(welcome) {
				connectedOnce = true
				return nil
			}
			if connectedOnce {
				return relayws.FatalError{Err: fmt.Errorf("relay version mismatch after connection: %s", relayWelcomeSummary(welcome))}
			}
			return fmt.Errorf("relay bootstrap welcome mismatch: %s", relayWelcomeSummary(welcome))
		},
		OnConnect: func(context.Context) error {
			a.debugf("relay connected: instance=%s connectedOnce=%t", a.config.InstanceID, connectedOnce)
			problems.Flush()
			return nil
		},
		OnError: func(_ context.Context, problem agentproto.ErrorInfo) error {
			problems.Emit(problem)
			return nil
		},
		OnCommand: func(ctx context.Context, command agentproto.Command) error {
			if command.Kind == agentproto.CommandProcessExit {
				a.debugf("relay shutdown command received: command=%s", command.CommandID)
				select {
				case shutdownCh <- shutdownRequest{CommandID: command.CommandID}:
				default:
				}
				return nil
			}
			if command.Kind == agentproto.CommandProcessChildRestart {
				a.debugf("relay child restart command received: command=%s", command.CommandID)
				if activeChild == nil {
					return agentproto.ErrorInfo{
						Code:      "child_restart_not_supported",
						Layer:     "wrapper",
						Stage:     "restart_child_launch",
						Operation: string(agentproto.CommandProcessChildRestart),
						Message:   "当前 backend 没有可重启的 provider child。",
						CommandID: command.CommandID,
					}
				}
				request := restartRequest{
					CommandID: command.CommandID,
					EmitEvent: true,
					ResultCh:  make(chan error, 1),
				}
				select {
				case restartCh <- request:
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case err := <-request.ResultCh:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			a.debugf(
				"relay command received: command=%s kind=%s thread=%s turn=%s cwd=%s surface=%s inputs=%d",
				command.CommandID,
				command.Kind,
				command.Target.ThreadID,
				command.Target.TurnID,
				command.Target.CWD,
				firstNonEmpty(command.Origin.Surface, command.Origin.ChatID),
				len(command.Prompt.Inputs),
			)
			result, err := a.runtime.TranslateCommand(command)
			if err != nil {
				a.debugf("relay command translation failed: command=%s err=%v", command.CommandID, err)
				if problem, ok := err.(agentproto.ErrorInfo); ok {
					return problem.Normalize()
				}
				return agentproto.ErrorInfo{
					Code:             "translate_command_failed",
					Layer:            "wrapper",
					Stage:            "translate_command",
					Operation:        string(command.Kind),
					Message:          "wrapper 无法把 relay 命令转换成当前 backend 的 provider 请求。",
					Details:          err.Error(),
					SurfaceSessionID: command.Origin.Surface,
					CommandID:        command.CommandID,
					ThreadID:         command.Target.ThreadID,
					TurnID:           command.Target.TurnID,
				}
			}
			if result.Restart != nil {
				if activeChild == nil {
					return agentproto.ErrorInfo{
						Code:             "child_restart_not_supported",
						Layer:            "wrapper",
						Stage:            "prepare_child_restart",
						Operation:        string(command.Kind),
						Message:          "当前 Claude runtime 无法在不重启 child 的情况下切换到目标会话。",
						SurfaceSessionID: command.Origin.Surface,
						CommandID:        command.CommandID,
						ThreadID:         command.Target.ThreadID,
						TurnID:           command.Target.TurnID,
					}
				}
				request := restartRequest{
					CommandID: command.CommandID,
					Target:    &result.Restart.Target,
					EmitEvent: false,
					ResultCh:  make(chan error, 1),
				}
				select {
				case restartCh <- request:
				case <-ctx.Done():
					return ctx.Err()
				}
				select {
				case err := <-request.ResultCh:
					if err != nil {
						return err
					}
				case <-ctx.Done():
					return ctx.Err()
				}
				result, err = a.runtime.TranslateCommand(command)
				if err != nil {
					if problem, ok := err.(agentproto.ErrorInfo); ok {
						return problem.Normalize()
					}
					return agentproto.ErrorInfo{
						Code:             "translate_command_failed",
						Layer:            "wrapper",
						Stage:            "translate_command",
						Operation:        string(command.Kind),
						Message:          "wrapper 无法把 relay 命令转换成当前 backend 的 provider 请求。",
						Details:          err.Error(),
						SurfaceSessionID: command.Origin.Surface,
						CommandID:        command.CommandID,
						ThreadID:         command.Target.ThreadID,
						TurnID:           command.Target.TurnID,
					}
				}
				if result.Restart != nil {
					return agentproto.ErrorInfo{
						Code:             "command_restart_not_converged",
						Layer:            "wrapper",
						Stage:            "translate_command",
						Operation:        string(command.Kind),
						Message:          "Claude 会话切换后仍未收敛到目标会话，当前无法继续发送请求。",
						SurfaceSessionID: command.Origin.Surface,
						CommandID:        command.CommandID,
						ThreadID:         command.Target.ThreadID,
						TurnID:           command.Target.TurnID,
					}
				}
			}
			a.debugf(
				"relay command translated: command=%s events=%s outbound=%d frames=%s",
				command.CommandID,
				summarizeEventKinds(result.Events),
				len(result.OutboundToChild),
				summarizeFrames(result.OutboundToChild),
			)
			var waitCh <-chan *agentproto.ErrorInfo
			responseGate := result.CommandResponseGate
			if responseGate != nil {
				waitCh = commandResponses.Register(
					responseGate.RequestID,
					responseGate.RejectProblem,
					responseGate.SuppressFrame,
				)
			}
			turnTracker.ObserveEvents(result.Events)
			if len(result.Events) != 0 {
				if sendErr := client.SendEvents(result.Events); sendErr != nil {
					return agentproto.ErrorInfoFromError(sendErr, agentproto.ErrorInfo{
						Code:             "relay_send_local_command_events_failed",
						Layer:            "wrapper",
						Stage:            "translate_command",
						Operation:        string(command.Kind),
						Message:          "wrapper 无法把 Claude 本地 catalog/history 事件发送到 relay。",
						Retryable:        true,
						SurfaceSessionID: command.Origin.Surface,
						CommandID:        command.CommandID,
						ThreadID:         command.Target.ThreadID,
						TurnID:           command.Target.TurnID,
					})
				}
			}
			for _, line := range result.OutboundToChild {
				select {
				case writeCh <- line:
					a.debugf("relay command queued for child: command=%s frame=%s", command.CommandID, summarizeFrame(line))
				case <-ctx.Done():
					if responseGate != nil {
						commandResponses.Cancel(responseGate.RequestID)
					}
					return ctx.Err()
				}
			}
			turnTracker.ObserveCommand(command)
			if responseGate != nil {
				err := waitCommandResponse(ctx, waitCh, responseGate.Timeout, responseGate.TimeoutProblem)
				if err != nil {
					commandResponses.Cancel(responseGate.RequestID)
					a.debugf("relay command response failed: command=%s request=%s err=%v", command.CommandID, responseGate.RequestID, err)
					return err
				}
				a.debugf("relay command response accepted: command=%s request=%s", command.CommandID, responseGate.RequestID)
			}
			return nil
		},
	})
	problems.SetClient(client)
	client.SetRawLogger(rawLogger)

	activeChild, err = a.runtime.Launch(ctx, a, rawLogger, problems.Emit)
	if err != nil {
		return 1, err
	}
	startChildSessionIO(ctx, activeChild, stdout, stderr, writeCh, a.runtime, client, commandResponses, turnTracker, &activeChildGeneration, 1, errCh, a.debugf, rawLogger, problems.Emit)

	go func() {
		if err := runRelayClient(ctx, a.config.RelayServerURL, client, manager, func() bool { return connectedOnce }); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	if activeChild != nil {
		go stdinLoop(ctx, stdin, writeCh, a.runtime, client, errCh, a.debugf, rawLogger, problems.Emit)
	}

	for {
		var waitErrCh <-chan error
		if activeChild != nil {
			waitErrCh = activeChild.waitErr
		}
		select {
		case err := <-waitErrCh:
			waitForSessionStdoutStopped(activeChild, wrapperChildWaitTimeout)
			emitRuntimeExitReconciliation(client, turnTracker, err, problems.Emit)
			drainAndCloseRelayClient(client, problems.Emit)
			if err == nil {
				return 0, nil
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode(), nil
			}
			return 1, err
		case err := <-errCh:
			drainAndCloseRelayClient(client, problems.Emit)
			stopChildSession(activeChild, a.debugf)
			if err == nil || err == context.Canceled {
				return 0, nil
			}
			return 1, err
		case request := <-shutdownCh:
			a.debugf("wrapper shutdown requested by daemon: command=%s", request.CommandID)
			stopChildSession(activeChild, a.debugf)
			drainAndCloseRelayClient(client, problems.Emit)
			return 0, nil
		case request := <-restartCh:
			a.debugf("wrapper child restart requested by daemon: command=%s", request.CommandID)
			nextGeneration := atomic.LoadInt64(&activeChildGeneration) + 1
			nextChild, err := a.restartChildSession(ctx, request, activeChild, stdout, stderr, writeCh, client, commandResponses, turnTracker, &activeChildGeneration, nextGeneration, errCh, rawLogger, problems.Emit)
			if nextChild != nil {
				activeChild = nextChild
			}
			request.ResultCh <- err
			close(request.ResultCh)
		case <-hostExitCh:
			a.debugf("wrapper shutdown requested by host exit: lifetime=%s parentPid=%d", a.config.Lifetime, a.config.ParentPID)
			stopChildSession(activeChild, a.debugf)
			drainAndCloseRelayClient(client, problems.Emit)
			return 0, nil
		case <-ctx.Done():
			drainAndCloseRelayClient(client, problems.Emit)
			stopChildSession(activeChild, a.debugf)
			return 0, ctx.Err()
		}
	}
}

func (a *App) openRawLogger() (*debuglog.RawLogger, error) {
	if !a.config.DebugRelayRaw {
		return nil, nil
	}
	return debuglog.OpenRaw(a.config.RawLogPath, "wrapper", a.config.InstanceID, os.Getpid())
}

func drainAndCloseRelayClient(client *relayws.Client, reportProblem func(agentproto.ErrorInfo)) {
	if client == nil {
		return
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.WaitForOutboundIdle(drainCtx); err != nil && reportProblem != nil {
		reportProblem(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:      "relay_drain_before_close_failed",
			Layer:     "wrapper",
			Stage:     "shutdown",
			Operation: "relay.ws",
			Message:   "wrapper 在关闭 relay client 前等待 outbox 排空时失败。",
			Retryable: true,
		}))
	}
	client.Close()
}
