package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// HandleKnownUnsent restores the exact dispatching prompt only when the host
// proved that the transport callback was never called. Unknown sends must not
// use this path. Steer/control retain their existing failure restoration.
func (s *Service) HandleKnownUnsent(surfaceID, commandID string, cause error) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		return s.HandleCommandDispatchFailure(surfaceID, commandID, cause)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	binding := s.remoteBindingForSurface(surface)
	if item == nil || item.Status != state.QueueItemDispatching || binding == nil || strings.TrimSpace(binding.CommandID) != commandID || binding.QueueItemID != item.ID {
		if _, steer := s.pendingSteerForCommand("", commandID); steer != nil {
			return s.HandleCommandDispatchFailure(surfaceID, commandID, cause)
		}
		if _, shell := s.pendingShellForCommand("", commandID); shell != nil {
			return s.HandleCommandDispatchFailure(surfaceID, commandID, cause)
		}
		if s.pendingReviewStartSurface(commandID) != nil {
			return s.HandleCommandDispatchFailure(surfaceID, commandID, cause)
		}
		return nil
	}
	item.Status = state.QueueItemQueued
	surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
	surface.QueuedQueueItemIDs = append([]string{item.ID}, surface.QueuedQueueItemIDs...)
	s.clearSurfaceActiveQueueItem(surface, item.ID)
	s.clearRemoteOwnership(surface)
	events := s.pendingInputEvents(surface, control.PendingInputState{QueueItemID: item.ID, Status: string(item.Status), TypingOff: true}, queueItemSourceMessageIDs(item))
	events = append(events, eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: surfaceID, Notice: &control.Notice{Code: "native_command_not_sent", Text: "命令未发送，输入已恢复到队列。"}})
	return events
}
