package orchestrator

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
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

func (s *Service) PendingRequest(surfaceID, requestID string) *state.RequestPromptRecord {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil
	}
	return pendingRequestRecord(surface, requestID)
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

func (s *Service) MaterializeSurfaceResume(surfaceID, gatewayID, chatID, actorUserID string, mode state.ProductMode, backend agentproto.Backend, claudeProfileID string, verbosity state.SurfaceVerbosity, planMode state.PlanModeSetting) {
	s.MaterializeSurfaceResumeWithCodexProvider(surfaceID, gatewayID, chatID, actorUserID, mode, backend, "", claudeProfileID, verbosity, planMode)
}

func (s *Service) MaterializeSurfaceResumeWithCodexProvider(surfaceID, gatewayID, chatID, actorUserID string, mode state.ProductMode, backend agentproto.Backend, codexProviderID, claudeProfileID string, verbosity state.SurfaceVerbosity, planMode state.PlanModeSetting) {
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
	s.setSurfaceDesiredContract(surface, state.SurfaceBackendContract{
		ProductMode:     mode,
		Backend:         backend,
		CodexProviderID: codexProviderID,
		ClaudeProfileID: claudeProfileID,
	})
	surface.Verbosity = state.NormalizeSurfaceVerbosity(verbosity)
	surface.PlanMode = state.NormalizePlanModeSetting(planMode)
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
		compact := s.turns.compactTurns[surface.AttachedInstanceID]
		if compact != nil && compact.SurfaceSessionID == surfaceID && compact.CommandID == "" {
			compact.CommandID = commandID
			return
		}
		binding := s.turns.pendingRemote[surface.AttachedInstanceID]
		if binding != nil && binding.SurfaceSessionID == surfaceID {
			if surface.ActiveQueueItemID != "" && binding.QueueItemID != surface.ActiveQueueItemID {
				return
			}
			binding.CommandID = commandID
			return
		}
	}
	for _, binding := range s.turns.pendingSteers {
		if binding == nil || binding.SurfaceSessionID != surfaceID || binding.CommandID != "" {
			continue
		}
		binding.CommandID = commandID
		return
	}
}

func (s *Service) HandleCommandDispatchFailure(surfaceID, commandID string, err error) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	if events := s.restorePendingCompactDispatch(surfaceID, commandID, "dispatch_failed", err); len(events) != 0 {
		return events
	}
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

func (s *Service) HandleCommandAccepted(instanceID string, ack agentproto.CommandAck) []eventcontract.Event {
	if ack.CommandID == "" {
		return nil
	}
	if surface, request := s.findPendingRequestByCommandID(ack.CommandID); surface != nil && request != nil {
		markRequestAwaitingBackendConsume(request)
		return nil
	}
	key, binding := s.pendingSteerForCommand(instanceID, ack.CommandID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.turns.pendingSteers, key)
		return nil
	}
	delete(s.turns.pendingSteers, key)
	events := []eventcontract.Event{}
	if strings.TrimSpace(binding.OwnerCardMessageID) != "" {
		events = append(events, steerAllCompletedOwnerCardEvent(surface.SurfaceSessionID, binding.OwnerCardMessageID, len(pendingSteerQueueItemIDs(binding))))
	}
	if supplement := s.steerUserSupplementEvent(surface, binding); supplement != nil {
		events = append(events, *supplement)
	}
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
	return s.insertExecCommandProgressBoundary(binding.InstanceID, binding.ThreadID, binding.TurnID, events)
}

func (s *Service) HandleCommandRejected(instanceID string, ack agentproto.CommandAck) []eventcontract.Event {
	if ack.CommandID == "" {
		return nil
	}
	if events := s.restorePendingCompactCommand(instanceID, ack.CommandID, commandAckProblem("", ack)); len(events) != 0 {
		return events
	}
	if key, binding := s.pendingSteerForCommand(instanceID, ack.CommandID); binding != nil {
		notice := NoticeForProblem(commandAckProblem(binding.SurfaceSessionID, ack))
		notice.Code = "steer_failed"
		notice.Text = appendSteerRestoreHint(notice.Text)
		return s.restorePendingSteer(key, &notice)
	}
	binding := s.turns.pendingRemote[instanceID]
	if binding == nil || binding.CommandID != ack.CommandID {
		if surface := s.findAttachedSurface(instanceID); surface != nil {
			return s.restorePendingRequestDispatch(surface, ack.CommandID, "command_rejected")
		}
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		s.clearPendingRemoteTurn(instanceID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || item.Status != state.QueueItemDispatching {
		s.clearPendingRemoteTurn(instanceID)
		return nil
	}
	notice := NoticeForProblem(commandAckProblem(surface.SurfaceSessionID, ack))
	notice.Code = "command_rejected"
	return s.failSurfaceActiveQueueItem(surface, item, &notice, true)
}

func (s *Service) restorePendingRequestDispatch(surface *state.SurfaceConsoleRecord, commandID, noticeCode string) []eventcontract.Event {
	if surface == nil || strings.TrimSpace(commandID) == "" {
		return nil
	}
	surface, request := s.findPendingRequestByCommandID(commandID)
	if request == nil || surface == nil {
		return nil
	}
	if normalizeRequestType(request.RequestType) == "tool_callback" {
		markRequestAwaitingBackendConsume(request)
		bumpRequestCardRevision(request)
		noticeText := "自动上报 unsupported 结果失败，当前 turn 可能仍在等待 callback。可使用 `/stop` 结束本轮，或等待本地 Codex 恢复后重试。"
		return []eventcontract.Event{
			s.requestPromptInlinePhaseEvent(surface, request, "", frontstagecontract.PhaseWaitingDispatch, noticeText),
			{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  strings.TrimSpace(request.SourceMessageID),
				Notice: &control.Notice{
					Code: noticeCode,
					Text: noticeText,
				},
			},
		}
	}
	markRequestVisibleEditing(request)
	bumpRequestCardRevision(request)
	noticeText := "请求提交失败，请在最新卡片上重试。"
	if noticeCode == "command_rejected" {
		noticeText = "本地 Codex 拒绝了这次请求提交，请在最新卡片上重试。"
	}
	if !requestPromptRenderable(request.RequestType) {
		return notice(surface, noticeCode, noticeText)
	}
	return s.requestPromptRefreshWithNotice(surface, request, noticeCode, noticeText)
}

func (s *Service) pendingSteerForCommand(instanceID, commandID string) (string, *pendingSteerBinding) {
	if strings.TrimSpace(commandID) == "" {
		return "", nil
	}
	for key, binding := range s.turns.pendingSteers {
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

func (s *Service) restorePendingSteer(key string, notice *control.Notice) []eventcontract.Event {
	binding := s.turns.pendingSteers[key]
	if binding == nil {
		return nil
	}
	delete(s.turns.pendingSteers, key)
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
		if s.restorePendingSteerAsStagedImage(surface, item) {
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
	var events []eventcontract.Event
	if strings.TrimSpace(binding.OwnerCardMessageID) != "" {
		text := ""
		if notice != nil {
			text = notice.Text
		}
		if strings.TrimSpace(text) == "" {
			text = "本地连接已断开，排队输入已恢复原位置。"
		}
		events = append(events, steerAllFailedOwnerCardEvent(surface.SurfaceSessionID, binding.OwnerCardMessageID, appendSteerRestoreHint(text)))
	}
	if notice != nil && (strings.TrimSpace(notice.Code) != "" || strings.TrimSpace(notice.Title) != "" || strings.TrimSpace(notice.Text) != "") {
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	events = append(events, s.dispatchNext(surface)...)
	events = append(events, s.finishSurfaceAfterWork(surface)...)
	return s.insertExecCommandProgressBoundary(binding.InstanceID, binding.ThreadID, binding.TurnID, events)
}

func (s *Service) restorePendingSteersForInstance(instanceID string) []eventcontract.Event {
	var events []eventcontract.Event
	keys := make([]string, 0, len(s.turns.pendingSteers))
	for key, binding := range s.turns.pendingSteers {
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

func (s *Service) restorePendingSteerAsStagedImage(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord) bool {
	if surface == nil || item == nil || !item.RestoreAsStagedImage {
		return false
	}
	if len(item.Inputs) != 1 || item.Inputs[0].Type != agentproto.InputLocalImage {
		return false
	}
	s.nextImageID++
	image := &state.StagedImageRecord{
		ImageID:          "img-" + strconv.Itoa(s.nextImageID),
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  item.SourceMessageID,
		ActorUserID:      strings.TrimSpace(firstNonEmpty(item.ActorUserID, surface.ActorUserID)),
		LocalPath:        item.Inputs[0].Path,
		MIMEType:         item.Inputs[0].MIMEType,
		State:            state.ImageStaged,
	}
	surface.StagedImages[image.ImageID] = image
	delete(surface.QueueItems, item.ID)
	return true
}

func (s *Service) HandleHeadlessLaunchStarted(surfaceID, instanceID string, pid int) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	pending := s.pendingSurfaceHeadlessLaunch(surface, instanceID)
	if pending == nil {
		return nil
	}
	pending.PID = pid
	return nil
}

func (s *Service) HandleHeadlessLaunchFailed(surfaceID, instanceID string, err error) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	pending := s.consumeSurfacePendingHeadlessLaunch(surface, instanceID)
	if pending == nil {
		return nil
	}
	if pending.AutoRestore {
		events := []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_restore_start_failed",
				Title: "恢复失败",
				Text:  "之前的会话暂时无法恢复，请稍后重试或尝试其他会话。",
			},
		}}
		return s.maybeFinalizePendingTargetPicker(surface, events, "之前的会话暂时无法恢复，请稍后重试或尝试其他会话。")
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
		events := []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &notice,
		}}
		return s.maybeFinalizePendingTargetPicker(surface, events, notice.Text)
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
	events := []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	}}
	return s.maybeFinalizePendingTargetPicker(surface, events, notice.Text)
}

func (s *Service) ApplyInstanceConnected(instanceID string) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = true

	var events []eventcontract.Event
	events = append(events, s.restorePendingSteersForInstance(instanceID)...)
	for _, surface := range s.root.Surfaces {
		pending := s.pendingSurfaceHeadlessLaunch(surface, instanceID)
		if pending == nil || pending.InstanceID != instanceID {
			continue
		}
		attachEvents := s.attachHeadlessInstance(surface, inst, pending)
		events = append(events, s.maybeFinalizePendingTargetPicker(surface, attachEvents, "")...)
	}
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.dispatchNext(surface)...)
	}
	events = append(events, s.reevaluateFollowSurfaces(instanceID)...)
	return events
}

func (s *Service) ApplyInstanceDisconnected(instanceID string) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = false
	inst.ActiveTurnID = ""
	events := s.failCompactTurn(instanceID, "当前实例已离线，上下文压缩已中断。", nil, false)

	for _, surface := range s.root.Surfaces {
		if s.consumeSurfacePendingHeadlessLaunch(surface, instanceID) == nil {
			continue
		}
		events = append(events, s.maybeFinalizePendingTargetPicker(surface, nil, "当前工作目标准备已中断，请重新发送 /list、/use 或 /useall 再试一次。")...)
	}

	surfaces := s.findAttachedSurfaces(instanceID)
	events = append(events, s.restorePendingSteersForInstance(instanceID)...)
	if len(surfaces) == 0 {
		delete(s.instanceClaims, instanceID)
		s.clearInstanceRemoteTurnOwnership(instanceID)
		return events
	}

	for _, surface := range surfaces {
		s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
		s.resetSurfaceExecutionGates(surface)
		clearSurfaceRequests(surface)

		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				events = append(events, s.failSurfaceActiveQueueItem(surface, item, &control.Notice{
					Code: "attached_instance_offline",
					Text: s.attachmentOfflineText(surface, inst),
				}, false)...)
			} else {
				s.clearSurfaceActiveQueueItem(surface, "")
			}
		}

		events = append(events, s.finalizeDetachedSurface(surface)...)
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "attached_instance_offline",
				Text: s.attachmentOfflineText(surface, inst),
			},
		})
	}
	delete(s.instanceClaims, instanceID)
	s.clearInstanceRemoteTurnOwnership(instanceID)
	return events
}

func (s *Service) ApplyInstanceTransportDegraded(instanceID string, emitNotice bool) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = false
	inst.ActiveTurnID = ""

	delete(s.threadRefreshes, instanceID)

	surfaces := s.findAttachedSurfaces(instanceID)
	events := s.failCompactTurn(instanceID, "当前实例连接已中断，上下文压缩已中断。", nil, false)
	events = append(events, s.restorePendingSteersForInstance(instanceID)...)
	if len(surfaces) == 0 {
		s.clearInstanceRemoteTurnOwnership(instanceID)
		return events
	}

	noticeText := s.attachmentTransportDegradedText(nil, inst)
	preserveRemoteOwnership := false
	for _, surface := range surfaces {
		noticeText = s.attachmentTransportDegradedText(surface, inst)
		s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
		s.resetSurfaceExecutionGates(surface)

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
					notice := globalRuntimeNotice(control.NoticeDeliveryFamilyTransportDegraded, "attached_instance_transport_degraded", "", noticeText)
					events = append(events, eventcontract.Event{
						Kind:             eventcontract.KindNotice,
						SurfaceSessionID: surface.SurfaceSessionID,
						Notice:           &notice,
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
					notice := globalRuntimeNotice(control.NoticeDeliveryFamilyTransportDegraded, "attached_instance_transport_degraded", "", noticeText)
					noticePtr = &notice
				}
				events = append(events, s.failSurfaceActiveQueueItem(surface, item, noticePtr, true)...)
				continue
			}
			s.clearSurfaceActiveQueueItem(surface, "")
		}

		s.clearRemoteOwnership(surface)
		events = append(events, s.finishSurfaceAfterWork(surface)...)
		if emitNotice {
			notice := globalRuntimeNotice(control.NoticeDeliveryFamilyTransportDegraded, "attached_instance_transport_degraded", "", noticeText)
			events = append(events, eventcontract.Event{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           &notice,
			})
		}
	}
	if !preserveRemoteOwnership {
		s.clearInstanceRemoteTurnOwnership(instanceID)
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
	_ = s.failCompactTurn(instanceID, "当前实例已移除，上下文压缩已中断。", nil, false)
	s.restorePendingSteersForInstance(instanceID)
	for _, surface := range s.root.Surfaces {
		if surface == nil {
			continue
		}
		s.consumeSurfacePendingHeadlessLaunch(surface, instanceID)
		if surface.AttachedInstanceID != instanceID {
			continue
		}
		s.discardDrafts(surface)
		s.resetSurfaceExecutionGates(surface)
		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				s.failSurfaceActiveQueueItem(surface, item, nil, false)
			} else {
				s.clearRemoteOwnership(surface)
				s.clearSurfaceActiveQueueItem(surface, "")
			}
		} else {
			s.clearRemoteOwnership(surface)
		}
		_ = s.finalizeDetachedSurface(surface)
	}
	delete(s.root.Instances, instanceID)
	delete(s.instanceClaims, instanceID)
	s.clearInstanceRemoteTurnOwnership(instanceID)
	delete(s.threadRefreshes, instanceID)
	deleteMatchingItemBuffers(s.itemBuffers, instanceID, "", "")
	deleteMatchingMCPToolCallProgress(s.progress.mcpToolCallProgress, instanceID, "", "")
	for key, item := range s.progress.pendingTurnText {
		if item == nil || item.InstanceID != instanceID {
			continue
		}
		delete(s.progress.pendingTurnText, key)
	}
	for key, item := range s.progress.pendingPlanProposal {
		if item == nil || item.InstanceID != instanceID {
			continue
		}
		delete(s.progress.pendingPlanProposal, key)
	}
}
