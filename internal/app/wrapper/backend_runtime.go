package wrapper

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
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
		return &claudeBackendRuntime{translator: claude.NewTranslator(cfg.InstanceID)}
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

type claudeBackendRuntime struct {
	translator *claude.Translator
}

func (r *claudeBackendRuntime) Backend() agentproto.Backend {
	return agentproto.BackendClaude
}

func (r *claudeBackendRuntime) Capabilities() agentproto.Capabilities {
	return agentproto.DefaultCapabilitiesForBackend(agentproto.BackendClaude)
}

func (r *claudeBackendRuntime) Launch(ctx context.Context, app *App, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (*childSession, error) {
	if app == nil {
		return nil, nil
	}
	return app.launchClaudeChildSession(ctx, rawLogger, reportProblem)
}

func (r *claudeBackendRuntime) ObserveClient(line []byte) (runtimeObserveResult, error) {
	result, err := r.translator.ObserveClient(line)
	if err != nil {
		return runtimeObserveResult{}, err
	}
	return runtimeObserveResult{
		Events:          result.Events,
		OutboundToChild: result.OutboundToClaude,
		Suppress:        result.Suppress,
	}, nil
}

func (r *claudeBackendRuntime) ObserveServer(line []byte) (runtimeObserveResult, error) {
	result, err := r.translator.ObserveServer(line)
	if err != nil {
		return runtimeObserveResult{}, err
	}
	return runtimeObserveResult{
		Events:           result.Events,
		OutboundToChild:  result.OutboundToClaude,
		OutboundToParent: result.OutboundToParent,
		Suppress:         result.Suppress,
	}, nil
}

func (r *claudeBackendRuntime) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	return r.translator.TranslateCommand(command)
}

func (r *claudeBackendRuntime) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	return r.translator.BuildChildRestartRestoreFrame(commandID)
}

func (r *claudeBackendRuntime) CancelChildRestartRestore(requestID string) {
	r.translator.CancelChildRestartRestore(requestID)
}

func (r *claudeBackendRuntime) SetDebugLogger(debugLog func(string, ...any)) {
	r.translator.SetDebugLogger(debugLog)
}
