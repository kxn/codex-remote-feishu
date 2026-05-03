package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type headlessContractSwitchContinuation struct {
	Attempt           SurfaceResumeAttempt
	RestartManagedNow bool
	RestartInstanceID string
}

func (s *Service) buildHeadlessWorkspaceContinuation(surface *state.SurfaceConsoleRecord, workspaceKey string, backend agentproto.Backend, prepareNewThread bool) headlessContractSwitchContinuation {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	backend = agentproto.NormalizeBackend(backend)
	if surface == nil || workspaceKey == "" || backend == "" {
		return headlessContractSwitchContinuation{}
	}
	continuation := headlessContractSwitchContinuation{
		Attempt: SurfaceResumeAttempt{
			WorkspaceKey:     workspaceKey,
			Backend:          backend,
			PrepareNewThread: prepareNewThread,
		},
	}
	if resolution := s.resolveWorkspaceContract(surface, workspaceKey, backend); resolution.Mode == contractResolutionRestartManaged && resolution.RestartInstance != nil {
		continuation.RestartManagedNow = true
		continuation.RestartInstanceID = strings.TrimSpace(resolution.RestartInstance.InstanceID)
	}
	return continuation
}

func (s *Service) buildCurrentHeadlessResumeAttempt(surface *state.SurfaceConsoleRecord, workspaceKey string, backend agentproto.Backend) SurfaceResumeAttempt {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	backend = agentproto.NormalizeBackend(backend)
	if surface == nil || workspaceKey == "" || backend == "" {
		return SurfaceResumeAttempt{}
	}

	attempt := SurfaceResumeAttempt{
		WorkspaceKey:     workspaceKey,
		Backend:          backend,
		PrepareNewThread: surface.RouteMode == state.RouteModeNewThreadReady,
	}
	selectedThreadID := strings.TrimSpace(surface.SelectedThreadID)
	if selectedThreadID == "" || !s.surfaceOwnsThread(surface, selectedThreadID) {
		return attempt
	}

	attempt.ThreadID = selectedThreadID
	attempt.ResumeHeadless = true

	if view := s.mergedThreadViewForBackend(surface, selectedThreadID, backend, true); view != nil {
		attempt.ThreadTitle = displayThreadTitle(view.Inst, view.Thread, selectedThreadID)
		attempt.ThreadCWD = threadCWD(view)
	}
	if attempt.ThreadTitle == "" && surface.LastSelection != nil &&
		strings.TrimSpace(surface.LastSelection.ThreadID) == selectedThreadID {
		attempt.ThreadTitle = strings.TrimSpace(surface.LastSelection.Title)
	}
	if attempt.ThreadCWD == "" {
		inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
		if inst != nil {
			thread := inst.Threads[selectedThreadID]
			if thread != nil {
				attempt.ThreadCWD = strings.TrimSpace(thread.CWD)
				if attempt.ThreadTitle == "" {
					attempt.ThreadTitle = displayThreadTitle(inst, thread, selectedThreadID)
				}
			}
		}
	}
	if attempt.ThreadCWD == "" {
		attempt.ThreadCWD = workspaceKey
	}
	return attempt
}

func (s *Service) buildHeadlessContractSwitchContinuation(surface *state.SurfaceConsoleRecord, workspaceKey string, backend agentproto.Backend) headlessContractSwitchContinuation {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	backend = agentproto.NormalizeBackend(backend)
	if surface == nil || workspaceKey == "" || backend == "" {
		return headlessContractSwitchContinuation{}
	}

	continuation := headlessContractSwitchContinuation{
		Attempt: s.buildCurrentHeadlessResumeAttempt(surface, workspaceKey, backend),
	}

	selectedThreadID := strings.TrimSpace(continuation.Attempt.ThreadID)
	if selectedThreadID == "" {
		if resolution := s.resolveWorkspaceContract(surface, workspaceKey, backend); resolution.Mode == contractResolutionRestartManaged && resolution.RestartInstance != nil {
			continuation.RestartManagedNow = true
			continuation.RestartInstanceID = strings.TrimSpace(resolution.RestartInstance.InstanceID)
		}
		return continuation
	}

	if inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]; isHeadlessInstance(inst) && state.EffectiveInstanceBackend(inst) == backend {
		continuation.RestartManagedNow = true
		continuation.RestartInstanceID = strings.TrimSpace(inst.InstanceID)
	}
	if view := s.mergedThreadViewForBackend(surface, selectedThreadID, backend, true); view != nil {
		resolution := s.resolveThreadContract(surface, view, view.CurrentVisible && s.currentVisibleThreadEligible(surface, view.ThreadID), true)
		if resolution.Mode == contractResolutionRestartManaged && resolution.RestartInstance != nil {
			continuation.RestartManagedNow = true
			continuation.RestartInstanceID = strings.TrimSpace(resolution.RestartInstance.InstanceID)
		}
	}
	return continuation
}

func (s *Service) restartHeadlessContractContinuation(surface *state.SurfaceConsoleRecord, continuation headlessContractSwitchContinuation) []eventcontract.Event {
	return s.restartHeadlessContractContinuationWithOverlayCleanup(surface, continuation, surfaceOverlayRouteCleanupOptions{})
}

func (s *Service) restartHeadlessContractContinuationWithOverlayCleanup(surface *state.SurfaceConsoleRecord, continuation headlessContractSwitchContinuation, cleanup surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	attempt := continuation.Attempt
	if normalizeWorkspaceClaimKey(attempt.WorkspaceKey) == "" {
		return nil
	}
	return s.startHeadlessForContractSwitchWithOverlayCleanup(surface, attempt, cleanup)
}

func (s *Service) executeResolvedWorkspaceContinuation(surface *state.SurfaceConsoleRecord, continuation headlessContractSwitchContinuation, resolution contractResolution, options attachWorkspaceOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	workspaceKey := normalizeWorkspaceClaimKey(continuation.Attempt.WorkspaceKey)
	if workspaceKey == "" {
		return nil
	}
	options.PrepareNewThread = options.PrepareNewThread || continuation.Attempt.PrepareNewThread
	switch resolution.Mode {
	case contractResolutionAttachVisible, contractResolutionReuseManaged:
		return s.attachWorkspaceWithOptions(surface, workspaceKey, options)
	case contractResolutionRestartManaged, contractResolutionCreateHeadless:
		events := s.queueHeadlessContractRestart(nil, surface, continuation)
		return append(events, s.restartHeadlessContractContinuationWithOverlayCleanup(surface, continuation, options.OverlayCleanup)...)
	case contractResolutionUnavailable:
		return notice(surface,
			firstNonEmpty(strings.TrimSpace(resolution.NoticeCode), "workspace_instance_busy"),
			firstNonEmpty(strings.TrimSpace(resolution.NoticeText), "目标工作区当前暂时不可接管，请稍后重试。"),
		)
	default:
		return nil
	}
}

func (s *Service) startHeadlessForContractSwitch(surface *state.SurfaceConsoleRecord, attempt SurfaceResumeAttempt) []eventcontract.Event {
	return s.startHeadlessForContractSwitchWithOverlayCleanup(surface, attempt, surfaceOverlayRouteCleanupOptions{})
}

func (s *Service) startHeadlessForContractSwitchWithOverlayCleanup(surface *state.SurfaceConsoleRecord, attempt SurfaceResumeAttempt, cleanup surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if strings.TrimSpace(attempt.ThreadID) != "" {
		view := s.headlessRestoreView(surface, attempt)
		if view != nil {
			return s.startHeadlessForResolvedThreadWithModeAndOverlayCleanup(surface, view, startHeadlessModeDefault, cleanup)
		}
	}
	return s.startFreshWorkspaceHeadlessWithOverlayCleanup(surface, attempt.WorkspaceKey, attempt.PrepareNewThread, cleanup)
}

func (s *Service) queueHeadlessContractRestart(events []eventcontract.Event, surface *state.SurfaceConsoleRecord, continuation headlessContractSwitchContinuation) []eventcontract.Event {
	if surface == nil || !continuation.RestartManagedNow || strings.TrimSpace(continuation.RestartInstanceID) == "" {
		return events
	}
	return append(events, eventcontract.Event{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandKillHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       continuation.RestartInstanceID,
			ThreadID:         continuation.Attempt.ThreadID,
			ThreadTitle:      continuation.Attempt.ThreadTitle,
			ThreadCWD:        continuation.Attempt.ThreadCWD,
		},
	})
}
