package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"strings"
)

// HandlePrivateSteerAccepted projects a verified native turn/steer result.
func (s *Service) HandlePrivateSteerAccepted(instanceID, commandID string) []eventcontract.Event {
	if !s.config.PrivateApplication {
		return nil
	}
	return s.handleCommandAccepted(instanceID, commandID)
}

// HandlePrivateShellRequestAcknowledged projects native shell RPC receipt.
// It does not claim that the shell item has started or completed.
func (s *Service) HandlePrivateShellRequestAcknowledged(instanceID, commandID string) []eventcontract.Event {
	if !s.config.PrivateApplication {
		return nil
	}
	return s.handleCommandAccepted(instanceID, commandID)
}

// HandlePrivateReviewStartAcknowledged records the verified native review RPC result.
func (s *Service) HandlePrivateReviewStartAcknowledged(instanceID, commandID string) []eventcontract.Event {
	if !s.config.PrivateApplication {
		return nil
	}
	return s.handleCommandAccepted(instanceID, commandID)
}

// HandlePrivateRequestResponseWritten marks delivery to the local child;
// the prompt remains awaiting the separate native request-resolved event.
func (s *Service) HandlePrivateRequestResponseWritten(instanceID, commandID string) []eventcontract.Event {
	if !s.config.PrivateApplication {
		return nil
	}
	return s.handleCommandAccepted(instanceID, commandID)
}

func (s *Service) handleCommandAccepted(instanceID, commandID string) []eventcontract.Event {
	if commandID == "" {
		return nil
	}
	if s.acknowledgePendingReviewStart(commandID) {
		return nil
	}
	if surface, request := s.findPendingRequestByCommandID(commandID); surface != nil && request != nil {
		markRequestAwaitingBackendConsume(request)
		if requestPromptRenderable(request.RequestType) && strings.TrimSpace(request.VisibleMessageID) != "" {
			return []eventcontract.Event{s.requestPromptDeliveryEvent(surface, request, "")}
		}
		return nil
	}
	if key, binding := s.pendingShellForCommand(instanceID, commandID); binding != nil {
		surface := s.root.Surfaces[binding.SurfaceSessionID]
		if surface == nil {
			s.clearPendingShell(key)
			return nil
		}
		s.clearPendingShell(key)
		item := surface.QueueItems[binding.QueueItemID]
		if item == nil || item.Status != state.QueueItemShellCommanding {
			return nil
		}
		item.Status = state.QueueItemShellCommanded
		item.ShellCommandThreadID = binding.ThreadID
		item.ShellCommandTurnID = binding.TurnID
		events := s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			Applause:    true,
		}, []string{binding.SourceMessageID})
		return s.insertExecCommandProgressBoundary(binding.InstanceID, binding.ThreadID, binding.TurnID, events)
	}
	key, binding := s.pendingSteerForCommand(instanceID, commandID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		s.clearPendingSteer(key)
		return nil
	}
	s.clearPendingSteer(key)
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
