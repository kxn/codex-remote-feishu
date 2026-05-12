package orchestrator

import (
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) buildSnapshot(surface *state.SurfaceConsoleRecord) *control.Snapshot {
	snapshot := &control.Snapshot{
		SurfaceSessionID: surface.SurfaceSessionID,
		ActorUserID:      surface.ActorUserID,
		ProductMode:      string(s.normalizeSurfaceProductMode(surface)),
		Backend:          s.surfaceBackend(surface),
		WorkspaceKey:     s.surfaceCurrentWorkspaceKey(surface),
		CodexProviderID:  s.surfaceCodexProviderID(surface),
		AutoWhip:         snapshotAutoWhipSummary(surface),
		AutoContinue:     snapshotAutoContinueSummary(surface),
	}
	if snapshot.Backend == agentproto.BackendClaude && state.IsHeadlessProductMode(s.normalizeSurfaceProductMode(surface)) {
		snapshot.ClaudeProfileID = s.surfaceClaudeProfileID(surface)
		snapshot.ClaudeProfileName = s.claudeProfileDisplayName(snapshot.ClaudeProfileID)
	}
	snapshot.Gate = s.snapshotGateSummary(surface)
	if pending := surface.PendingHeadless; pending != nil {
		snapshot.PendingHeadless = control.PendingHeadlessSummary{
			InstanceID:            pending.InstanceID,
			ThreadID:              pending.ThreadID,
			ThreadTitle:           pending.ThreadTitle,
			WorkspaceKey:          pending.WorkspaceKey,
			ThreadCWD:             pending.ThreadCWD,
			Backend:               pending.Backend,
			CodexProviderID:       pending.CodexProviderID,
			ClaudeProfileID:       pending.ClaudeProfileID,
			ClaudeReasoningEffort: pending.ClaudeReasoningEffort,
			Status:                string(pending.Status),
			PID:                   pending.PID,
			ExpiresAt:             pending.ExpiresAt,
			RequestedAt:           pending.RequestedAt,
		}
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		selected := inst.Threads[surface.SelectedThreadID]
		if !threadVisible(selected) {
			selected = nil
		}
		selectedTitle := ""
		selectedFirstUserMessage := ""
		selectedLastUserMessage := ""
		selectedModelReroute := (*agentproto.TurnModelReroute)(nil)
		selectedAgeText := ""
		if selected != nil {
			selectedTitle = displayThreadTitle(inst, selected, surface.SelectedThreadID)
			selectedFirstUserMessage = threadFirstUserSnippet(selected, 64)
			selectedLastUserMessage = threadLastUserSnippet(selected, 64)
			selectedModelReroute = agentproto.CloneTurnModelReroute(selected.LastModelReroute)
			selectedAgeText = humanizeRelativeTime(s.now(), selected.LastUsedAt)
		}
		snapshot.Attachment = control.AttachmentSummary{
			InstanceID:                     inst.InstanceID,
			ObjectType:                     snapshotAttachmentObjectType(s.normalizeSurfaceProductMode(surface), inst),
			DisplayName:                    inst.DisplayName,
			Source:                         inst.Source,
			Managed:                        inst.Managed,
			PID:                            inst.PID,
			SelectedThreadID:               surface.SelectedThreadID,
			SelectedThreadTitle:            selectedTitle,
			SelectedThreadFirstUserMessage: selectedFirstUserMessage,
			SelectedThreadLastUserMessage:  selectedLastUserMessage,
			SelectedThreadModelReroute:     selectedModelReroute,
			SelectedThreadAgeText:          selectedAgeText,
			RouteMode:                      string(surface.RouteMode),
			Abandoning:                     surface.Abandoning,
		}
		snapshot.Dispatch = snapshotDispatchSummary(surface, inst)
		snapshot.NextPrompt = s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	}

	for _, inst := range s.root.Instances {
		snapshot.Instances = append(snapshot.Instances, control.InstanceSummary{
			InstanceID:              inst.InstanceID,
			DisplayName:             inst.DisplayName,
			WorkspaceRoot:           inst.WorkspaceRoot,
			WorkspaceKey:            inst.WorkspaceKey,
			Source:                  inst.Source,
			Managed:                 inst.Managed,
			PID:                     inst.PID,
			Online:                  inst.Online,
			State:                   threadStateForInstance(inst),
			ObservedFocusedThreadID: inst.ObservedFocusedThreadID,
		})
		if inst.InstanceID != surface.AttachedInstanceID {
			continue
		}
		for _, thread := range visibleThreads(inst) {
			snapshot.Threads = append(snapshot.Threads, control.ThreadSummary{
				ThreadID:           thread.ThreadID,
				Name:               thread.Name,
				DisplayTitle:       displayThreadTitle(inst, thread, thread.ThreadID),
				Preview:            thread.Preview,
				CWD:                thread.CWD,
				State:              threadLegacyState(thread),
				RuntimeStatus:      threadRuntimeStatusType(thread),
				Model:              thread.ExplicitModel,
				ReasoningEffort:    thread.ExplicitReasoningEffort,
				LastModelReroute:   agentproto.CloneTurnModelReroute(thread.LastModelReroute),
				Loaded:             thread.Loaded,
				WaitingOnApproval:  threadWaitingOnApproval(thread),
				WaitingOnUserInput: threadWaitingOnUserInput(thread),
				IsObservedFocused:  inst.ObservedFocusedThreadID == thread.ThreadID,
				IsSelected:         surface.SelectedThreadID == thread.ThreadID,
			})
		}
	}
	sort.Slice(snapshot.Instances, func(i, j int) bool {
		return snapshot.Instances[i].WorkspaceKey < snapshot.Instances[j].WorkspaceKey
	})
	return snapshot
}

func snapshotAttachmentObjectType(mode state.ProductMode, inst *state.InstanceRecord) string {
	switch {
	case inst == nil:
		return ""
	case state.IsHeadlessProductMode(mode):
		return "workspace"
	case isVSCodeInstance(inst):
		return "vscode_instance"
	case isHeadlessInstance(inst):
		return "headless_instance"
	default:
		return "instance"
	}
}

func snapshotAutoWhipSummary(surface *state.SurfaceConsoleRecord) control.AutoWhipSummary {
	if surface == nil {
		return control.AutoWhipSummary{}
	}
	return control.AutoWhipSummary{
		Enabled:             surface.AutoWhip.Enabled,
		PendingReason:       string(surface.AutoWhip.PendingReason),
		PendingDueAt:        surface.AutoWhip.PendingDueAt,
		ConsecutiveCount:    surface.AutoWhip.ConsecutiveCount,
		LastTriggeredTurnID: surface.AutoWhip.LastTriggeredTurnID,
	}
}

func snapshotAutoContinueSummary(surface *state.SurfaceConsoleRecord) control.AutoContinueSummary {
	if surface == nil {
		return control.AutoContinueSummary{}
	}
	summary := control.AutoContinueSummary{
		Enabled: surface.AutoContinue.Enabled,
	}
	if episode := activeAutoContinueEpisode(surface); episode != nil {
		summary.State = string(episode.State)
		summary.PendingDueAt = episode.PendingDueAt
		summary.AttemptCount = episode.AttemptCount
		summary.ConsecutiveDryFailureCount = episode.ConsecutiveDryFailureCount
		summary.TriggerKind = string(episode.TriggerKind)
	}
	return summary
}

func (s *Service) snapshotGateSummary(surface *state.SurfaceConsoleRecord) control.GateSummary {
	if surface == nil {
		return control.GateSummary{}
	}
	if surface.ActiveRequestCapture != nil {
		return control.GateSummary{Kind: "request_capture"}
	}
	if s.targetPickerHasBlockingProcessing(surface) {
		return control.GateSummary{Kind: "target_picker"}
	}
	if s.activePathPicker(surface) != nil {
		return control.GateSummary{Kind: "path_picker"}
	}
	count := 0
	for requestID, request := range surface.PendingRequests {
		if request == nil {
			removePendingRequest(surface, requestID)
			continue
		}
		count++
	}
	if count != 0 {
		summary := control.GateSummary{Kind: "pending_request", PendingRequestCount: count}
		if active := activePendingRequest(surface); active != nil {
			summary.PendingRequestLifecycle = normalizeRequestLifecycleState(active.LifecycleState)
			summary.PendingRequestVisibility = normalizeRequestVisibilityState(active.VisibilityState)
		}
		return summary
	}
	return control.GateSummary{}
}

func snapshotDispatchSummary(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) control.DispatchSummary {
	if surface == nil {
		return control.DispatchSummary{}
	}
	summary := control.DispatchSummary{
		DispatchMode: string(surface.DispatchMode),
		QueuedCount:  len(surface.QueuedQueueItemIDs),
	}
	if inst != nil {
		summary.InstanceOnline = inst.Online
	}
	if surface.ActiveQueueItemID == "" {
		return summary
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		return summary
	}
	summary.ActiveItemStatus = string(item.Status)
	return summary
}
