package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type codexProviderSwitchContinuation struct {
	Attempt           SurfaceResumeAttempt
	RestartManagedNow bool
	RestartInstanceID string
}

func (s *Service) buildCodexProviderSwitchContinuation(surface *state.SurfaceConsoleRecord, workspaceKey string) codexProviderSwitchContinuation {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if surface == nil || workspaceKey == "" {
		return codexProviderSwitchContinuation{}
	}

	continuation := codexProviderSwitchContinuation{
		Attempt: SurfaceResumeAttempt{
			WorkspaceKey:     workspaceKey,
			Backend:          agentproto.BackendCodex,
			PrepareNewThread: surface.RouteMode == state.RouteModeNewThreadReady,
		},
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if isHeadlessInstance(inst) && state.EffectiveInstanceBackend(inst) == agentproto.BackendCodex {
		continuation.RestartManagedNow = true
		continuation.RestartInstanceID = strings.TrimSpace(inst.InstanceID)
	}

	selectedThreadID := strings.TrimSpace(surface.SelectedThreadID)
	if selectedThreadID == "" || !s.surfaceOwnsThread(surface, selectedThreadID) {
		return continuation
	}

	continuation.Attempt.ThreadID = selectedThreadID
	continuation.Attempt.ResumeHeadless = true

	if view := s.mergedThreadViewForBackend(surface, selectedThreadID, agentproto.BackendCodex, true); view != nil {
		continuation.Attempt.ThreadTitle = displayThreadTitle(view.Inst, view.Thread, selectedThreadID)
		continuation.Attempt.ThreadCWD = threadCWD(view)
	}
	if continuation.Attempt.ThreadTitle == "" && surface.LastSelection != nil &&
		strings.TrimSpace(surface.LastSelection.ThreadID) == selectedThreadID {
		continuation.Attempt.ThreadTitle = strings.TrimSpace(surface.LastSelection.Title)
	}
	if continuation.Attempt.ThreadCWD == "" {
		inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
		if inst != nil {
			thread := inst.Threads[selectedThreadID]
			if thread != nil {
				continuation.Attempt.ThreadCWD = strings.TrimSpace(thread.CWD)
				if continuation.Attempt.ThreadTitle == "" {
					continuation.Attempt.ThreadTitle = displayThreadTitle(inst, thread, selectedThreadID)
				}
			}
		}
	}
	if continuation.Attempt.ThreadCWD == "" {
		continuation.Attempt.ThreadCWD = workspaceKey
	}

	return continuation
}

func (s *Service) restartCodexProviderContinuation(surface *state.SurfaceConsoleRecord, continuation codexProviderSwitchContinuation) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	attempt := continuation.Attempt
	if normalizeWorkspaceClaimKey(attempt.WorkspaceKey) == "" {
		return nil
	}
	return s.startHeadlessForCodexProviderSwitch(surface, attempt)
}

func (s *Service) startHeadlessForCodexProviderSwitch(surface *state.SurfaceConsoleRecord, attempt SurfaceResumeAttempt) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if strings.TrimSpace(attempt.ThreadID) != "" {
		view := s.headlessRestoreView(surface, attempt)
		if view != nil {
			return s.startHeadlessForResolvedThreadWithMode(surface, view, startHeadlessModeDefault)
		}
	}
	return s.startFreshWorkspaceHeadlessWithOptions(surface, attempt.WorkspaceKey, attempt.PrepareNewThread)
}

