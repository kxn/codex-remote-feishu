package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"sort"
	"strings"
)

func (s *Service) enqueueQueueItem(surface *state.SurfaceConsoleRecord, sourceMessageID string, relatedMessageIDs []string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, front bool) []control.UIEvent {
	s.nextQueueItemID++
	itemID := fmt.Sprintf("queue-%d", s.nextQueueItemID)
	inst := s.root.Instances[surface.AttachedInstanceID]
	sourceMessageIDs := uniqueStrings(append([]string{sourceMessageID}, relatedMessageIDs...))
	item := &state.QueueItemRecord{
		ID:                 itemID,
		SurfaceSessionID:   surface.SurfaceSessionID,
		SourceMessageID:    sourceMessageID,
		SourceMessageIDs:   sourceMessageIDs,
		Inputs:             inputs,
		FrozenThreadID:     threadID,
		FrozenCWD:          cwd,
		FrozenOverride:     s.resolveFrozenPromptOverride(inst, surface, threadID, cwd, overrides),
		RouteModeAtEnqueue: routeMode,
		Status:             state.QueueItemQueued,
	}
	surface.QueueItems[item.ID] = item
	if front {
		surface.QueuedQueueItemIDs = append([]string{item.ID}, surface.QueuedQueueItemIDs...)
	} else {
		surface.QueuedQueueItemIDs = append(surface.QueuedQueueItemIDs, item.ID)
	}
	position := len(surface.QueuedQueueItemIDs)
	if front {
		position = 1
	}
	events := []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     item.ID,
			SourceMessageID: item.SourceMessageID,
			Status:          string(item.Status),
			QueuePosition:   position,
			QueueOn:         true,
		},
	}}
	return append(events, s.dispatchNext(surface)...)
}

func (s *Service) consumeStagedInputs(surface *state.SurfaceConsoleRecord) ([]agentproto.Input, []string) {
	keys := make([]string, 0, len(surface.StagedImages))
	for imageID := range surface.StagedImages {
		keys = append(keys, imageID)
	}
	sort.Strings(keys)

	var inputs []agentproto.Input
	var sourceMessageIDs []string
	for _, imageID := range keys {
		image := surface.StagedImages[imageID]
		if image.State != state.ImageStaged {
			continue
		}
		inputs = append(inputs, agentproto.Input{
			Type:     agentproto.InputLocalImage,
			Path:     image.LocalPath,
			MIMEType: image.MIMEType,
		})
		image.State = state.ImageBound
		sourceMessageIDs = append(sourceMessageIDs, image.SourceMessageID)
	}
	return inputs, sourceMessageIDs
}

func freezeRoute(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord) (threadID, cwd string, routeMode state.RouteMode, createThread bool) {
	switch {
	case surface.RouteMode == state.RouteModeNewThreadReady && strings.TrimSpace(surface.PreparedThreadCWD) != "":
		return "", surface.PreparedThreadCWD, state.RouteModeNewThreadReady, true
	case surface.RouteMode == state.RouteModeFollowLocal && surface.SelectedThreadID != "":
		threadID = surface.SelectedThreadID
		if thread := inst.Threads[threadID]; threadVisible(thread) {
			cwd = thread.CWD
			return threadID, cwd, state.RouteModeFollowLocal, false
		}
	case surface.RouteMode == state.RouteModePinned && surface.SelectedThreadID != "":
		threadID = surface.SelectedThreadID
		if thread := inst.Threads[threadID]; threadVisible(thread) {
			cwd = thread.CWD
			return threadID, cwd, state.RouteModePinned, false
		}
	}
	return "", inst.WorkspaceRoot, surface.RouteMode, false
}

func (s *Service) dispatchNext(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.DispatchMode != state.DispatchModeNormal || surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) == 0 {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.pendingRemote[inst.InstanceID] != nil {
		return nil
	}

	queueID := surface.QueuedQueueItemIDs[0]
	surface.QueuedQueueItemIDs = surface.QueuedQueueItemIDs[1:]
	item := surface.QueueItems[queueID]
	if item == nil || item.Status != state.QueueItemQueued {
		return nil
	}
	item.Status = state.QueueItemDispatching
	surface.ActiveQueueItemID = item.ID
	s.pendingRemote[inst.InstanceID] = &remoteTurnBinding{
		InstanceID:       inst.InstanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
		QueueItemID:      item.ID,
		SourceMessageID:  item.SourceMessageID,
		ThreadID:         item.FrozenThreadID,
		Status:           string(item.Status),
	}

	command := &agentproto.Command{
		Kind: agentproto.CommandPromptSend,
		Origin: agentproto.Origin{
			Surface:   surface.SurfaceSessionID,
			UserID:    surface.ActorUserID,
			ChatID:    surface.ChatID,
			MessageID: item.SourceMessageID,
		},
		Target: agentproto.Target{
			ThreadID:              item.FrozenThreadID,
			CWD:                   item.FrozenCWD,
			CreateThreadIfMissing: item.FrozenThreadID == "",
		},
		Prompt: agentproto.Prompt{
			Inputs: item.Inputs,
		},
		Overrides: agentproto.PromptOverrides{
			Model:           item.FrozenOverride.Model,
			ReasoningEffort: item.FrozenOverride.ReasoningEffort,
			AccessMode:      item.FrozenOverride.AccessMode,
		},
	}

	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, true)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command:          command,
	})
	return events
}

func (s *Service) markRemoteTurnRunning(instanceID string, initiator agentproto.Initiator, threadID, turnID string) []control.UIEvent {
	binding := s.promotePendingRemote(instanceID, initiator, threadID, turnID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	if item.FrozenThreadID == "" {
		item.FrozenThreadID = threadID
	}
	item.Status = state.QueueItemRunning
	events := []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     item.ID,
			SourceMessageID: item.SourceMessageID,
			Status:          string(item.Status),
		},
	}}
	if item.FrozenThreadID != "" {
		inst := s.root.Instances[instanceID]
		routeMode := item.RouteModeAtEnqueue
		if routeMode == "" || routeMode == state.RouteModeNewThreadReady {
			routeMode = state.RouteModePinned
		}
		events = append(events, s.bindSurfaceToThreadMode(surface, inst, item.FrozenThreadID, routeMode)...)
	}
	return events
}

func (s *Service) completeRemoteTurn(instanceID, threadID, turnID, status, errorMessage string, problem *agentproto.ErrorInfo) []control.UIEvent {
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	if status == "failed" || (status != "completed" && strings.TrimSpace(errorMessage) != "") {
		item.Status = state.QueueItemFailed
	} else {
		item.Status = state.QueueItemCompleted
	}
	surface.ActiveQueueItemID = ""
	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, false)
	if errorMessage != "" {
		if inst := s.root.Instances[instanceID]; inst != nil {
			s.clearThreadReplay(inst, threadID)
		}
		notice := &control.Notice{
			Code: "turn_failed",
			Text: errorMessage,
		}
		if problem != nil {
			problemNotice := NoticeForProblem(*problem)
			problemNotice.Code = "turn_failed"
			notice = &problemNotice
		}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	events = append(events, s.dispatchNext(surface)...)
	s.clearRemoteTurn(instanceID, turnID)
	events = append(events, s.finishSurfaceAfterWork(surface)...)
	return events
}

func (s *Service) renderTextItem(instanceID, threadID, turnID, itemID, text string, final bool) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil {
		if final {
			s.storeThreadReplayText(inst, threadID, turnID, itemID, text)
		}
		return nil
	}
	if final {
		s.clearThreadReplay(inst, threadID)
	}
	return s.renderTextToSurface(surface, inst, threadID, turnID, itemID, text, final)
}

func (s *Service) renderTextToSurface(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID, turnID, itemID, text string, final bool) []control.UIEvent {
	if surface == nil {
		return nil
	}
	events := []control.UIEvent{}
	if surface.ActiveTurnOrigin != agentproto.InitiatorLocalUI {
		routeMode := surface.RouteMode
		if routeMode != state.RouteModeFollowLocal {
			routeMode = state.RouteModePinned
		}
		if inst != nil {
			events = append(events, s.bindSurfaceToThreadMode(surface, inst, threadID, routeMode)...)
		}
	}
	instanceKey := ""
	if inst != nil {
		instanceKey = inst.InstanceID
	}
	blocks := s.renderer.PlanAssistantBlocks(surface.SurfaceSessionID, instanceKey, threadID, turnID, itemID, text)
	thread := (*state.ThreadRecord)(nil)
	if inst != nil {
		thread = s.ensureThread(inst, threadID)
	}
	title := displayThreadTitle(inst, thread, threadID)
	themeKey := threadID
	if themeKey == "" {
		themeKey = title
	}
	for i := range blocks {
		block := blocks[i]
		block.ThreadTitle = title
		block.ThemeKey = themeKey
		block.Final = final
		events = append(events, control.UIEvent{
			Kind:             control.UIEventBlockCommitted,
			SurfaceSessionID: surface.SurfaceSessionID,
			Block:            &block,
		})
	}
	if thread != nil {
		thread.Preview = previewOfText(text)
		s.touchThread(thread)
	}
	return events
}

func (s *Service) trackItemStart(instanceID string, event agentproto.Event) {
	if event.ItemID == "" || !tracksTextItem(event.ItemKind) {
		return
	}
	buf := s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	if text, _ := event.Metadata["text"].(string); text != "" {
		buf.Text = text
	}
}

func (s *Service) trackItemDelta(instanceID string, event agentproto.Event) {
	if event.ItemID == "" || event.Delta == "" || !tracksTextItem(event.ItemKind) {
		return
	}
	buf := s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	buf.Text += event.Delta
}

func (s *Service) completeItem(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.ItemID == "" {
		return nil
	}
	key := itemBufferKey(instanceID, event.ThreadID, event.TurnID, event.ItemID)
	buf := s.itemBuffers[key]
	if buf == nil {
		buf = s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	}
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	if text, _ := event.Metadata["text"].(string); text != "" {
		if buf.Text == "" || strings.TrimSpace(buf.Text) != strings.TrimSpace(text) {
			buf.Text = text
		}
		if buf.ItemKind == "" {
			buf.ItemKind = "agent_message"
		}
	}
	delete(s.itemBuffers, key)
	if !rendersTextItem(buf.ItemKind) || strings.TrimSpace(buf.Text) == "" {
		return nil
	}
	if buf.ItemKind == "agent_message" {
		return s.storePendingTurnText(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.ItemKind, buf.Text)
	}
	return s.renderTextItem(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.Text, false)
}

func (s *Service) storePendingTurnText(instanceID, threadID, turnID, itemID, itemKind, text string) []control.UIEvent {
	key := turnRenderKey(instanceID, threadID, turnID)
	previous := s.pendingTurnText[key]
	s.pendingTurnText[key] = &completedTextItem{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
		ItemID:     itemID,
		ItemKind:   itemKind,
		Text:       text,
	}
	if previous == nil {
		return nil
	}
	return s.renderTextItem(previous.InstanceID, previous.ThreadID, previous.TurnID, previous.ItemID, previous.Text, false)
}

func (s *Service) flushPendingTurnText(instanceID, threadID, turnID string, final bool) []control.UIEvent {
	key := turnRenderKey(instanceID, threadID, turnID)
	pending := s.pendingTurnText[key]
	if pending == nil {
		return nil
	}
	delete(s.pendingTurnText, key)
	return s.renderTextItem(pending.InstanceID, pending.ThreadID, pending.TurnID, pending.ItemID, pending.Text, final)
}

func (s *Service) flushPendingTurnTextIfTurnContinues(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.ThreadID == "" || event.TurnID == "" {
		return nil
	}
	if event.Kind == agentproto.EventTurnCompleted {
		return nil
	}
	key := turnRenderKey(instanceID, event.ThreadID, event.TurnID)
	pending := s.pendingTurnText[key]
	if pending == nil {
		return nil
	}
	switch event.Kind {
	case agentproto.EventItemStarted, agentproto.EventItemDelta, agentproto.EventItemCompleted:
		if event.ItemID == pending.ItemID {
			return nil
		}
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	case agentproto.EventRequestStarted, agentproto.EventRequestResolved:
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	default:
		return nil
	}
}

func (s *Service) normalizeTurnInitiator(instanceID string, event agentproto.Event) agentproto.Initiator {
	if event.Initiator.Kind != agentproto.InitiatorLocalUI && event.Initiator.Kind != agentproto.InitiatorUnknown {
		return event.Initiator
	}
	if binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID); binding != nil {
		return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: binding.SurfaceSessionID}
	}
	return event.Initiator
}

func queuedItemMatchesTurn(item *state.QueueItemRecord, threadID string) bool {
	if item == nil {
		return false
	}
	if item.FrozenThreadID != "" {
		return threadID == "" || threadID == item.FrozenThreadID
	}
	return threadID == ""
}

func (s *Service) pendingRemoteBinding(instanceID, threadID string) *remoteTurnBinding {
	binding := s.pendingRemote[instanceID]
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || (item.Status != state.QueueItemDispatching && item.Status != state.QueueItemRunning) {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	if !queuedItemMatchesTurn(item, threadID) {
		return nil
	}
	return binding
}

func (s *Service) promotePendingRemote(instanceID string, initiator agentproto.Initiator, threadID, turnID string) *remoteTurnBinding {
	binding := s.pendingRemoteBindingForInitiator(instanceID, initiator, threadID)
	if binding == nil {
		return s.activeRemoteBinding(instanceID, turnID)
	}
	delete(s.pendingRemote, instanceID)
	if threadID != "" {
		binding.ThreadID = threadID
	}
	binding.TurnID = turnID
	binding.Status = string(state.QueueItemRunning)
	s.activeRemote[instanceID] = binding
	return binding
}

func (s *Service) pendingRemoteBindingForInitiator(instanceID string, initiator agentproto.Initiator, threadID string) *remoteTurnBinding {
	if initiator.Kind == agentproto.InitiatorRemoteSurface && strings.TrimSpace(initiator.SurfaceSessionID) != "" {
		binding := s.pendingRemote[instanceID]
		if binding == nil {
			return nil
		}
		surface := s.root.Surfaces[binding.SurfaceSessionID]
		if surface == nil {
			delete(s.pendingRemote, instanceID)
			return nil
		}
		item := surface.QueueItems[binding.QueueItemID]
		if item == nil || (item.Status != state.QueueItemDispatching && item.Status != state.QueueItemRunning) {
			delete(s.pendingRemote, instanceID)
			return nil
		}
		if binding.SurfaceSessionID == initiator.SurfaceSessionID {
			return binding
		}
	}
	return s.pendingRemoteBinding(instanceID, threadID)
}

func (s *Service) activeRemoteBinding(instanceID, turnID string) *remoteTurnBinding {
	binding := s.activeRemote[instanceID]
	if binding == nil {
		return nil
	}
	if turnID != "" && binding.TurnID != "" && binding.TurnID != turnID {
		return nil
	}
	return binding
}

func (s *Service) lookupRemoteTurn(instanceID, threadID, turnID string) *remoteTurnBinding {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		if threadID == "" || binding.ThreadID == "" || binding.ThreadID == threadID {
			return binding
		}
	}
	return s.pendingRemoteBinding(instanceID, threadID)
}

func (s *Service) clearRemoteTurn(instanceID, turnID string) {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		delete(s.activeRemote, instanceID)
	}
	if binding := s.pendingRemote[instanceID]; binding != nil && (turnID == "" || binding.TurnID == turnID) {
		delete(s.pendingRemote, instanceID)
	}
}

func (s *Service) clearRemoteOwnership(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.AttachedInstanceID == "" {
		return
	}
	if binding := s.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.pendingRemote, surface.AttachedInstanceID)
	}
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.activeRemote, surface.AttachedInstanceID)
	}
}

func (s *Service) remoteBindingForSurface(surface *state.SurfaceConsoleRecord) *remoteTurnBinding {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	if binding := s.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	return nil
}
