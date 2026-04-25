package wrapper

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type App struct {
	config     Config
	translator *codex.Translator
}

const (
	steerCommandResponseTimeout       = 5 * time.Second
	wrapperChildRestoreTimeout        = 5 * time.Second
	wrapperChildStopGrace             = 2 * time.Second
	wrapperChildWaitTimeout           = 5 * time.Second
	wrapperStartupRefreshBorrowWindow = 300 * time.Millisecond
)

type shutdownRequest struct {
	CommandID string
}

type restartRequest struct {
	CommandID string
	ResultCh  chan error
}

type Config struct {
	RelayServerURL  string
	CodexRealBinary string
	NameMode        string
	Args            []string
	ConfigPath      string

	InstanceID           string
	DisplayName          string
	WorkspaceRoot        string
	WorkspaceKey         string
	ShortName            string
	Source               string
	Managed              bool
	Lifetime             string
	ParentPID            int
	Version              string
	Branch               string
	BuildFingerprint     string
	BinaryPath           string
	ChildProxyEnv        []string
	DaemonBinaryPath     string
	DaemonUseSystemProxy bool
	RuntimePaths         relayruntime.Paths
	DebugRelayFlow       bool
	DebugRelayRaw        bool
	RawLogPath           string
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
		RelayServerURL:       loaded.RelayServerURL,
		CodexRealBinary:      loaded.CodexRealBinary,
		NameMode:             loaded.NameMode,
		Args:                 args,
		ConfigPath:           firstNonEmpty(services.ConfigPath, loaded.ConfigPath, paths.ConfigFile),
		InstanceID:           instanceID,
		DisplayName:          displayName,
		WorkspaceRoot:        workspaceRoot,
		WorkspaceKey:         state.ResolveWorkspaceKey(workspaceRoot),
		ShortName:            shortName,
		Source:               source,
		Managed:              managed,
		Lifetime:             string(lifetime),
		ParentPID:            parentPID,
		Version:              firstNonEmpty(strings.TrimSpace(version), "dev"),
		Branch:               firstNonEmpty(strings.TrimSpace(branch), "dev"),
		BuildFingerprint:     binaryIdentity.BuildFingerprint,
		BinaryPath:           binaryIdentity.BinaryPath,
		ChildProxyEnv:        config.CaptureAndClearProxyEnv(),
		DaemonBinaryPath:     binaryIdentity.BinaryPath,
		DaemonUseSystemProxy: services.FeishuUseSystemProxy,
		RuntimePaths:         paths,
		DebugRelayFlow:       loaded.DebugRelayFlow || services.DebugRelayFlow,
		DebugRelayRaw:        loaded.DebugRelayRaw || services.DebugRelayRaw,
		RawLogPath:           relayruntime.WrapperRawLogFile(paths.LogsDir, os.Getpid()),
	}, nil
}

func New(cfg Config) *App {
	translator := codex.NewTranslator(cfg.InstanceID)
	if cfg.DebugRelayFlow {
		translator.SetDebugLogger(func(format string, args ...any) {
			log.Printf("relay flow translator: "+format, args...)
		})
	}
	return &App{
		config:     cfg,
		translator: translator,
	}
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
	connectedOnce := false
	startupThreadsRefreshHandled := false
	client = relayws.NewClient(a.config.RelayServerURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID:       a.config.InstanceID,
			DisplayName:      a.config.DisplayName,
			WorkspaceRoot:    a.config.WorkspaceRoot,
			WorkspaceKey:     a.config.WorkspaceKey,
			ShortName:        a.config.ShortName,
			Source:           a.config.Source,
			Managed:          a.config.Managed,
			Version:          a.config.Version,
			Branch:           a.config.Branch,
			BuildFingerprint: a.config.BuildFingerprint,
			BinaryPath:       a.config.BinaryPath,
			PID:              os.Getpid(),
		},
		Capabilities: agentproto.Capabilities{ThreadsRefresh: true},
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
				request := restartRequest{
					CommandID: command.CommandID,
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
			if command.Kind == agentproto.CommandThreadsRefresh &&
				!startupThreadsRefreshHandled &&
				strings.EqualFold(firstNonEmpty(a.config.Source, "vscode"), "vscode") {
				startupThreadsRefreshHandled = true
				a.translator.ArmStartupThreadListBorrow()
				a.debugf(
					"relay command startup refresh borrow window opened: command=%s window=%s",
					command.CommandID,
					wrapperStartupRefreshBorrowWindow,
				)
				timer := time.NewTimer(wrapperStartupRefreshBorrowWindow)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
			outbound, err := a.translator.TranslateCommand(command)
			if err != nil {
				a.debugf("relay command translation failed: command=%s err=%v", command.CommandID, err)
				return agentproto.ErrorInfo{
					Code:             "translate_command_failed",
					Layer:            "wrapper",
					Stage:            "translate_command",
					Operation:        string(command.Kind),
					Message:          "wrapper 无法把 relay 命令转换成 Codex 请求。",
					Details:          err.Error(),
					SurfaceSessionID: command.Origin.Surface,
					CommandID:        command.CommandID,
					ThreadID:         command.Target.ThreadID,
					TurnID:           command.Target.TurnID,
				}
			}
			a.debugf("relay command translated: command=%s outbound=%d frames=%s", command.CommandID, len(outbound), summarizeFrames(outbound))
			var waitCh <-chan *agentproto.ErrorInfo
			requestID := ""
			if command.Kind == agentproto.CommandTurnSteer && len(outbound) > 0 {
				requestID = lookupStringFromRawFrame(outbound[0], "id")
				if strings.TrimSpace(requestID) == "" {
					return agentproto.ErrorInfo{
						Code:             "missing_command_request_id",
						Layer:            "wrapper",
						Stage:            "translate_command",
						Operation:        string(command.Kind),
						Message:          "wrapper 生成追加输入请求时缺少 request id。",
						SurfaceSessionID: command.Origin.Surface,
						CommandID:        command.CommandID,
						ThreadID:         command.Target.ThreadID,
						TurnID:           command.Target.TurnID,
					}
				}
				waitCh = commandResponses.Register(requestID, agentproto.ErrorInfo{
					Code:             "steer_rejected",
					Layer:            "wrapper",
					Stage:            "command_response",
					Operation:        string(command.Kind),
					Message:          "本地 Codex 拒绝了这次追加输入。",
					SurfaceSessionID: command.Origin.Surface,
					CommandID:        command.CommandID,
					ThreadID:         command.Target.ThreadID,
					TurnID:           command.Target.TurnID,
				}, true)
			}
			for _, line := range outbound {
				select {
				case writeCh <- line:
					a.debugf("relay command queued for codex: command=%s frame=%s", command.CommandID, summarizeFrame(line))
				case <-ctx.Done():
					commandResponses.Cancel(requestID)
					return ctx.Err()
				}
			}
			if command.Kind == agentproto.CommandTurnSteer {
				err := waitCommandResponse(ctx, waitCh, steerCommandResponseTimeout, agentproto.ErrorInfo{
					Code:             "steer_response_timeout",
					Layer:            "wrapper",
					Stage:            "command_response",
					Operation:        string(command.Kind),
					Message:          "等待本地 Codex 确认追加输入时超时。",
					SurfaceSessionID: command.Origin.Surface,
					CommandID:        command.CommandID,
					ThreadID:         command.Target.ThreadID,
					TurnID:           command.Target.TurnID,
				})
				if err != nil {
					commandResponses.Cancel(requestID)
					a.debugf("relay command response failed: command=%s request=%s err=%v", command.CommandID, requestID, err)
					return err
				}
				a.debugf("relay command response accepted: command=%s request=%s", command.CommandID, requestID)
			}
			return nil
		},
	})
	problems.SetClient(client)
	client.SetRawLogger(rawLogger)

	activeChild, err := a.launchChildSession(ctx, rawLogger, problems.Emit)
	if err != nil {
		return 1, err
	}
	startChildSessionIO(ctx, activeChild, stdout, stderr, writeCh, a.translator, client, commandResponses, errCh, a.debugf, rawLogger, problems.Emit)

	go func() {
		if err := runRelayClient(ctx, a.config.RelayServerURL, client, manager, func() bool { return connectedOnce }); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	go stdinLoop(ctx, stdin, writeCh, a.translator, client, errCh, a.debugf, rawLogger, problems.Emit)

	for {
		var waitErrCh <-chan error
		if activeChild != nil {
			waitErrCh = activeChild.waitErr
		}
		select {
		case err := <-waitErrCh:
			client.Close()
			if err == nil {
				return 0, nil
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode(), nil
			}
			return 1, err
		case err := <-errCh:
			client.Close()
			stopChildSession(activeChild, a.debugf)
			if err == nil || err == context.Canceled {
				return 0, nil
			}
			return 1, err
		case request := <-shutdownCh:
			a.debugf("wrapper shutdown requested by daemon: command=%s", request.CommandID)
			stopChildSession(activeChild, a.debugf)
			client.Close()
			return 0, nil
		case request := <-restartCh:
			a.debugf("wrapper child restart requested by daemon: command=%s", request.CommandID)
			nextChild, err := a.restartChildSession(ctx, activeChild, stdout, stderr, writeCh, client, commandResponses, errCh, rawLogger, problems.Emit)
			if nextChild != nil {
				activeChild = nextChild
			}
			request.ResultCh <- err
			close(request.ResultCh)
		case <-hostExitCh:
			a.debugf("wrapper shutdown requested by host exit: lifetime=%s parentPid=%d", a.config.Lifetime, a.config.ParentPID)
			stopChildSession(activeChild, a.debugf)
			client.Close()
			return 0, nil
		case <-ctx.Done():
			client.Close()
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
