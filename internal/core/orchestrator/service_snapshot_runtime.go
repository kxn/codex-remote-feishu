package orchestrator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

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

func (s *Service) MaterializeSurfaceResume(surfaceID, gatewayID, chatID, actorUserID string, mode state.ProductMode) {
	if strings.TrimSpace(surfaceID) == "" {
		return
	}
	surface := s.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        gatewayID,
		SurfaceSessionID: surfaceID,
		ChatID:           chatID,
		ActorUserID:      actorUserID,
	})
	if surface == nil {
		return
	}
	surface.ProductMode = state.NormalizeProductMode(mode)
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
	if events := s.restorePendingRequestDispatch(surface, commandID, "dispatch_failed"); len(events) != 0 {
		return events
	}
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
		if surface := s.findAttachedSurface(instanceID); surface != nil {
			return s.restorePendingRequestDispatch(surface, ack.CommandID, "command_rejected")
		}
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

func (s *Service) restorePendingRequestDispatch(surface *state.SurfaceConsoleRecord, commandID, noticeCode string) []control.UIEvent {
	if surface == nil || strings.TrimSpace(commandID) == "" {
		return nil
	}
	for _, request := range surface.PendingRequests {
		if request == nil || request.PendingDispatchCommandID != commandID {
			continue
		}
		request.PendingDispatchCommandID = ""
		bumpRequestCardRevision(request)
		noticeText := "请求提交失败，请在最新卡片上重试。"
		if noticeCode == "command_rejected" {
			noticeText = "本地 Codex 拒绝了这次请求提交，请在最新卡片上重试。"
		}
		return []control.UIEvent{
			s.requestPromptEvent(surface, request, ""),
			{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code: noticeCode,
					Text: noticeText,
				},
			},
		}
	}
	return nil
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
					Text: s.attachmentOfflineText(surface, inst),
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
				Text: s.attachmentOfflineText(surface, inst),
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
	noticeText := s.attachmentTransportDegradedText(nil, inst)
	preserveRemoteOwnership := false
	for _, surface := range surfaces {
		noticeText = s.attachmentTransportDegradedText(surface, inst)
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

func (s *Service) observeConfig(inst *state.InstanceRecord, threadID, cwd, scope, model, effort, access string) {
	if inst == nil {
		return
	}
	cwd = state.NormalizeWorkspaceKey(cwd)
	workspaceKey := state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot, cwd)
	cwdDefaultKey := firstNonEmpty(cwd, workspaceKey)
	access = agentproto.NormalizeAccessMode(access)
	switch scope {
	case "cwd_default":
		s.updateInstanceCWDDefaults(inst, cwdDefaultKey, func(current *state.ModelConfigRecord) {
			if model != "" {
				current.Model = model
			}
			if effort != "" {
				current.ReasoningEffort = effort
			}
			if access != "" {
				current.AccessMode = access
			}
		})
		if workspaceKey == "" || isVSCodeInstance(inst) {
			return
		}
		s.updateWorkspaceDefaults(workspaceKey, func(current *state.ModelConfigRecord) {
			if model != "" {
				current.Model = model
			}
			if effort != "" {
				current.ReasoningEffort = effort
			}
			if access != "" {
				current.AccessMode = access
			}
		})
	default:
		if threadID == "" && access == "" {
			return
		}
		if threadID != "" {
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
		if access != "" && isVSCodeInstance(inst) {
			s.updateInstanceCWDDefaults(inst, cwdDefaultKey, func(current *state.ModelConfigRecord) {
				current.AccessMode = access
			})
		}
		if access != "" && workspaceKey != "" && !isVSCodeInstance(inst) {
			s.updateWorkspaceDefaults(workspaceKey, func(current *state.ModelConfigRecord) {
				current.AccessMode = access
			})
		}
	}
}

func (s *Service) updateInstanceCWDDefaults(inst *state.InstanceRecord, cwd string, apply func(*state.ModelConfigRecord)) {
	cwd = state.NormalizeWorkspaceKey(cwd)
	if inst == nil || cwd == "" || apply == nil {
		return
	}
	if inst.CWDDefaults == nil {
		inst.CWDDefaults = map[string]state.ModelConfigRecord{}
	}
	current := compactModelConfig(inst.CWDDefaults[cwd])
	apply(&current)
	current = compactModelConfig(current)
	if modelConfigRecordEmpty(current) {
		delete(inst.CWDDefaults, cwd)
		return
	}
	inst.CWDDefaults[cwd] = current
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
	if inst == nil {
		return
	}
	cwd = state.NormalizeWorkspaceKey(cwd)
	if cwd == "" {
		return
	}
	currentRoot := state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
	switch {
	case currentRoot == "":
		currentRoot = cwd
	}
	inst.WorkspaceRoot = currentRoot
	inst.WorkspaceKey = state.ResolveWorkspaceKey(currentRoot)
	inst.ShortName = state.WorkspaceShortName(inst.WorkspaceKey)
	if inst.DisplayName == "" {
		inst.DisplayName = inst.ShortName
	}
}

func (s *Service) retargetManagedHeadlessInstance(inst *state.InstanceRecord, cwd string) {
	cwd = state.NormalizeWorkspaceKey(cwd)
	if inst == nil || cwd == "" || !isHeadlessInstance(inst) {
		return
	}
	inst.WorkspaceRoot = cwd
	inst.WorkspaceKey = state.ResolveWorkspaceKey(cwd)
	inst.ShortName = state.WorkspaceShortName(cwd)
	inst.DisplayName = inst.ShortName
}
