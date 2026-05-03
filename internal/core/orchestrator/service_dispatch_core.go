package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newRemoteTurnBindingForQueueItem(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, item *state.QueueItemRecord) *remoteTurnBinding {
	if surface == nil || inst == nil || item == nil {
		return nil
	}
	return &remoteTurnBinding{
		InstanceID:            inst.InstanceID,
		SurfaceSessionID:      surface.SurfaceSessionID,
		QueueItemID:           item.ID,
		SourceMessageID:       item.SourceMessageID,
		SourceMessagePreview:  item.SourceMessagePreview,
		ReplyToMessageID:      firstNonEmpty(item.ReplyToMessageID, item.SourceMessageID),
		ReplyToMessagePreview: firstNonEmpty(item.ReplyToMessagePreview, item.SourceMessagePreview),
		ExecutionMode:         item.FrozenExecutionMode,
		BootstrapNewThread:    item.RouteModeAtEnqueue == state.RouteModeNewThreadReady,
		ThreadID:              strings.TrimSpace(item.FrozenThreadID),
		SourceThreadID:        queuedItemSourceThreadID(item),
		SurfaceBindingPolicy:  queuedItemSurfaceBindingPolicy(item),
		ThreadCWD:             item.FrozenCWD,
		Status:                string(item.Status),
	}
}

func (s *Service) bindPendingRemoteTurn(instanceID string, binding *remoteTurnBinding) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || binding == nil {
		return
	}
	s.turns.pendingRemote[instanceID] = binding
}

func (s *Service) bindActiveRemoteTurn(instanceID string, binding *remoteTurnBinding) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || binding == nil {
		return
	}
	s.turns.activeRemote[instanceID] = binding
}

func (s *Service) clearPendingRemoteTurn(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	delete(s.turns.pendingRemote, instanceID)
}

func (s *Service) clearActiveRemoteTurn(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	delete(s.turns.activeRemote, instanceID)
}

func (s *Service) clearInstanceRemoteTurnOwnership(instanceID string) {
	s.clearPendingRemoteTurn(instanceID)
	s.clearActiveRemoteTurn(instanceID)
}

func (s *Service) setSurfaceActiveQueueItem(surface *state.SurfaceConsoleRecord, queueItemID string) {
	if surface == nil {
		return
	}
	surface.ActiveQueueItemID = strings.TrimSpace(queueItemID)
}

func (s *Service) clearSurfaceActiveQueueItem(surface *state.SurfaceConsoleRecord, queueItemID string) {
	if surface == nil {
		return
	}
	queueItemID = strings.TrimSpace(queueItemID)
	if queueItemID != "" && strings.TrimSpace(surface.ActiveQueueItemID) != queueItemID {
		return
	}
	surface.ActiveQueueItemID = ""
}

func (s *Service) activateSurfaceQueueItemDispatchWithBinding(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, binding *remoteTurnBinding) {
	if surface == nil || item == nil {
		return
	}
	item.Status = state.QueueItemDispatching
	s.setSurfaceActiveQueueItem(surface, item.ID)
	if binding != nil {
		binding.Status = string(item.Status)
		s.bindPendingRemoteTurn(binding.InstanceID, binding)
	}
}

func (s *Service) pauseSurfaceDispatchForLocal(surface *state.SurfaceConsoleRecord, until time.Time) bool {
	if surface == nil {
		return false
	}
	s.clearSurfaceDispatchWaits(surface)
	if !until.IsZero() {
		s.pausedUntil[surface.SurfaceSessionID] = until
	}
	if surface.DispatchMode == state.DispatchModePausedForLocal {
		return false
	}
	surface.DispatchMode = state.DispatchModePausedForLocal
	return true
}

func (s *Service) restoreSurfaceDispatchNormal(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	s.clearSurfaceDispatchWaits(surface)
	changed := surface.DispatchMode != state.DispatchModeNormal
	surface.DispatchMode = state.DispatchModeNormal
	return changed
}

func (s *Service) enterSurfaceDispatchHandoff(surface *state.SurfaceConsoleRecord, until time.Time) {
	if surface == nil {
		return
	}
	s.clearSurfaceDispatchWaits(surface)
	if len(surface.QueuedQueueItemIDs) == 0 {
		surface.DispatchMode = state.DispatchModeNormal
		return
	}
	surface.DispatchMode = state.DispatchModeHandoffWait
	if !until.IsZero() {
		s.handoffUntil[surface.SurfaceSessionID] = until
	}
}

func (s *Service) activateSurfaceQueueItemDispatch(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, item *state.QueueItemRecord) {
	if surface == nil || inst == nil || item == nil {
		return
	}
	s.activateSurfaceQueueItemDispatchWithBinding(surface, item, newRemoteTurnBindingForQueueItem(surface, inst, item))
}

func (s *Service) failSurfaceActiveQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, notice *control.Notice, tryDispatchNext bool) []eventcontract.Event {
	if surface == nil || item == nil {
		return nil
	}
	item.Status = state.QueueItemFailed
	s.clearSurfaceActiveQueueItem(surface, item.ID)
	binding := s.remoteBindingForSurface(surface)
	if binding != nil {
		s.clearTurnArtifacts(binding.InstanceID, binding.ThreadID, binding.TurnID)
	}
	s.clearRemoteOwnership(surface)
	if shouldRestorePreparedNewThread(surface, item, binding) {
		s.transitionSurfaceRouteCore(surface, s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)], surfaceRouteCoreState{
			AttachedInstanceID:   strings.TrimSpace(surface.AttachedInstanceID),
			RouteMode:            state.RouteModeNewThreadReady,
			PreparedThreadCWD:    strings.TrimSpace(surface.PreparedThreadCWD),
			PreparedFromThreadID: strings.TrimSpace(surface.PreparedFromThreadID),
		})
	}

	events := s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		TypingOff:   true,
	}, queueItemSourceMessageIDs(item))
	if notice != nil && (strings.TrimSpace(notice.Code) != "" || strings.TrimSpace(notice.Title) != "" || strings.TrimSpace(notice.Text) != "") {
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
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
