package orchestrator

import (
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func queueItemSteerInputs(item *state.QueueItemRecord) []agentproto.Input {
	if item == nil {
		return nil
	}
	if len(item.SteerInputs) != 0 {
		return append([]agentproto.Input(nil), item.SteerInputs...)
	}
	return append([]agentproto.Input(nil), item.Inputs...)
}

func (s *Service) maybeAutoSteerReply(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	replyTargetMessageID := strings.TrimSpace(action.TargetMessageID)
	sourceMessageID := strings.TrimSpace(action.MessageID)
	if replyTargetMessageID == "" || sourceMessageID == "" || len(action.SteerInputs) == 0 {
		return nil
	}
	inst, activeThreadID, activeTurnID, ok := s.activeReplySteerTarget(surface, replyTargetMessageID)
	if !ok {
		return nil
	}

	fullInputs := append([]agentproto.Input(nil), action.Inputs...)
	if len(fullInputs) == 0 {
		fullInputs = append(fullInputs, action.SteerInputs...)
	}
	if len(fullInputs) == 0 {
		return nil
	}

	s.nextQueueItemID++
	queueItemID := "queue-" + strconv.Itoa(s.nextQueueItemID)
	queueIndex := len(surface.QueuedQueueItemIDs)
	thread := inst.Threads[activeThreadID]
	cwd := strings.TrimSpace(firstNonEmpty(threadCWDFromRecord(thread), inst.WorkspaceRoot))
	item := &state.QueueItemRecord{
		ID:                         queueItemID,
		SurfaceSessionID:           surface.SurfaceSessionID,
		ActorUserID:                action.ActorUserID,
		SourceKind:                 state.QueueItemSourceUser,
		SourceMessageID:            sourceMessageID,
		SourceMessagePreview:       normalizeSourceMessagePreview(action.Text),
		SourceMessageIDs:           uniqueStrings([]string{sourceMessageID}),
		ReplyToMessageID:           sourceMessageID,
		ReplyToMessagePreview:      normalizeSourceMessagePreview(action.Text),
		Inputs:                     fullInputs,
		SteerInputs:                append([]agentproto.Input(nil), action.SteerInputs...),
		RestoreAsStagedImage:       action.Kind == control.ActionImageMessage,
		FrozenThreadID:             activeThreadID,
		FrozenCWD:                  cwd,
		FrozenExecutionMode:        defaultPromptExecutionModeForThread(activeThreadID),
		FrozenSourceThreadID:       "",
		FrozenSurfaceBindingPolicy: defaultSurfaceBindingPolicy(),
		FrozenOverride:             s.resolveFrozenPromptOverride(inst, surface, activeThreadID, cwd, surface.PromptOverride),
		RouteModeAtEnqueue:         surface.RouteMode,
		Status:                     state.QueueItemSteering,
	}
	surface.QueueItems[item.ID] = item
	if activeThreadID != "" {
		s.recordThreadUserMessage(inst, activeThreadID, action.Text)
	}
	s.turns.pendingSteers[item.ID] = &pendingSteerBinding{
		InstanceID:       inst.InstanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
		QueueItemID:      item.ID,
		QueueItemIDs:     []string{item.ID},
		QueueIndices:     map[string]int{item.ID: queueIndex},
		SourceMessageID:  sourceMessageID,
		ThreadID:         activeThreadID,
		TurnID:           activeTurnID,
		QueueIndex:       queueIndex,
	}
	events := s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOn:     true,
	}, []string{sourceMessageID})
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandTurnSteer,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    queuedItemActorUserID(item, surface),
				ChatID:    surface.ChatID,
				MessageID: sourceMessageID,
			},
			Target: agentproto.Target{
				ThreadID: activeThreadID,
				TurnID:   activeTurnID,
			},
			Prompt: agentproto.Prompt{
				Inputs: queueItemSteerInputs(item),
			},
		},
	})
	return events
}

func threadCWDFromRecord(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	return strings.TrimSpace(thread.CWD)
}

func (s *Service) activeReplySteerTarget(surface *state.SurfaceConsoleRecord, replyTargetMessageID string) (*state.InstanceRecord, string, string, bool) {
	if surface == nil || strings.TrimSpace(replyTargetMessageID) == "" {
		return nil, "", "", false
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online {
		return nil, "", "", false
	}
	binding := s.turns.activeRemote[surface.AttachedInstanceID]
	if binding == nil || binding.SurfaceSessionID != surface.SurfaceSessionID {
		return nil, "", "", false
	}
	activeThreadID := strings.TrimSpace(binding.ThreadID)
	activeTurnID := strings.TrimSpace(binding.TurnID)
	if activeThreadID == "" || activeTurnID == "" {
		return nil, "", "", false
	}
	if s.progress.isCompactTurn(inst.InstanceID, activeThreadID, activeTurnID) {
		return nil, "", "", false
	}
	if strings.TrimSpace(inst.ActiveThreadID) != activeThreadID || strings.TrimSpace(inst.ActiveTurnID) != activeTurnID {
		return nil, "", "", false
	}
	if strings.TrimSpace(surface.ActiveQueueItemID) == "" {
		return nil, "", "", false
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.Status != state.QueueItemRunning {
		return nil, "", "", false
	}
	if !queueItemHasSourceMessage(item, replyTargetMessageID) {
		return nil, "", "", false
	}
	return inst, activeThreadID, activeTurnID, true
}
