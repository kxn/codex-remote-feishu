package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Service) buildSnapshot(surface *state.SurfaceConsoleRecord) *control.Snapshot {
	snapshot := &control.Snapshot{
		SurfaceSessionID: surface.SurfaceSessionID,
		ActorUserID:      surface.ActorUserID,
		ProductMode:      string(s.normalizeSurfaceProductMode(surface)),
		WorkspaceKey:     s.surfaceCurrentWorkspaceKey(surface),
		AutoContinue:     snapshotAutoContinueSummary(surface),
	}
	snapshot.Gate = snapshotGateSummary(surface)
	if pending := surface.PendingHeadless; pending != nil {
		snapshot.PendingHeadless = control.PendingHeadlessSummary{
			InstanceID:  pending.InstanceID,
			ThreadID:    pending.ThreadID,
			ThreadTitle: pending.ThreadTitle,
			ThreadCWD:   pending.ThreadCWD,
			Status:      string(pending.Status),
			PID:         pending.PID,
			ExpiresAt:   pending.ExpiresAt,
			RequestedAt: pending.RequestedAt,
		}
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		selected := inst.Threads[surface.SelectedThreadID]
		if !threadVisible(selected) {
			selected = nil
		}
		selectedTitle := ""
		selectedPreview := ""
		if selected != nil {
			selectedTitle = displayThreadTitle(inst, selected, surface.SelectedThreadID)
			selectedPreview = threadPreview(selected)
		}
		snapshot.Attachment = control.AttachmentSummary{
			InstanceID:            inst.InstanceID,
			DisplayName:           inst.DisplayName,
			Source:                inst.Source,
			Managed:               inst.Managed,
			PID:                   inst.PID,
			SelectedThreadID:      surface.SelectedThreadID,
			SelectedThreadTitle:   selectedTitle,
			SelectedThreadPreview: selectedPreview,
			RouteMode:             string(surface.RouteMode),
			Abandoning:            surface.Abandoning,
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
				ThreadID:          thread.ThreadID,
				Name:              thread.Name,
				DisplayTitle:      displayThreadTitle(inst, thread, thread.ThreadID),
				Preview:           thread.Preview,
				CWD:               thread.CWD,
				State:             thread.State,
				Model:             thread.ExplicitModel,
				ReasoningEffort:   thread.ExplicitReasoningEffort,
				Loaded:            thread.Loaded,
				IsObservedFocused: inst.ObservedFocusedThreadID == thread.ThreadID,
				IsSelected:        surface.SelectedThreadID == thread.ThreadID,
			})
		}
	}
	sort.Slice(snapshot.Instances, func(i, j int) bool {
		return snapshot.Instances[i].WorkspaceKey < snapshot.Instances[j].WorkspaceKey
	})
	return snapshot
}

func snapshotAutoContinueSummary(surface *state.SurfaceConsoleRecord) control.AutoContinueSummary {
	if surface == nil {
		return control.AutoContinueSummary{}
	}
	return control.AutoContinueSummary{
		Enabled:             surface.AutoContinue.Enabled,
		PendingReason:       string(surface.AutoContinue.PendingReason),
		PendingDueAt:        surface.AutoContinue.PendingDueAt,
		ConsecutiveCount:    surface.AutoContinue.ConsecutiveCount,
		LastTriggeredTurnID: surface.AutoContinue.LastTriggeredTurnID,
	}
}

func snapshotGateSummary(surface *state.SurfaceConsoleRecord) control.GateSummary {
	if surface == nil {
		return control.GateSummary{}
	}
	if surface.ActiveRequestCapture != nil {
		return control.GateSummary{Kind: "request_capture"}
	}
	count := 0
	for requestID, request := range surface.PendingRequests {
		if request == nil {
			delete(surface.PendingRequests, requestID)
			continue
		}
		count++
	}
	if count != 0 {
		return control.GateSummary{Kind: "pending_request", PendingRequestCount: count}
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

func (s *Service) resolveNextPromptSummary(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, frozenThreadID, frozenCWD string, override state.ModelConfigRecord) control.PromptRouteSummary {
	if inst == nil || surface == nil {
		return control.PromptRouteSummary{}
	}
	threadID := frozenThreadID
	cwd := frozenCWD
	routeMode := surface.RouteMode
	createThread := false
	if threadID == "" && cwd == "" {
		threadID, cwd, routeMode, createThread = freezeRoute(inst, surface)
	} else {
		createThread = threadID == ""
	}
	if promptOverrideIsEmpty(override) {
		override = surface.PromptOverride
	}
	threadTitle := ""
	if threadID != "" {
		threadTitle = displayThreadTitle(inst, inst.Threads[threadID], threadID)
	}
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return control.PromptRouteSummary{
		RouteMode:                      string(routeMode),
		ThreadID:                       threadID,
		ThreadTitle:                    threadTitle,
		CWD:                            cwd,
		CreateThread:                   createThread,
		BaseModel:                      resolution.BaseModel.Value,
		BaseReasoningEffort:            resolution.BaseReasoningEffort.Value,
		BaseModelSource:                resolution.BaseModel.Source,
		BaseReasoningEffortSource:      resolution.BaseReasoningEffort.Source,
		OverrideModel:                  resolution.Override.Model,
		OverrideReasoningEffort:        resolution.Override.ReasoningEffort,
		OverrideAccessMode:             resolution.Override.AccessMode,
		EffectiveModel:                 resolution.EffectiveModel.Value,
		EffectiveReasoningEffort:       resolution.EffectiveReasoningEffort.Value,
		EffectiveAccessMode:            resolution.EffectiveAccessMode,
		EffectiveModelSource:           resolution.EffectiveModel.Source,
		EffectiveReasoningEffortSource: resolution.EffectiveReasoningEffort.Source,
		EffectiveAccessModeSource:      resolution.EffectiveAccessModeSource,
	}
}

type configValue struct {
	Value  string
	Source string
}

type promptConfigResolution struct {
	Override                  state.ModelConfigRecord
	BaseModel                 configValue
	BaseReasoningEffort       configValue
	EffectiveModel            configValue
	EffectiveReasoningEffort  configValue
	EffectiveAccessMode       string
	EffectiveAccessModeSource string
}

func promptOverrideIsEmpty(value state.ModelConfigRecord) bool {
	return strings.TrimSpace(value.Model) == "" &&
		strings.TrimSpace(value.ReasoningEffort) == "" &&
		strings.TrimSpace(value.AccessMode) == ""
}

func compactPromptOverride(value state.ModelConfigRecord) state.ModelConfigRecord {
	value.AccessMode = agentproto.NormalizeAccessMode(value.AccessMode)
	if promptOverrideIsEmpty(value) {
		return state.ModelConfigRecord{}
	}
	return value
}

func (s *Service) resolveFrozenPromptOverride(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) state.ModelConfigRecord {
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return state.ModelConfigRecord{
		Model:           resolution.EffectiveModel.Value,
		ReasoningEffort: resolution.EffectiveReasoningEffort.Value,
		AccessMode:      resolution.EffectiveAccessMode,
	}
}

func (s *Service) resolvePromptConfig(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) promptConfigResolution {
	if surface != nil && promptOverrideIsEmpty(override) {
		override = surface.PromptOverride
	}
	override = compactPromptOverride(override)
	baseModel, baseEffort := resolveBasePromptConfig(inst, threadID, cwd)
	effectiveModel := baseModel
	if override.Model != "" {
		effectiveModel = configValue{Value: override.Model, Source: "surface_override"}
	} else if effectiveModel.Value == "" {
		effectiveModel = configValue{Value: defaultModel, Source: "surface_default"}
	}
	effectiveEffort := baseEffort
	if override.ReasoningEffort != "" {
		effectiveEffort = configValue{Value: override.ReasoningEffort, Source: "surface_override"}
	} else if effectiveEffort.Value == "" {
		effectiveEffort = configValue{Value: defaultReasoningEffort, Source: "surface_default"}
	}
	effectiveAccessMode := agentproto.EffectiveAccessMode(override.AccessMode)
	effectiveAccessModeSource := "surface_default"
	if agentproto.NormalizeAccessMode(override.AccessMode) != "" {
		effectiveAccessModeSource = "surface_override"
	}
	return promptConfigResolution{
		Override:                  override,
		BaseModel:                 baseModel,
		BaseReasoningEffort:       baseEffort,
		EffectiveModel:            effectiveModel,
		EffectiveReasoningEffort:  effectiveEffort,
		EffectiveAccessMode:       effectiveAccessMode,
		EffectiveAccessModeSource: effectiveAccessModeSource,
	}
}

func resolveBasePromptConfig(inst *state.InstanceRecord, threadID, cwd string) (configValue, configValue) {
	model := configValue{Source: "unknown"}
	effort := configValue{Source: "unknown"}
	if inst == nil {
		return model, effort
	}
	if thread := inst.Threads[threadID]; thread != nil {
		if cwd == "" {
			cwd = thread.CWD
		}
		if thread.ExplicitModel != "" {
			model = configValue{Value: thread.ExplicitModel, Source: "thread"}
		}
		if thread.ExplicitReasoningEffort != "" {
			effort = configValue{Value: thread.ExplicitReasoningEffort, Source: "thread"}
		}
	}
	if cwd != "" {
		if defaults, ok := inst.CWDDefaults[cwd]; ok {
			if model.Value == "" && defaults.Model != "" {
				model = configValue{Value: defaults.Model, Source: "cwd_default"}
			}
			if effort.Value == "" && defaults.ReasoningEffort != "" {
				effort = configValue{Value: defaults.ReasoningEffort, Source: "cwd_default"}
			}
		}
	}
	return model, effort
}

func (s *Service) findAttachedSurface(instanceID string) *state.SurfaceConsoleRecord {
	for _, surface := range s.root.Surfaces {
		if surface.AttachedInstanceID == instanceID {
			return surface
		}
	}
	return nil
}

func (s *Service) findAttachedSurfaces(instanceID string) []*state.SurfaceConsoleRecord {
	var surfaces []*state.SurfaceConsoleRecord
	for _, surface := range s.root.Surfaces {
		if surface.AttachedInstanceID == instanceID {
			surfaces = append(surfaces, surface)
		}
	}
	return surfaces
}

func (s *Service) SurfaceSnapshot(surfaceID string) *control.Snapshot {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil
	}
	return s.buildSnapshot(surface)
}

func (s *Service) AttachedInstanceID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.AttachedInstanceID
}

func (s *Service) SurfaceChatID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.ChatID
}

func (s *Service) SurfaceGatewayID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.GatewayID
}

func (s *Service) SurfaceActorUserID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.ActorUserID
}

func (s *Service) MaterializeSurface(surfaceID, gatewayID, chatID, actorUserID string) {
	if strings.TrimSpace(surfaceID) == "" {
		return
	}
	s.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        gatewayID,
		SurfaceSessionID: surfaceID,
		ChatID:           chatID,
		ActorUserID:      actorUserID,
	})
}

func (s *Service) BindPendingRemoteCommand(surfaceID, commandID string) {
	if commandID == "" {
		return
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return
	}
	if surface.AttachedInstanceID != "" {
		binding := s.pendingRemote[surface.AttachedInstanceID]
		if binding != nil && binding.SurfaceSessionID == surfaceID {
			if surface.ActiveQueueItemID != "" && binding.QueueItemID != surface.ActiveQueueItemID {
				return
			}
			binding.CommandID = commandID
			return
		}
	}
	for _, binding := range s.pendingSteers {
		if binding == nil || binding.SurfaceSessionID != surfaceID || binding.CommandID != "" {
			continue
		}
		binding.CommandID = commandID
		return
	}
}

func (s *Service) failSurfaceActiveQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, notice *control.Notice, tryDispatchNext bool) []control.UIEvent {
	if surface == nil || item == nil {
		return nil
	}
	item.Status = state.QueueItemFailed
	if surface.ActiveQueueItemID == item.ID {
		surface.ActiveQueueItemID = ""
	}
	if binding := s.remoteBindingForSurface(surface); binding != nil {
		s.clearTurnArtifacts(binding.InstanceID, binding.ThreadID, binding.TurnID)
	}
	s.clearRemoteOwnership(surface)

	events := s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		TypingOff:   true,
	}, queueItemSourceMessageIDs(item))
	if notice != nil && (strings.TrimSpace(notice.Code) != "" || strings.TrimSpace(notice.Title) != "" || strings.TrimSpace(notice.Text) != "") {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	if tryDispatchNext {
		events = append(events, s.dispatchNext(surface)...)
	}
	events = append(events, s.finishSurfaceAfterWork(surface)...)
	return events
}

func (s *Service) HandleCommandDispatchFailure(surfaceID, commandID string, err error) []control.UIEvent {
	surface := s.root.Surfaces[surfaceID]
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             "dispatch_failed",
		Layer:            "daemon",
		Stage:            "dispatch_command",
		Message:          "消息未成功发送到本地 Codex。",
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	notice := NoticeForProblem(problem)
	notice.Code = "dispatch_failed"
	if key, binding := s.pendingSteerForCommand("", commandID); binding != nil {
		_ = binding
		notice.Code = "steer_failed"
		notice.Text = appendSteerRestoreHint(notice.Text)
		return s.restorePendingSteer(key, &notice)
	}
	if surface == nil || surface.ActiveQueueItemID == "" {
		return nil
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.Status != state.QueueItemDispatching {
		return nil
	}
	return s.failSurfaceActiveQueueItem(surface, item, &notice, true)
}

func (s *Service) HandleCommandAccepted(instanceID string, ack agentproto.CommandAck) []control.UIEvent {
	if ack.CommandID == "" {
		return nil
	}
	key, binding := s.pendingSteerForCommand(instanceID, ack.CommandID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.pendingSteers, key)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || item.Status != state.QueueItemSteering {
		delete(s.pendingSteers, key)
		return nil
	}
	item.Status = state.QueueItemSteered
	delete(s.pendingSteers, key)
	return s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
		ThumbsUp:    true,
	}, queueItemSourceMessageIDs(item))
}

func (s *Service) HandleCommandRejected(instanceID string, ack agentproto.CommandAck) []control.UIEvent {
	if ack.CommandID == "" {
		return nil
	}
	if key, binding := s.pendingSteerForCommand(instanceID, ack.CommandID); binding != nil {
		notice := NoticeForProblem(commandAckProblem(binding.SurfaceSessionID, ack))
		notice.Code = "steer_failed"
		notice.Text = appendSteerRestoreHint(notice.Text)
		return s.restorePendingSteer(key, &notice)
	}
	binding := s.pendingRemote[instanceID]
	if binding == nil || binding.CommandID != ack.CommandID {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || item.Status != state.QueueItemDispatching {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	notice := NoticeForProblem(commandAckProblem(surface.SurfaceSessionID, ack))
	notice.Code = "command_rejected"
	return s.failSurfaceActiveQueueItem(surface, item, &notice, true)
}

func (s *Service) pendingSteerForCommand(instanceID, commandID string) (string, *pendingSteerBinding) {
	if strings.TrimSpace(commandID) == "" {
		return "", nil
	}
	for key, binding := range s.pendingSteers {
		if binding == nil || binding.CommandID != commandID {
			continue
		}
		if strings.TrimSpace(instanceID) != "" && binding.InstanceID != instanceID {
			continue
		}
		return key, binding
	}
	return "", nil
}

func (s *Service) restorePendingSteer(key string, notice *control.Notice) []control.UIEvent {
	binding := s.pendingSteers[key]
	if binding == nil {
		return nil
	}
	delete(s.pendingSteers, key)
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		return nil
	}
	if item.Status == state.QueueItemSteered {
		return nil
	}
	item.Status = state.QueueItemQueued
	surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
	surface.QueuedQueueItemIDs = insertString(surface.QueuedQueueItemIDs, binding.QueueIndex, item.ID)
	var events []control.UIEvent
	if notice != nil && (strings.TrimSpace(notice.Code) != "" || strings.TrimSpace(notice.Title) != "" || strings.TrimSpace(notice.Text) != "") {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	events = append(events, s.dispatchNext(surface)...)
	events = append(events, s.finishSurfaceAfterWork(surface)...)
	return events
}

func (s *Service) restorePendingSteersForInstance(instanceID string) []control.UIEvent {
	var events []control.UIEvent
	keys := make([]string, 0, len(s.pendingSteers))
	for key, binding := range s.pendingSteers {
		if binding == nil || binding.InstanceID != instanceID {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		events = append(events, s.restorePendingSteer(key, nil)...)
	}
	return events
}

func appendSteerRestoreHint(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "追加输入失败，已恢复原排队位置。"
	}
	if strings.Contains(text, "恢复") {
		return text
	}
	return text + " 已恢复原排队位置。"
}

func (s *Service) HandleHeadlessLaunchStarted(surfaceID, instanceID string, pid int) []control.UIEvent {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID {
		return nil
	}
	surface.PendingHeadless.PID = pid
	return nil
}

func (s *Service) HandleHeadlessLaunchFailed(surfaceID, instanceID string, err error) []control.UIEvent {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID {
		return nil
	}
	pending := surface.PendingHeadless
	surface.PendingHeadless = nil
	if pending.AutoRestore {
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_restore_start_failed",
				Title: "恢复失败",
				Text:  "之前的会话暂时无法恢复，请稍后重试或尝试其他会话。",
			},
		}}
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             "headless_start_failed",
		Layer:            "daemon",
		Stage:            "headless_start",
		Operation:        "start_headless",
		Message:          "无法准备恢复会话。",
		SurfaceSessionID: surface.SurfaceSessionID,
		ThreadID:         pending.ThreadID,
		Retryable:        true,
	})
	notice := NoticeForProblem(problem)
	notice.Code = "headless_start_failed"
	notice.Title = "恢复准备失败"
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	}}
}

func (s *Service) Instance(instanceID string) *state.InstanceRecord {
	return s.root.Instances[instanceID]
}

func (s *Service) Instances() []*state.InstanceRecord {
	instances := make([]*state.InstanceRecord, 0, len(s.root.Instances))
	for _, instance := range s.root.Instances {
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].WorkspaceKey == instances[j].WorkspaceKey {
			return instances[i].InstanceID < instances[j].InstanceID
		}
		return instances[i].WorkspaceKey < instances[j].WorkspaceKey
	})
	return instances
}

func (s *Service) Surfaces() []*state.SurfaceConsoleRecord {
	surfaces := make([]*state.SurfaceConsoleRecord, 0, len(s.root.Surfaces))
	for _, surface := range s.root.Surfaces {
		surfaces = append(surfaces, surface)
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].SurfaceSessionID < surfaces[j].SurfaceSessionID
	})
	return surfaces
}

type RemoteTurnStatus struct {
	InstanceID       string `json:"instanceId"`
	SurfaceSessionID string `json:"surfaceSessionId"`
	QueueItemID      string `json:"queueItemId"`
	SourceMessageID  string `json:"sourceMessageId,omitempty"`
	CommandID        string `json:"commandId,omitempty"`
	ThreadID         string `json:"threadId,omitempty"`
	TurnID           string `json:"turnId,omitempty"`
	Status           string `json:"status"`
}

func (s *Service) PendingRemoteTurns() []RemoteTurnStatus {
	values := make([]RemoteTurnStatus, 0, len(s.pendingRemote))
	for _, binding := range s.pendingRemote {
		if binding == nil {
			continue
		}
		values = append(values, RemoteTurnStatus{
			InstanceID:       binding.InstanceID,
			SurfaceSessionID: binding.SurfaceSessionID,
			QueueItemID:      binding.QueueItemID,
			SourceMessageID:  binding.SourceMessageID,
			CommandID:        binding.CommandID,
			ThreadID:         binding.ThreadID,
			TurnID:           binding.TurnID,
			Status:           binding.Status,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].InstanceID == values[j].InstanceID {
			return values[i].QueueItemID < values[j].QueueItemID
		}
		return values[i].InstanceID < values[j].InstanceID
	})
	return values
}

func (s *Service) ActiveRemoteTurns() []RemoteTurnStatus {
	values := make([]RemoteTurnStatus, 0, len(s.activeRemote))
	for _, binding := range s.activeRemote {
		if binding == nil {
			continue
		}
		values = append(values, RemoteTurnStatus{
			InstanceID:       binding.InstanceID,
			SurfaceSessionID: binding.SurfaceSessionID,
			QueueItemID:      binding.QueueItemID,
			SourceMessageID:  binding.SourceMessageID,
			CommandID:        binding.CommandID,
			ThreadID:         binding.ThreadID,
			TurnID:           binding.TurnID,
			Status:           binding.Status,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].InstanceID == values[j].InstanceID {
			return values[i].TurnID < values[j].TurnID
		}
		return values[i].InstanceID < values[j].InstanceID
	})
	return values
}

func (s *Service) ApplyInstanceConnected(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = true

	var events []control.UIEvent
	events = append(events, s.restorePendingSteersForInstance(instanceID)...)
	for _, surface := range s.root.Surfaces {
		pending := surface.PendingHeadless
		if pending == nil || pending.InstanceID != instanceID {
			continue
		}
		events = append(events, s.attachHeadlessInstance(surface, inst, pending)...)
	}
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.dispatchNext(surface)...)
	}
	events = append(events, s.reevaluateFollowSurfaces(instanceID)...)
	return events
}

func (s *Service) ApplyInstanceDisconnected(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = false
	inst.ActiveTurnID = ""

	for _, surface := range s.root.Surfaces {
		if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID {
			continue
		}
		surface.PendingHeadless = nil
	}

	surfaces := s.findAttachedSurfaces(instanceID)
	if len(surfaces) == 0 {
		delete(s.instanceClaims, instanceID)
		delete(s.pendingRemote, instanceID)
		delete(s.activeRemote, instanceID)
		return nil
	}

	var events []control.UIEvent
	for _, surface := range surfaces {
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.ActiveTurnOrigin = ""
		surface.DispatchMode = state.DispatchModeNormal
		surface.Abandoning = false
		delete(s.handoffUntil, surface.SurfaceSessionID)
		clearSurfaceRequests(surface)

		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				events = append(events, s.failSurfaceActiveQueueItem(surface, item, &control.Notice{
					Code: "attached_instance_offline",
					Text: fmt.Sprintf("当前接管实例已离线：%s", inst.DisplayName),
				}, false)...)
			} else {
				surface.ActiveQueueItemID = ""
			}
		}

		events = append(events, s.finalizeDetachedSurface(surface)...)
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "attached_instance_offline",
				Text: fmt.Sprintf("当前接管实例已离线：%s", inst.DisplayName),
			},
		})
	}
	delete(s.instanceClaims, instanceID)
	delete(s.pendingRemote, instanceID)
	delete(s.activeRemote, instanceID)
	return events
}

func (s *Service) ApplyInstanceTransportDegraded(instanceID string, emitNotice bool) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = false
	inst.ActiveTurnID = ""

	delete(s.threadRefreshes, instanceID)

	surfaces := s.findAttachedSurfaces(instanceID)
	if len(surfaces) == 0 {
		delete(s.pendingRemote, instanceID)
		delete(s.activeRemote, instanceID)
		return nil
	}

	var events []control.UIEvent
	events = append(events, s.restorePendingSteersForInstance(instanceID)...)
	noticeText := fmt.Sprintf("当前接管实例链路过载，正在等待实例恢复：%s。当前 turn 可能继续执行，但实时输出可能延迟或丢失；如需放弃请 /detach。", inst.DisplayName)
	preserveRemoteOwnership := false
	for _, surface := range surfaces {
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.ActiveTurnOrigin = ""
		surface.DispatchMode = state.DispatchModeNormal
		surface.Abandoning = false
		delete(s.handoffUntil, surface.SurfaceSessionID)

		binding := s.remoteBindingForSurface(surface)
		if binding != nil && surface.ActiveQueueItemID != "" {
			item := surface.QueueItems[surface.ActiveQueueItemID]
			if item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				preserveRemoteOwnership = true
				events = append(events, appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
					QueueItemID: item.ID,
					Status:      string(item.Status),
				}, queueItemSourceMessageIDs(item)), item.SourceMessageID, false)...)
				if emitNotice {
					events = append(events, control.UIEvent{
						Kind:             control.UIEventNotice,
						SurfaceSessionID: surface.SurfaceSessionID,
						Notice: &control.Notice{
							Code: "attached_instance_transport_degraded",
							Text: noticeText,
						},
					})
				}
				continue
			}
		}
		clearSurfaceRequests(surface)
		if binding != nil {
			s.clearTurnArtifacts(binding.InstanceID, binding.ThreadID, binding.TurnID)
		}

		if surface.ActiveQueueItemID != "" {
			item := surface.QueueItems[surface.ActiveQueueItemID]
			if item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				var noticePtr *control.Notice
				if emitNotice {
					noticePtr = &control.Notice{
						Code: "attached_instance_transport_degraded",
						Text: noticeText,
					}
				}
				events = append(events, s.failSurfaceActiveQueueItem(surface, item, noticePtr, true)...)
				continue
			}
			surface.ActiveQueueItemID = ""
		}

		s.clearRemoteOwnership(surface)
		events = append(events, s.finishSurfaceAfterWork(surface)...)
		if emitNotice {
			events = append(events, control.UIEvent{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code: "attached_instance_transport_degraded",
					Text: noticeText,
				},
			})
		}
	}
	if !preserveRemoteOwnership {
		delete(s.pendingRemote, instanceID)
		delete(s.activeRemote, instanceID)
	}
	return events
}

func (s *Service) RemoveInstance(instanceID string) {
	if strings.TrimSpace(instanceID) == "" {
		return
	}
	if inst := s.root.Instances[instanceID]; inst != nil {
		inst.Online = false
		inst.ActiveTurnID = ""
	}
	s.restorePendingSteersForInstance(instanceID)
	for _, surface := range s.root.Surfaces {
		if surface == nil {
			continue
		}
		if surface.PendingHeadless != nil && surface.PendingHeadless.InstanceID == instanceID {
			surface.PendingHeadless = nil
		}
		if surface.AttachedInstanceID != instanceID {
			continue
		}
		s.discardDrafts(surface)
		surface.ActiveTurnOrigin = ""
		surface.Abandoning = false
		delete(s.handoffUntil, surface.SurfaceSessionID)
		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				s.failSurfaceActiveQueueItem(surface, item, nil, false)
			} else {
				s.clearRemoteOwnership(surface)
				surface.ActiveQueueItemID = ""
			}
		} else {
			s.clearRemoteOwnership(surface)
		}
		_ = s.finalizeDetachedSurface(surface)
	}
	delete(s.root.Instances, instanceID)
	delete(s.instanceClaims, instanceID)
	delete(s.pendingRemote, instanceID)
	delete(s.activeRemote, instanceID)
	delete(s.threadRefreshes, instanceID)
	deleteMatchingItemBuffers(s.itemBuffers, instanceID, "", "")
	for key, item := range s.pendingTurnText {
		if item == nil || item.InstanceID != instanceID {
			continue
		}
		delete(s.pendingTurnText, key)
	}
}

func (s *Service) observeConfig(inst *state.InstanceRecord, threadID, cwd, scope, model, effort string) {
	if inst == nil {
		return
	}
	switch scope {
	case "cwd_default":
		if cwd == "" {
			return
		}
		if inst.CWDDefaults == nil {
			inst.CWDDefaults = map[string]state.ModelConfigRecord{}
		}
		current := inst.CWDDefaults[cwd]
		if model != "" {
			current.Model = model
		}
		if effort != "" {
			current.ReasoningEffort = effort
		}
		inst.CWDDefaults[cwd] = current
	default:
		if threadID == "" {
			return
		}
		thread := s.ensureThread(inst, threadID)
		if cwd != "" {
			thread.CWD = cwd
		}
		if model != "" {
			thread.ExplicitModel = model
		}
		if effort != "" {
			thread.ExplicitReasoningEffort = effort
		}
	}
}

func (s *Service) discardDrafts(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	for _, image := range surface.StagedImages {
		if image.State != state.ImageStaged {
			continue
		}
		image.State = state.ImageDiscarded
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: image.ImageID,
			Status:      string(image.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{image.SourceMessageID})...)
	}
	for _, queueID := range append([]string{}, surface.QueuedQueueItemIDs...) {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued {
			continue
		}
		item.Status = state.QueueItemDiscarded
		s.markImagesForMessages(surface, queueItemSourceMessageIDs(item), state.ImageDiscarded)
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsDown:  true,
		}, queueItemSourceMessageIDs(item))...)
	}
	surface.QueuedQueueItemIDs = nil
	surface.StagedImages = map[string]*state.StagedImageRecord{}
	return events
}

func (s *Service) discardStagedImagesForRouteChange(surface *state.SurfaceConsoleRecord, prevThreadID string, prevRouteMode state.RouteMode, nextThreadID string, nextRouteMode state.RouteMode) []control.UIEvent {
	if surface == nil {
		return nil
	}
	prevThreadID = strings.TrimSpace(prevThreadID)
	nextThreadID = strings.TrimSpace(nextThreadID)
	if prevThreadID == nextThreadID && prevRouteMode == nextRouteMode {
		return nil
	}
	discarded := 0
	var events []control.UIEvent
	for imageID, image := range surface.StagedImages {
		if image == nil || image.State != state.ImageStaged {
			continue
		}
		image.State = state.ImageDiscarded
		discarded++
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: imageID,
			Status:      string(image.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{image.SourceMessageID})...)
		delete(surface.StagedImages, imageID)
	}
	if discarded == 0 {
		return nil
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "staged_images_discarded_on_route_change",
			Text: fmt.Sprintf("由于输入目标发生变化，已丢弃 %d 张尚未绑定的图片。", discarded),
		},
	})
	return events
}

func (s *Service) maybePromoteWorkspaceRoot(inst *state.InstanceRecord, cwd string) {
	if cwd == "" {
		return
	}
	switch {
	case inst.WorkspaceRoot == "":
		inst.WorkspaceRoot = cwd
	case strings.HasPrefix(inst.WorkspaceRoot, cwd+string(os.PathSeparator)):
		inst.WorkspaceRoot = cwd
	}
	inst.WorkspaceKey = inst.WorkspaceRoot
	inst.ShortName = filepath.Base(inst.WorkspaceKey)
	if inst.DisplayName == "" {
		inst.DisplayName = inst.ShortName
	}
}

func (s *Service) retargetManagedHeadlessInstance(inst *state.InstanceRecord, cwd string) {
	if inst == nil || strings.TrimSpace(cwd) == "" || !isHeadlessInstance(inst) {
		return
	}
	inst.WorkspaceRoot = cwd
	inst.WorkspaceKey = cwd
	inst.ShortName = filepath.Base(cwd)
	inst.DisplayName = inst.ShortName
}

func (s *Service) threadFocusEvents(instanceID, threadID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.maybeRequestThreadRefresh(surface, inst, threadID)...)
	}
	events = append(events, s.reevaluateFollowSurfaces(instanceID)...)
	return events
}

func (s *Service) bindSurfaceToThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	return s.bindSurfaceToThreadMode(surface, inst, threadID, state.RouteModePinned)
}

func (s *Service) bindSurfaceToThreadMode(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string, routeMode state.RouteMode) []control.UIEvent {
	if surface == nil || inst == nil || threadID == "" {
		return nil
	}
	thread := s.ensureThread(inst, threadID)
	if !threadVisible(thread) {
		return nil
	}
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimThread(surface, inst, threadID) {
		return nil
	}
	events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, threadID, routeMode)
	surface.SelectedThreadID = threadID
	s.clearPreparedNewThread(surface)
	surface.RouteMode = routeMode
	events = append(events, s.threadSelectionEvents(
		surface,
		threadID,
		string(surface.RouteMode),
		displayThreadTitle(inst, thread, threadID),
		threadPreview(thread),
	)...)
	return events
}

func (s *Service) threadSelectionEvents(surface *state.SurfaceConsoleRecord, threadID, routeMode, title, preview string) []control.UIEvent {
	if surface.LastSelection != nil &&
		surface.LastSelection.ThreadID == threadID &&
		surface.LastSelection.RouteMode == routeMode {
		surface.LastSelection.Title = title
		surface.LastSelection.Preview = preview
		return nil
	}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  threadID,
		RouteMode: routeMode,
		Title:     title,
		Preview:   preview,
	}
	return []control.UIEvent{threadSelectionEvent(surface, threadID, routeMode, title, preview)}
}

func notice(surface *state.SurfaceConsoleRecord, code, text string) []control.UIEvent {
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &control.Notice{Code: code, Text: text},
	}}
}

func commandCatalogEvent(surface *state.SurfaceConsoleRecord, catalog control.CommandCatalog) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventCommandCatalog,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		CommandCatalog:   &catalog,
	}
}

func (s *Service) HandleProblem(instanceID string, problem agentproto.ErrorInfo) []control.UIEvent {
	return s.handleProblem(instanceID, problem)
}

func (s *Service) handleProblem(instanceID string, problem agentproto.ErrorInfo) []control.UIEvent {
	problem = problem.Normalize()
	notice := NoticeForProblem(problem)
	surfaces := s.problemTargets(instanceID, problem)
	if len(surfaces) == 0 {
		if inst := s.root.Instances[instanceID]; inst != nil && strings.TrimSpace(problem.ThreadID) != "" {
			s.storeThreadReplayNotice(inst, problem.ThreadID, notice)
		}
		return nil
	}
	if inst := s.root.Instances[instanceID]; inst != nil && strings.TrimSpace(problem.ThreadID) != "" {
		s.clearThreadReplay(inst, problem.ThreadID)
	}
	events := make([]control.UIEvent, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		noticeCopy := notice
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &noticeCopy,
		})
	}
	return events
}

func (s *Service) problemTargets(instanceID string, problem agentproto.ErrorInfo) []*state.SurfaceConsoleRecord {
	if surface := s.root.Surfaces[problem.SurfaceSessionID]; surface != nil {
		return []*state.SurfaceConsoleRecord{surface}
	}
	if problem.CommandID != "" {
		for _, binding := range s.pendingRemote {
			if binding != nil && binding.CommandID == problem.CommandID {
				if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
					return []*state.SurfaceConsoleRecord{surface}
				}
			}
		}
		for _, binding := range s.activeRemote {
			if binding != nil && binding.CommandID == problem.CommandID {
				if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
					return []*state.SurfaceConsoleRecord{surface}
				}
			}
		}
	}
	if surface := s.turnSurface(instanceID, problem.ThreadID, problem.TurnID); surface != nil {
		return []*state.SurfaceConsoleRecord{surface}
	}
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	return s.findAttachedSurfaces(instanceID)
}

func commandAckProblem(surfaceID string, ack agentproto.CommandAck) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Code:             "command_rejected",
		Layer:            "wrapper",
		Stage:            "command_ack",
		Message:          "本地 Codex 拒绝了这条消息。",
		Details:          strings.TrimSpace(ack.Error),
		SurfaceSessionID: surfaceID,
		CommandID:        ack.CommandID,
	}
	if ack.Problem == nil {
		return defaults.Normalize()
	}
	return ack.Problem.WithDefaults(defaults)
}

func problemFromEvent(event agentproto.Event) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Message:   event.ErrorMessage,
		ThreadID:  event.ThreadID,
		TurnID:    event.TurnID,
		ItemID:    event.ItemID,
		RequestID: event.RequestID,
	}
	if event.Problem == nil {
		return defaults.Normalize()
	}
	return event.Problem.WithDefaults(defaults)
}
