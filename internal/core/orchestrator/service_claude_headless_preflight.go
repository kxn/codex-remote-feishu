package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) maybeRestartClaudeHeadlessForPrompt(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, override state.ModelConfigRecord, workspaceHint string) ([]eventcontract.Event, bool) {
	if surface == nil || inst == nil || !isHeadlessInstance(inst) {
		return nil, false
	}
	if state.EffectiveInstanceBackend(inst) != agentproto.BackendClaude {
		return nil, false
	}
	desired := s.headlessLaunchContractWithOverride(surface, override)
	if desired.Backend != agentproto.BackendClaude {
		return nil, false
	}
	current := state.HeadlessLaunchContractFromInstance(inst)
	if current == desired {
		return nil, false
	}
	workspaceKey := normalizeWorkspaceClaimKey(firstNonEmpty(workspaceHint, s.surfaceCurrentWorkspaceKey(surface), inst.WorkspaceRoot))
	attempt := s.buildCurrentHeadlessResumeAttempt(surface, workspaceKey, desired.Backend)
	if normalizeWorkspaceClaimKey(attempt.WorkspaceKey) == "" {
		return notice(surface, "claude_reasoning_restart_workspace_missing", "当前无法确定 Claude headless 的工作区，暂时不能自动切换推理强度。"), true
	}
	return s.startClaudePromptDispatchRestart(surface, inst, attempt, desired), true
}

func (s *Service) startClaudePromptDispatchRestart(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, attempt SurfaceResumeAttempt, launchContract state.HeadlessLaunchContract) []eventcontract.Event {
	if surface == nil || inst == nil {
		return nil
	}
	launchContract = state.NormalizeHeadlessLaunchContract(launchContract)
	if launchContract.Backend != agentproto.BackendClaude {
		return nil
	}
	workspaceKey := normalizeWorkspaceClaimKey(attempt.WorkspaceKey)
	if workspaceKey == "" {
		return nil
	}
	threadCWD := strings.TrimSpace(firstNonEmpty(attempt.ThreadCWD, workspaceKey))

	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-prompt-restart-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	pending := &state.HeadlessLaunchRecord{
		InstanceID:            instanceID,
		ThreadID:              strings.TrimSpace(attempt.ThreadID),
		ThreadTitle:           strings.TrimSpace(attempt.ThreadTitle),
		ThreadCWD:             threadCWD,
		Backend:               launchContract.Backend,
		CodexProviderID:       launchContract.CodexProviderID,
		ClaudeProfileID:       launchContract.ClaudeProfileID,
		ClaudeReasoningEffort: launchContract.ClaudeReasoningEffort,
		RequestedAt:           s.now(),
		ExpiresAt:             s.now().Add(s.config.HeadlessLaunchWait),
		Status:                state.HeadlessLaunchStarting,
		Purpose:               state.HeadlessLaunchPurposePromptDispatchRestart,
		PrepareNewThread:      attempt.PrepareNewThread,
		SourceInstanceID:      inst.InstanceID,
	}
	if !s.claimWorkspace(surface, workspaceKey) {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	surface.AttachedInstanceID = ""
	s.adoptSurfacePendingHeadlessLaunch(surface, pending)

	return []eventcontract.Event{
		{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "claude_runtime_restarting",
				Title: "正在切换推理强度",
				Text:  "当前 Claude 实例正在按下一条消息需要的推理强度重新准备；完成后会自动继续发送。",
			},
		},
		{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       inst.InstanceID,
				ThreadID:         strings.TrimSpace(attempt.ThreadID),
				ThreadTitle:      strings.TrimSpace(attempt.ThreadTitle),
				ThreadCWD:        threadCWD,
			},
		},
		{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: func() *control.DaemonCommand {
				command := &control.DaemonCommand{
					Kind:             control.DaemonCommandStartHeadless,
					SurfaceSessionID: surface.SurfaceSessionID,
					InstanceID:       instanceID,
					ThreadID:         strings.TrimSpace(attempt.ThreadID),
					ThreadTitle:      strings.TrimSpace(attempt.ThreadTitle),
					ThreadCWD:        threadCWD,
				}
				s.applyHeadlessLaunchContract(command, launchContract)
				return command
			}(),
		},
	}
}
