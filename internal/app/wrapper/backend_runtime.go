package wrapper

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

type runtimeObserveResult struct {
	Events                   []agentproto.Event
	OutboundToChild          [][]byte
	OutboundToParent         [][]byte
	ResolvedCommandResponses []runtimeResolvedCommandResponse
	Suppress                 bool
}

type runtimeResolvedCommandResponse struct {
	RequestID     string
	RejectMessage string
}

type runtimeCommandPhase struct {
	OutboundToChild [][]byte
	ResponseGate    *runtimeCommandResponseGate
}

type runtimeCommandResult struct {
	Events  []agentproto.Event
	Phases  []runtimeCommandPhase
	Restart *runtimeCommandRestart
}

type runtimeCommandRestart struct {
	Target agentproto.Target
}

type runtimeCommandResponseGate struct {
	RequestID      string
	RejectProblem  agentproto.ErrorInfo
	Timeout        time.Duration
	TimeoutProblem agentproto.ErrorInfo
	SuppressFrame  bool
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
	mu         sync.Mutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	outbound, err := r.translator.TranslateCommand(command)
	if err != nil {
		return runtimeCommandResult{}, err
	}
	result := runtimeCommandResult{Phases: singleRuntimeCommandPhases(outbound)}
	if command.Kind == agentproto.CommandTurnSteer && len(result.Phases) > 0 {
		gate, err := newTurnSteerResponseGate(command, outbound[0])
		if err != nil {
			return runtimeCommandResult{}, err
		}
		result.Phases[0].ResponseGate = gate
	}
	return result, nil
}

func (r *codexBackendRuntime) PrepareChildRestart(string, agentproto.Target) error {
	return nil
}

func (r *codexBackendRuntime) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.translator.BuildChildRestartRestoreFrame(commandID)
}

func (r *codexBackendRuntime) CancelChildRestartRestore(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translator.CancelChildRestartRestore(requestID)
}

func (r *codexBackendRuntime) SetDebugLogger(debugLog func(string, ...any)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translator.SetDebugLogger(debugLog)
}

type claudeBackendRuntime struct {
	mu                   sync.Mutex
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
	r.mu.Lock()
	resume := r.consumeLaunchResumeTarget()
	resumeThreadID := ""
	if resume != nil {
		resumeThreadID = strings.TrimSpace(resume.ThreadID)
	}
	r.translator.PrepareForChildLaunch(resumeThreadID)
	r.mu.Unlock()
	session, err := app.launchClaudeChildSession(ctx, rawLogger, reportProblem, resume)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if resume != nil {
		copy := *resume
		r.expectedResumeThread = &copy
	} else {
		r.expectedResumeThread = nil
	}
	r.mu.Unlock()
	return session, nil
}

func (r *claudeBackendRuntime) ObserveClient(line []byte) (runtimeObserveResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
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
		Events:                   result.Events,
		OutboundToChild:          result.OutboundToClaude,
		OutboundToParent:         result.OutboundToParent,
		ResolvedCommandResponses: mapClaudeResolvedCommandResponses(result.ResolvedCommandResponses),
		Suppress:                 result.Suppress,
	}, nil
}

func (r *claudeBackendRuntime) TranslateCommand(command agentproto.Command) (runtimeCommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	phases, err := newClaudeCommandPhases(command, outbound)
	if err != nil {
		return runtimeCommandResult{}, err
	}
	return runtimeCommandResult{Phases: phases}, nil
}

func singleRuntimeCommandPhases(outbound [][]byte) []runtimeCommandPhase {
	if len(outbound) == 0 {
		return nil
	}
	return []runtimeCommandPhase{{
		OutboundToChild: append([][]byte(nil), outbound...),
	}}
}

func mapClaudeResolvedCommandResponses(responses []claude.ResolvedCommandResponse) []runtimeResolvedCommandResponse {
	if len(responses) == 0 {
		return nil
	}
	mapped := make([]runtimeResolvedCommandResponse, 0, len(responses))
	for _, response := range responses {
		requestID := strings.TrimSpace(response.RequestID)
		if requestID == "" {
			continue
		}
		mapped = append(mapped, runtimeResolvedCommandResponse{
			RequestID:     requestID,
			RejectMessage: strings.TrimSpace(response.RejectMessage),
		})
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
}

func newClaudeCommandPhases(command agentproto.Command, outbound [][]byte) ([]runtimeCommandPhase, error) {
	phases := singleRuntimeCommandPhases(outbound)
	if command.Kind != agentproto.CommandPromptSend || len(outbound) < 2 {
		return phases, nil
	}
	gate, ok, err := newClaudePermissionModeResponseGate(command, outbound[0])
	if err != nil {
		return nil, err
	}
	if !ok {
		return phases, nil
	}
	phases = []runtimeCommandPhase{{
		OutboundToChild: [][]byte{outbound[0]},
		ResponseGate:    gate,
	}}
	if len(outbound) > 1 {
		phases = append(phases, runtimeCommandPhase{
			OutboundToChild: append([][]byte(nil), outbound[1:]...),
		})
	}
	return phases, nil
}

func newClaudePermissionModeResponseGate(command agentproto.Command, frame []byte) (*runtimeCommandResponseGate, bool, error) {
	var message map[string]any
	if err := json.Unmarshal(frame, &message); err != nil {
		return nil, false, agentproto.ErrorInfo{
			Code:             "invalid_claude_permission_frame",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "wrapper 无法解析 Claude 权限切换请求。",
			Details:          err.Error(),
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
	if strings.TrimSpace(lookupStringFromMap(message, "type")) != "control_request" {
		return nil, false, nil
	}
	request, _ := message["request"].(map[string]any)
	if strings.TrimSpace(lookupStringFromMap(request, "subtype")) != "set_permission_mode" {
		return nil, false, nil
	}
	requestID := strings.TrimSpace(lookupStringFromMap(message, "request_id"))
	if requestID == "" {
		return nil, false, agentproto.ErrorInfo{
			Code:             "missing_command_request_id",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "wrapper 生成 Claude 权限切换请求时缺少 request id。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
	return &runtimeCommandResponseGate{
		RequestID: requestID,
		RejectProblem: agentproto.ErrorInfo{
			Code:             "claude_permission_mode_rejected",
			Layer:            "wrapper",
			Stage:            "command_response",
			Operation:        string(command.Kind),
			Message:          "本地 Claude 拒绝了这次权限或 Plan 模式切换。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		},
		Timeout: steerCommandResponseTimeout,
		TimeoutProblem: agentproto.ErrorInfo{
			Code:             "claude_permission_mode_response_timeout",
			Layer:            "wrapper",
			Stage:            "command_response",
			Operation:        string(command.Kind),
			Message:          "等待本地 Claude 确认权限或 Plan 模式切换时超时。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		},
		SuppressFrame: true,
	}, true, nil
}

func newTurnSteerResponseGate(command agentproto.Command, frame []byte) (*runtimeCommandResponseGate, error) {
	requestID := lookupStringFromRawFrame(frame, "id")
	if strings.TrimSpace(requestID) == "" {
		return nil, agentproto.ErrorInfo{
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
	return &runtimeCommandResponseGate{
		RequestID: requestID,
		RejectProblem: agentproto.ErrorInfo{
			Code:             "steer_rejected",
			Layer:            "wrapper",
			Stage:            "command_response",
			Operation:        string(command.Kind),
			Message:          "本地 Codex 拒绝了这次追加输入。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		},
		Timeout: steerCommandResponseTimeout,
		TimeoutProblem: agentproto.ErrorInfo{
			Code:             "steer_response_timeout",
			Layer:            "wrapper",
			Stage:            "command_response",
			Operation:        string(command.Kind),
			Message:          "等待本地 Codex 确认追加输入时超时。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		},
		SuppressFrame: true,
	}, nil
}

func (r *claudeBackendRuntime) PrepareChildRestart(_ string, target agentproto.Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	resume, err := r.resolveLaunchResumeTarget(target)
	if err != nil {
		return err
	}
	r.pendingLaunchResume = resume
	return nil
}

func (r *claudeBackendRuntime) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.translator.BuildChildRestartRestoreFrame(commandID)
}

func (r *claudeBackendRuntime) CancelChildRestartRestore(requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translator.CancelChildRestartRestore(requestID)
}

func (r *claudeBackendRuntime) SetDebugLogger(debugLog func(string, ...any)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translator.SetDebugLogger(debugLog)
}

func (r *claudeBackendRuntime) restartPlanForCommand(command agentproto.Command) (*runtimeCommandRestart, error) {
	if command.Kind != agentproto.CommandPromptSend {
		return nil, nil
	}
	targetThreadID := strings.TrimSpace(command.Target.ThreadID)
	explicitMode := agentproto.NormalizePromptExecutionMode(command.Target.ExecutionMode)
	current := r.currentResumeTarget()
	if explicitMode == agentproto.PromptExecutionModeStartNew && targetThreadID == "" {
		if current == nil || strings.TrimSpace(current.ThreadID) == "" {
			return nil, nil
		}
		target := command.Target
		if strings.TrimSpace(target.CWD) == "" {
			target.CWD = strings.TrimSpace(current.CWD)
			if target.CWD == "" {
				target.CWD = strings.TrimSpace(r.workspaceRoot)
			}
		}
		return &runtimeCommandRestart{Target: target}, nil
	}
	if targetThreadID == "" {
		return nil, nil
	}
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
		if agentproto.NormalizePromptExecutionMode(target.ExecutionMode) == agentproto.PromptExecutionModeStartNew {
			return nil, nil
		}
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
