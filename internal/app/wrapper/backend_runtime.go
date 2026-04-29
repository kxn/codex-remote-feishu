package wrapper

import (
	"context"
	"path/filepath"
	"strings"

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

type runtimeCommandResult struct {
	Events          []agentproto.Event
	OutboundToChild [][]byte
	Restart         *runtimeCommandRestart
}

type runtimeCommandRestart struct {
	Target agentproto.Target
}

type backendRuntime interface {
	Backend() agentproto.Backend
	Capabilities() agentproto.Capabilities
	Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error)
	ObserveClient([]byte) (runtimeObserveResult, error)
	ObserveServer([]byte) (runtimeObserveResult, error)
	TranslateCommand(agentproto.Command) (runtimeCommandResult, error)
	PrepareChildRestart(string, agentproto.Target) error
	BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error)
	CancelChildRestartRestore(string)
}

type runtimeDebugLogger interface {
	SetDebugLogger(func(string, ...any))
}

func newBackendRuntime(cfg Config) backendRuntime {
	switch agentproto.NormalizeBackend(cfg.Backend) {
	case agentproto.BackendClaude:
		runtime := &claudeBackendRuntime{
			translator:    claude.NewTranslator(cfg.InstanceID),
			workspaceRoot: cfg.WorkspaceRoot,
		}
		if threadID := strings.TrimSpace(cfg.ResumeThreadID); threadID != "" {
			runtime.initialLaunchResume = &claudeLaunchResumeTarget{
				ThreadID: threadID,
				CWD:      strings.TrimSpace(cfg.WorkspaceRoot),
			}
		}
		return runtime
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

func (r *codexBackendRuntime) TranslateCommand(command agentproto.Command) (runtimeCommandResult, error) {
	outbound, err := r.translator.TranslateCommand(command)
	if err != nil {
		return runtimeCommandResult{}, err
	}
	return runtimeCommandResult{OutboundToChild: outbound}, nil
}

func (r *codexBackendRuntime) PrepareChildRestart(string, agentproto.Target) error {
	return nil
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
	translator           *claude.Translator
	workspaceRoot        string
	initialLaunchResume  *claudeLaunchResumeTarget
	pendingLaunchResume  *claudeLaunchResumeTarget
	expectedResumeThread *claudeLaunchResumeTarget
}

type claudeLaunchResumeTarget struct {
	ThreadID string
	CWD      string
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
	resume := r.consumeLaunchResumeTarget()
	session, err := app.launchClaudeChildSession(ctx, rawLogger, reportProblem, resume)
	if err != nil {
		return nil, err
	}
	if resume != nil {
		copy := *resume
		r.expectedResumeThread = &copy
	}
	return session, nil
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
	if r.expectedResumeThread != nil {
		if sessionID := strings.TrimSpace(r.translator.RuntimeStateSnapshot().SessionID); sessionID != "" {
			r.expectedResumeThread = nil
		}
	}
	return runtimeObserveResult{
		Events:           result.Events,
		OutboundToChild:  result.OutboundToClaude,
		OutboundToParent: result.OutboundToParent,
		Suppress:         result.Suppress,
	}, nil
}

func (r *claudeBackendRuntime) TranslateCommand(command agentproto.Command) (runtimeCommandResult, error) {
	if events, handled, err := claude.HandleLocalCommand(command, r.workspaceRoot, r.translator.RuntimeStateSnapshot()); handled {
		if err != nil {
			return runtimeCommandResult{}, err
		}
		return runtimeCommandResult{Events: events}, nil
	}
	if plan, err := r.restartPlanForCommand(command); err != nil {
		return runtimeCommandResult{}, err
	} else if plan != nil {
		return runtimeCommandResult{Restart: plan}, nil
	}
	outbound, err := r.translator.TranslateCommand(command)
	if err != nil {
		return runtimeCommandResult{}, err
	}
	return runtimeCommandResult{OutboundToChild: outbound}, nil
}

func (r *claudeBackendRuntime) PrepareChildRestart(_ string, target agentproto.Target) error {
	resume, err := r.resolveLaunchResumeTarget(target)
	if err != nil {
		return err
	}
	r.pendingLaunchResume = resume
	return nil
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

func (r *claudeBackendRuntime) restartPlanForCommand(command agentproto.Command) (*runtimeCommandRestart, error) {
	if command.Kind != agentproto.CommandPromptSend {
		return nil, nil
	}
	targetThreadID := strings.TrimSpace(command.Target.ThreadID)
	if targetThreadID == "" {
		return nil, nil
	}
	current := r.currentResumeTarget()
	if current != nil && strings.EqualFold(strings.TrimSpace(current.ThreadID), targetThreadID) {
		return nil, nil
	}
	resume, found, err := r.lookupStoredResumeTarget(command.Target)
	if err != nil {
		return nil, err
	}
	if !found || resume == nil {
		return nil, nil
	}
	target := command.Target
	target.ThreadID = resume.ThreadID
	if strings.TrimSpace(target.CWD) == "" {
		target.CWD = resume.CWD
	}
	return &runtimeCommandRestart{Target: target}, nil
}

func (r *claudeBackendRuntime) resolveLaunchResumeTarget(target agentproto.Target) (*claudeLaunchResumeTarget, error) {
	threadID := strings.TrimSpace(target.ThreadID)
	cwd := strings.TrimSpace(target.CWD)
	if threadID == "" {
		current := r.currentResumeTarget()
		if current == nil || strings.TrimSpace(current.ThreadID) == "" {
			return nil, nil
		}
		copy := *current
		return &copy, nil
	}
	resume, found, err := r.lookupStoredResumeTarget(target)
	if err != nil {
		return nil, err
	}
	if !found || resume == nil {
		return nil, agentproto.ErrorInfo{
			Code:      "claude_resume_thread_not_found",
			Layer:     "wrapper",
			Stage:     "prepare_child_restart",
			Operation: string(agentproto.CommandPromptSend),
			Message:   "目标 Claude 会话当前不可恢复，当前不能直接切回这个会话。",
			ThreadID:  threadID,
		}
	}
	if cwd == "" {
		cwd = resume.CWD
	}
	return &claudeLaunchResumeTarget{
		ThreadID: threadID,
		CWD:      cwd,
	}, nil
}

func (r *claudeBackendRuntime) lookupStoredResumeTarget(target agentproto.Target) (*claudeLaunchResumeTarget, bool, error) {
	threadID := strings.TrimSpace(target.ThreadID)
	if threadID == "" {
		return nil, false, nil
	}
	cwd := strings.TrimSpace(target.CWD)
	if meta, err := claude.ResolveResumeSession(r.workspaceRoot, threadID); err != nil {
		return nil, false, agentproto.ErrorInfo{
			Code:      "claude_resume_workspace_mismatch",
			Layer:     "wrapper",
			Stage:     "prepare_child_restart",
			Operation: string(agentproto.CommandPromptSend),
			Message:   "目标 Claude 会话不属于当前工作区，当前不能直接恢复到这个会话。",
			Details:   err.Error(),
			ThreadID:  threadID,
		}
	} else if meta == nil {
		return nil, false, nil
	} else if meta != nil && strings.TrimSpace(meta.CWD) != "" {
		cwd = strings.TrimSpace(meta.CWD)
	}
	if cwd == "" {
		cwd = strings.TrimSpace(r.workspaceRoot)
	}
	if strings.TrimSpace(cwd) != "" && strings.TrimSpace(r.workspaceRoot) != "" &&
		filepath.Clean(cwd) != filepath.Clean(r.workspaceRoot) {
		return nil, false, agentproto.ErrorInfo{
			Code:      "claude_resume_workspace_mismatch",
			Layer:     "wrapper",
			Stage:     "prepare_child_restart",
			Operation: string(agentproto.CommandPromptSend),
			Message:   "目标 Claude 会话不属于当前工作区，当前不能直接恢复到这个会话。",
			Details:   "target cwd does not match wrapper workspace root",
			ThreadID:  threadID,
		}
	}
	return &claudeLaunchResumeTarget{
		ThreadID: threadID,
		CWD:      cwd,
	}, true, nil
}

func (r *claudeBackendRuntime) consumeLaunchResumeTarget() *claudeLaunchResumeTarget {
	if r == nil {
		return nil
	}
	if r.pendingLaunchResume != nil {
		resume := r.pendingLaunchResume
		r.pendingLaunchResume = nil
		return resume
	}
	if r.initialLaunchResume != nil {
		resume := r.initialLaunchResume
		r.initialLaunchResume = nil
		return resume
	}
	return nil
}

func (r *claudeBackendRuntime) currentResumeTarget() *claudeLaunchResumeTarget {
	if r == nil {
		return nil
	}
	if r.expectedResumeThread != nil {
		copy := *r.expectedResumeThread
		return &copy
	}
	runtime := r.translator.RuntimeStateSnapshot()
	if strings.TrimSpace(runtime.SessionID) == "" {
		return nil
	}
	return &claudeLaunchResumeTarget{
		ThreadID: strings.TrimSpace(runtime.SessionID),
		CWD:      strings.TrimSpace(runtime.CWD),
	}
}
