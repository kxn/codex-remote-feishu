package wrapper

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type App struct {
	config     Config
	translator *codex.Translator
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
	Version              string
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

type problemReporter struct {
	mu      sync.Mutex
	client  *relayws.Client
	pending []agentproto.ErrorInfo
}

func (r *problemReporter) SetClient(client *relayws.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client = client
}

func (r *problemReporter) Emit(problem agentproto.ErrorInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, problem.Normalize())
	r.flushLocked()
}

func (r *problemReporter) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
}

func (r *problemReporter) flushLocked() {
	if r.client == nil || len(r.pending) == 0 {
		return
	}
	events := make([]agentproto.Event, 0, len(r.pending))
	for _, problem := range r.pending {
		events = append(events, agentproto.NewSystemErrorEvent(problem))
	}
	if err := r.client.SendEvents(events); err != nil {
		return
	}
	r.pending = nil
}

func LoadConfig(args []string) (Config, error) {
	loaded, err := config.LoadWrapperConfig()
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
	instanceID, err := generateInstanceID()
	if err != nil {
		return Config{}, err
	}
	shortName := filepath.Base(workspaceRoot)
	displayName := shortName
	if displayName == "." || displayName == "/" {
		displayName = workspaceRoot
	}
	paths, err := relayruntime.DefaultPaths()
	if err != nil {
		return Config{}, err
	}
	binaryIdentity, err := relayruntime.CurrentBinaryIdentity("dev")
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
		WorkspaceKey:         workspaceRoot,
		ShortName:            shortName,
		Version:              "dev",
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
			BuildFingerprint: a.config.BuildFingerprint,
			BinaryPath:       a.config.BinaryPath,
		},
		ConfigPath:           a.config.ConfigPath,
		Paths:                a.config.RuntimePaths,
		DaemonBinaryPath:     firstNonEmpty(a.config.DaemonBinaryPath, a.config.BinaryPath),
		DaemonUseSystemProxy: a.config.DaemonUseSystemProxy,
		CapturedProxyEnv:     a.config.ChildProxyEnv,
	})
	if err := manager.EnsureReady(ctx); err != nil {
		return 1, err
	}
	a.debugf("runtime ready: relay=%s instance=%s workspace=%s", a.config.RelayServerURL, a.config.InstanceID, a.config.WorkspaceRoot)

	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	cmd := exec.CommandContext(childCtx, a.config.CodexRealBinary, a.config.Args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Dir = a.config.WorkspaceRoot
	cmd.Env = childEnvWithProxy(a.config.ChildProxyEnv)

	childStdin, childStdout, childStderr, err := startChild(cmd)
	if err != nil {
		return 1, err
	}
	a.debugf("child started: binary=%s pid=%d cwd=%s", a.config.CodexRealBinary, cmd.Process.Pid, a.config.WorkspaceRoot)

	writeCh := make(chan []byte, 128)
	errCh := make(chan error, 8)
	problems := &problemReporter{}

	var client *relayws.Client
	connectedOnce := false
	client = relayws.NewClient(a.config.RelayServerURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID:       a.config.InstanceID,
			DisplayName:      a.config.DisplayName,
			WorkspaceRoot:    a.config.WorkspaceRoot,
			WorkspaceKey:     a.config.WorkspaceKey,
			ShortName:        a.config.ShortName,
			Version:          a.config.Version,
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
			for _, line := range outbound {
				select {
				case writeCh <- line:
					a.debugf("relay command queued for codex: command=%s frame=%s", command.CommandID, summarizeFrame(line))
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	})
	problems.SetClient(client)
	client.SetRawLogger(rawLogger)

	go func() {
		if err := runRelayClient(ctx, a.config.RelayServerURL, client, manager, func() bool { return connectedOnce }); err != nil && err != context.Canceled {
			errCh <- err
		}
	}()

	go writeLoop(ctx, childStdin, writeCh, errCh, a.debugf, rawLogger, problems.Emit)
	go stdinLoop(ctx, stdin, writeCh, a.translator, client, errCh, a.debugf, rawLogger, problems.Emit)
	go stdoutLoop(ctx, childStdout, stdout, writeCh, a.translator, client, errCh, a.debugf, rawLogger, problems.Emit)
	go streamCopy(childStderr, stderr, errCh)

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	stopChild := func() {
		childCancel()
		select {
		case <-waitErr:
		case <-time.After(2 * time.Second):
		}
	}

	select {
	case err := <-waitErr:
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
		stopChild()
		if err == nil || err == context.Canceled {
			return 0, nil
		}
		return 1, err
	case <-ctx.Done():
		client.Close()
		stopChild()
		return 0, ctx.Err()
	}
}

func (a *App) openRawLogger() (*debuglog.RawLogger, error) {
	if !a.config.DebugRelayRaw {
		return nil, nil
	}
	return debuglog.OpenRaw(a.config.RawLogPath, "wrapper", a.config.InstanceID, os.Getpid())
}

func runRelayClient(ctx context.Context, relayURL string, client *relayws.Client, manager *relayruntime.Manager, connectedOnce func() bool) error {
	backoff := 200 * time.Millisecond
	for {
		if !connectedOnce() {
			if err := manager.EnsureReady(ctx); err != nil {
				return err
			}
		}
		err := client.RunOnce(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		var fatal relayws.FatalError
		if errors.As(err, &fatal) {
			return err
		}
		if !connectedOnce() {
			log.Printf("relay bootstrap connection failed: url=%s err=%v", relayURL, err)
		} else {
			log.Printf("relay steady reconnect failed: url=%s err=%v", relayURL, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

func relayWelcomeSummary(welcome agentproto.Welcome) string {
	if welcome.Server == nil {
		return "legacy relay without server identity"
	}
	switch {
	case welcome.Server.BuildFingerprint != "":
		return welcome.Server.BuildFingerprint
	case welcome.Server.Version != "":
		return welcome.Server.Version
	default:
		return "unknown relay identity"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func startChild(cmd *exec.Cmd) (io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return stdin, stdout, stderr, nil
}

func stdinLoop(ctx context.Context, stdin io.Reader, writeCh chan<- []byte, translator *codex.Translator, client *relayws.Client, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) {
	reader := bufio.NewReader(stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			logRawFrame(rawLogger, "parent.stdin", "in", line, "", "")
			if debugf != nil {
				debugf("stdin from parent: %s", summarizeFrame(line))
			}
			if result, parseErr := translator.ObserveClient(line); parseErr == nil {
				if debugf != nil && (len(result.Events) > 0 || len(result.OutboundToCodex) > 0 || result.Suppress) {
					debugf("stdin observe result: events=%s followups=%d suppress=%t", summarizeEventKinds(result.Events), len(result.OutboundToCodex), result.Suppress)
				}
				if sendErr := client.SendEvents(result.Events); sendErr != nil {
					log.Printf("relay send client events failed: %v", sendErr)
					if reportProblem != nil {
						reportProblem(agentproto.ErrorInfoFromError(sendErr, agentproto.ErrorInfo{
							Code:      "relay_send_client_events_failed",
							Layer:     "wrapper",
							Stage:     "forward_client_events",
							Operation: "parent.stdin",
							Message:   "wrapper 无法把本地客户端事件发送到 relay。",
							Retryable: true,
						}))
					}
				}
			} else {
				if debugf != nil {
					debugf("stdin observe parse failed: err=%v preview=%q", parseErr, previewRawLine(line))
				}
				if reportProblem != nil {
					reportProblem(agentproto.ErrorInfo{
						Code:      "stdin_parse_failed",
						Layer:     "wrapper",
						Stage:     "observe_parent_stdin",
						Operation: "parent.stdin",
						Message:   "wrapper 无法解析上游传来的 JSON-RPC 帧。",
						Details:   fmt.Sprintf("%v; frame=%q", parseErr, previewRawLine(line)),
					})
				}
			}
			select {
			case writeCh <- line:
				if debugf != nil {
					debugf("stdin forwarded to codex: %s", summarizeFrame(line))
				}
			case <-ctx.Done():
				return
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return
		}
		errCh <- err
		return
	}
}

func stdoutLoop(ctx context.Context, childStdout io.Reader, parentStdout io.Writer, writeCh chan<- []byte, translator *codex.Translator, client *relayws.Client, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) {
	reader := bufio.NewReader(childStdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			logRawFrame(rawLogger, "codex.stdout", "in", line, "", "")
			if debugf != nil {
				debugf("stdout from codex: %s", summarizeFrame(line))
			}
			result, parseErr := translator.ObserveServer(line)
			if parseErr == nil {
				if debugf != nil {
					debugf(
						"stdout observe result: events=%s followups=%d frames=%s suppress=%t",
						summarizeEventKinds(result.Events),
						len(result.OutboundToCodex),
						summarizeFrames(result.OutboundToCodex),
						result.Suppress,
					)
				}
				if sendErr := client.SendEvents(result.Events); sendErr != nil {
					log.Printf("relay send server events failed: %v", sendErr)
					if reportProblem != nil {
						reportProblem(agentproto.ErrorInfoFromError(sendErr, agentproto.ErrorInfo{
							Code:      "relay_send_server_events_failed",
							Layer:     "wrapper",
							Stage:     "forward_server_events",
							Operation: "codex.stdout",
							Message:   "wrapper 无法把 Codex 事件发送到 relay。",
							Retryable: true,
						}))
					}
				}
				for _, followup := range result.OutboundToCodex {
					select {
					case writeCh <- followup:
						if debugf != nil {
							debugf("stdout queued followup to codex: %s", summarizeFrame(followup))
						}
					case <-ctx.Done():
						return
					}
				}
				if !result.Suppress {
					if _, writeErr := parentStdout.Write(line); writeErr != nil {
						if reportProblem != nil {
							reportProblem(agentproto.ErrorInfoFromError(writeErr, agentproto.ErrorInfo{
								Code:      "write_parent_stdout_failed",
								Layer:     "wrapper",
								Stage:     "write_parent_stdout",
								Operation: "parent.stdout",
								Message:   "wrapper 无法把 Codex 输出回传给上游客户端。",
							}))
						}
						errCh <- writeErr
						return
					}
				}
			} else {
				if debugf != nil {
					debugf("stdout observe parse failed: err=%v preview=%q", parseErr, previewRawLine(line))
				}
				if reportProblem != nil {
					reportProblem(agentproto.ErrorInfo{
						Code:      "stdout_parse_failed",
						Layer:     "wrapper",
						Stage:     "observe_codex_stdout",
						Operation: "codex.stdout",
						Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
						Details:   fmt.Sprintf("%v; frame=%q", parseErr, previewRawLine(line)),
					})
				}
				if _, writeErr := parentStdout.Write(line); writeErr != nil {
					if reportProblem != nil {
						reportProblem(agentproto.ErrorInfoFromError(writeErr, agentproto.ErrorInfo{
							Code:      "write_parent_stdout_failed",
							Layer:     "wrapper",
							Stage:     "write_parent_stdout",
							Operation: "parent.stdout",
							Message:   "wrapper 无法把 Codex 输出回传给上游客户端。",
						}))
					}
					errCh <- writeErr
					return
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return
		}
		errCh <- err
		return
	}
}

func writeLoop(ctx context.Context, childStdin io.WriteCloser, writeCh <-chan []byte, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) {
	defer childStdin.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-writeCh:
			if len(line) == 0 {
				continue
			}
			if debugf != nil {
				debugf("write to codex: %s", summarizeFrame(line))
			}
			logRawFrame(rawLogger, "codex.stdin", "out", line, "", "")
			if _, err := childStdin.Write(line); err != nil {
				if reportProblem != nil {
					reportProblem(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
						Code:      "write_codex_stdin_failed",
						Layer:     "wrapper",
						Stage:     "write_codex_stdin",
						Operation: "codex.stdin",
						Message:   "wrapper 无法继续向 Codex 子进程写入数据。",
					}))
				}
				errCh <- err
				return
			}
		}
	}
}

func logRawFrame(rawLogger *debuglog.RawLogger, channel, direction string, payload []byte, envelopeType, commandID string) {
	if rawLogger == nil {
		return
	}
	rawLogger.Log(debuglog.RawEntry{
		Channel:      channel,
		Direction:    direction,
		EnvelopeType: envelopeType,
		CommandID:    commandID,
		Frame:        payload,
	})
}

func streamCopy(src io.Reader, dst io.Writer, errCh chan<- error) {
	if _, err := io.Copy(dst, src); err != nil && !strings.Contains(err.Error(), "file already closed") {
		errCh <- err
	}
}

func summarizeFrames(lines [][]byte) string {
	if len(lines) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, summarizeFrame(line))
	}
	return strings.Join(parts, "; ")
}

func summarizeEventKinds(events []agentproto.Event) string {
	if len(events) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(events))
	for _, event := range events {
		part := string(event.Kind)
		if event.ThreadID != "" {
			part += " thread=" + event.ThreadID
		}
		if event.TurnID != "" {
			part += " turn=" + event.TurnID
		}
		if event.Initiator.Kind != "" {
			part += " initiator=" + string(event.Initiator.Kind)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func summarizeFrame(line []byte) string {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Sprintf("raw=%q", previewRawLine(line))
	}
	parts := []string{}
	if id := lookupStringFromMap(message, "id"); id != "" {
		parts = append(parts, "id="+id)
	}
	if method := lookupStringFromMap(message, "method"); method != "" {
		parts = append(parts, "method="+method)
	}
	if threadID := firstNonEmpty(
		lookupNestedString(message, "params", "threadId"),
		lookupNestedString(message, "params", "thread", "id"),
		lookupNestedString(message, "result", "thread", "id"),
	); threadID != "" {
		parts = append(parts, "thread="+threadID)
	}
	if turnID := firstNonEmpty(
		lookupNestedString(message, "params", "turnId"),
		lookupNestedString(message, "params", "turn", "id"),
		lookupNestedString(message, "result", "turn", "id"),
	); turnID != "" {
		parts = append(parts, "turn="+turnID)
	}
	if cwd := lookupNestedString(message, "params", "cwd"); cwd != "" {
		parts = append(parts, "cwd="+cwd)
	}
	if inputs := countJSONArray(message, "params", "input"); inputs > 0 {
		parts = append(parts, fmt.Sprintf("inputs=%d", inputs))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("raw=%q", previewRawLine(line))
	}
	return strings.Join(parts, " ")
}

func lookupStringFromMap(message map[string]any, key string) string {
	value, ok := message[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func lookupNestedString(message map[string]any, path ...string) string {
	var current any = message
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = asMap[key]
		if !ok {
			return ""
		}
	}
	switch value := current.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func countJSONArray(message map[string]any, path ...string) int {
	var current any = message
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current, ok = asMap[key]
		if !ok {
			return 0
		}
	}
	values, ok := current.([]any)
	if !ok {
		return 0
	}
	return len(values)
}

func previewRawLine(line []byte) string {
	text := strings.TrimSpace(string(line))
	if len(text) > 200 {
		return text[:200] + "..."
	}
	return text
}

func childEnvWithProxy(proxyEnv []string) []string {
	filtered := config.FilterEnvWithoutProxy(os.Environ())
	filtered = append(filtered, proxyEnv...)
	return filtered
}

func generateInstanceID() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("inst-%s", hex.EncodeToString(bytes[:])), nil
}
