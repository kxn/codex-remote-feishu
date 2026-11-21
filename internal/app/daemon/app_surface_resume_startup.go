package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) shouldDeferHeadlessResumeUntilInitialRefreshLocked(entry surfaceresume.Entry, allowMissingTargetFailure bool) bool {
	if allowMissingTargetFailure {
		return false
	}
	instanceID := strings.TrimSpace(entry.ResumeInstanceID)
	if instanceID == "" {
		return false
	}
	inst := a.service.Instance(instanceID)
	if inst == nil {
		return false
	}
	// Give a connected visible-instance resume one startup refresh round before
	// falling back to a managed headless restart for the same persisted target.
	return !state.IsInstanceSource(inst.Source, state.InstanceSourceHeadless)
}

func (a *App) markStartupThreadsRefreshRequestedLocked(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	if a.surfaceResumeRuntime.startupRefreshPending == nil {
		a.surfaceResumeRuntime.startupRefreshPending = map[string]bool{}
	}
	a.surfaceResumeRuntime.startupRefreshSeen = true
	a.surfaceResumeRuntime.startupRefreshPending[instanceID] = true
}

func (a *App) markStartupThreadsRefreshSettledLocked(instanceID string) {
	a.surfaceResumeRuntime.startupRefreshSeen = true
	delete(a.surfaceResumeRuntime.startupRefreshPending, strings.TrimSpace(instanceID))
}

func (a *App) initialThreadsRefreshRoundCompleteLocked() bool {
	return a.surfaceResumeRuntime.startupRefreshSeen && len(a.surfaceResumeRuntime.startupRefreshPending) == 0
}
