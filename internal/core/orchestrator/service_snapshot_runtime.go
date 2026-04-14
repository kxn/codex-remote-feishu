package orchestrator

import (
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

func (s *Service) MaterializeSurfaceResume(surfaceID, gatewayID, chatID, actorUserID string, mode state.ProductMode, verbosity state.SurfaceVerbosity) {
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
	surface.Verbosity = state.NormalizeSurfaceVerbosity(verbosity)
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
	delete(s.pendingSteers, key)
	events := []control.UIEvent{}
	for _, queueItemID := range pendingSteerQueueItemIDs(binding) {
		item := surface.QueueItems[queueItemID]
		if item == nil || item.Status != state.QueueItemSteering {
			continue
		}
		item.Status = state.QueueItemSteered
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsUp:    true,
		}, queueItemSourceMessageIDs(item))...)
	}
	return events
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
	restoreOrder := []struct {
		queueItemID string
		queueIndex  int
	}{}
	for _, queueItemID := range pendingSteerQueueItemIDs(binding) {
		item := surface.QueueItems[queueItemID]
		if item == nil || item.Status == state.QueueItemSteered {
			continue
		}
		item.Status = state.QueueItemQueued
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		queueIndex, ok := pendingSteerQueueIndex(binding, queueItemID)
		if !ok {
			queueIndex = len(surface.QueuedQueueItemIDs)
		}
		restoreOrder = append(restoreOrder, struct {
			queueItemID string
			queueIndex  int
		}{
			queueItemID: queueItemID,
			queueIndex:  queueIndex,
		})
	}
	sort.SliceStable(restoreOrder, func(i, j int) bool {
		if restoreOrder[i].queueIndex == restoreOrder[j].queueIndex {
			return restoreOrder[i].queueItemID < restoreOrder[j].queueItemID
		}
		return restoreOrder[i].queueIndex < restoreOrder[j].queueIndex
	})
	for _, restore := range restoreOrder {
		surface.QueuedQueueItemIDs = insertString(surface.QueuedQueueItemIDs, restore.queueIndex, restore.queueItemID)
	}
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

func pendingSteerQueueItemIDs(binding *pendingSteerBinding) []string {
	if binding == nil {
		return nil
	}
	ids := uniqueStrings(binding.QueueItemIDs)
	if len(ids) > 0 {
		return ids
	}
	if strings.TrimSpace(binding.QueueItemID) == "" {
		return nil
	}
	return []string{binding.QueueItemID}
}

func pendingSteerQueueIndex(binding *pendingSteerBinding, queueItemID string) (int, bool) {
	if binding == nil || strings.TrimSpace(queueItemID) == "" {
		return 0, false
	}
	if binding.QueueIndices != nil {
		if index, ok := binding.QueueIndices[queueItemID]; ok {
			return index, true
		}
	}
	if binding.QueueItemID == queueItemID {
		return binding.QueueIndex, true
	}
	return 0, false
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
	if pending.Purpose == state.HeadlessLaunchPurposeFreshWorkspace {
		notice := NoticeForProblem(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:             "workspace_create_start_failed",
			Layer:            "daemon",
			Stage:            "headless_start",
			Operation:        "create_workspace",
			Message:          "无法准备这个工作区。",
			SurfaceSessionID: surface.SurfaceSessionID,
			Retryable:        true,
		}))
		notice.Code = "workspace_create_start_failed"
		notice.Title = "工作区准备失败"
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &notice,
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
	deleteMatchingMCPToolCallProgress(s.mcpToolCallProgress, instanceID, "", "")
	for key, item := range s.pendingTurnText {
		if item == nil || item.InstanceID != instanceID {
			continue
		}
		delete(s.pendingTurnText, key)
	}
}
