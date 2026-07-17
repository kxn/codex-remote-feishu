package daemon

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type surfaceRecoveryPipelineOptions struct {
	ConsumeVSCodeCompatibilityFollowup bool
	VSCodeSurfaceFilter                string
	VSCodeForceSyncPrompt              bool
	VSCodeInlineSourceMessageID        string
	RunVSCodeRecovery                  bool
	RunDetachedVSCodePrompt            bool
	RunHeadlessRecovery                bool
	HandleEvents                       bool
	SyncAllSurfaceResumeState          bool
	SyncInstanceSurfaceResumeState     string
	SyncClaudeWorkspaceProfileState    bool
	SyncWorkspaceSurfaceContextFiles   bool
}

func (a *App) runSurfaceRecoveryPipelineLocked(ctx context.Context, now time.Time, opts surfaceRecoveryPipelineOptions) []eventcontract.Event {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	events := []eventcontract.Event{}
	vscodeBlocked := false
	if opts.ConsumeVSCodeCompatibilityFollowup {
		a.consumeVSCodeCompatibilityFollowupLocked()
	}
	if opts.RunVSCodeRecovery {
		promptEvents, blocked := a.promptVSCodeCompatibilityAtLocked(
			opts.VSCodeSurfaceFilter,
			now,
			opts.VSCodeForceSyncPrompt,
			opts.VSCodeInlineSourceMessageID,
		)
		events = append(events, promptEvents...)
		if blocked {
			vscodeBlocked = true
		}
		if !vscodeBlocked {
			events = append(events, a.maybeRecoverVSCodeSurfacesLocked(now)...)
			if opts.RunDetachedVSCodePrompt {
				events = append(events, a.maybePromptDetachedVSCodeSurfacesLocked()...)
			}
		}
	}
	if opts.RunHeadlessRecovery {
		events = append(events, a.maybeRecoverHeadlessSurfacesLocked(now)...)
	}
	if opts.HandleEvents {
		a.handleUIEventsLocked(ctx, events)
		events = nil
	}
	if opts.SyncAllSurfaceResumeState {
		a.syncSurfaceResumeStateLocked(nil)
	}
	if opts.SyncInstanceSurfaceResumeState != "" {
		a.syncSurfaceResumeStateForInstanceLocked(opts.SyncInstanceSurfaceResumeState, nil)
	}
	if opts.SyncClaudeWorkspaceProfileState {
		a.syncClaudeWorkspaceProfileStateLocked()
	}
	if opts.SyncWorkspaceSurfaceContextFiles {
		a.syncWorkspaceSurfaceContextFilesLocked()
	}
	return events
}
