package wrapper

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

type runtimeObserveResult struct {
	Events           []agentproto.Event
	OutboundToChild  [][]byte
	OutboundToParent [][]byte
	Suppress         bool
}

type backendRuntime interface {
	Backend() agentproto.Backend
	Capabilities() agentproto.Capabilities
	Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error)
	ObserveClient([]byte) (runtimeObserveResult, error)
	ObserveServer([]byte) (runtimeObserveResult, error)
	TranslateCommand(agentproto.Command) ([][]byte, error)
	BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error)
	CancelChildRestartRestore(string)
}

type runtimeDebugLogger interface {
	SetDebugLogger(func(string, ...any))
}

func newBackendRuntime(cfg Config) backendRuntime {
	switch agentproto.NormalizeBackend(cfg.Backend) {
	case agentproto.BackendClaude:
		return &claudeSkeletonRuntime{}
	default:
		return &codexBackendRuntime{translator: codex.NewTranslator(cfg.InstanceID)}
	}
}

type codexBackendRuntime struct {
	translator *codex.Translator
}

func (r *codexBackendRuntime) Backend() agentproto.Backend {
	return agentproto.BackendCodex
}

func (r *codexBackendRuntime) Capabilities() agentproto.Capabilities {
	return agentproto.DefaultCapabilitiesForBackend(agentproto.BackendCodex)
}

func (r *codexBackendRuntime) Launch(ctx context.Context, app *App, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (*childSession, error) {
	if app == nil {
		return nil, nil
	}
	return app.launchCodexChildSession(ctx, rawLogger, reportProblem)
}

func (r *codexBackendRuntime) ObserveClient(line []byte) (runtimeObserveResult, error) {
	result, err := r.translator.ObserveClient(line)
	if err != nil {
		return runtimeObserveResult{}, err
	}
	return runtimeObserveResult{
		Events:          result.Events,
		OutboundToChild: result.OutboundToCodex,
		Suppress:        result.Suppress,
	}, nil
}

func (r *codexBackendRuntime) ObserveServer(line []byte) (runtimeObserveResult, error) {
	result, err := r.translator.ObserveServer(line)
	if err != nil {
		return runtimeObserveResult{}, err
	}
	return runtimeObserveResult{
		Events:           result.Events,
		OutboundToChild:  result.OutboundToCodex,
		OutboundToParent: result.OutboundToParent,
		Suppress:         result.Suppress,
	}, nil
}

func (r *codexBackendRuntime) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	return r.translator.TranslateCommand(command)
}

func (r *codexBackendRuntime) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	return r.translator.BuildChildRestartRestoreFrame(commandID)
}

func (r *codexBackendRuntime) CancelChildRestartRestore(requestID string) {
	r.translator.CancelChildRestartRestore(requestID)
}

func (r *codexBackendRuntime) SetDebugLogger(debugLog func(string, ...any)) {
	r.translator.SetDebugLogger(debugLog)
}

type claudeSkeletonRuntime struct{}

func (r *claudeSkeletonRuntime) Backend() agentproto.Backend {
	return agentproto.BackendClaude
}

func (r *claudeSkeletonRuntime) Capabilities() agentproto.Capabilities {
	return agentproto.Capabilities{}
}

func (r *claudeSkeletonRuntime) Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error) {
	return nil, nil
}

func (r *claudeSkeletonRuntime) ObserveClient([]byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{}, nil
}

func (r *claudeSkeletonRuntime) ObserveServer([]byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{}, nil
}

func (r *claudeSkeletonRuntime) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	return nil, agentproto.ErrorInfo{
		Code:             "claude_runtime_dark_launch_only",
		Layer:            "wrapper",
		Stage:            "translate_command",
		Operation:        string(command.Kind),
		Message:          "Claude runtime skeleton 仅用于 dark-launch seam，当前还不接受命令执行。",
		SurfaceSessionID: command.Origin.Surface,
		CommandID:        command.CommandID,
		ThreadID:         command.Target.ThreadID,
		TurnID:           command.Target.TurnID,
	}
}

func (r *claudeSkeletonRuntime) BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error) {
	return nil, "", false, nil
}

func (r *claudeSkeletonRuntime) CancelChildRestartRestore(string) {}
