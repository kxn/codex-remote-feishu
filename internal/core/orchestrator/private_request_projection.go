package orchestrator

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RefreshPrivateRequestVisibility is called only after an authenticated,
// complete same-carrier inventory proved that this request is still pending.
// Its old card belongs to the previous daemon lifecycle and cannot serve as
// the new lifecycle's actionable projection.
func (s *Service) RefreshPrivateRequestVisibility(surfaceID, requestID string) error {
	if !s.config.PrivateApplication {
		return fmt.Errorf("invalid private request visibility owner")
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.PendingRequests[requestID] == nil {
		return fmt.Errorf("private request visibility proof mismatch")
	}
	request := surface.PendingRequests[requestID]
	request.VisibilityState = requestVisibilityDeliveryDegraded
	request.NeedsRedelivery = true
	return nil
}

// RestorePrivateRequestProjection restores only the request identified by an
// authenticated durable native fact; unrelated requests remain frozen.
func (s *Service) RestorePrivateRequestProjection(surfaceID, instanceID, requestID, commandID, threadID, turnID string) error {
	if !s.config.PrivateApplication || surfaceID == "" || instanceID == "" || requestID == "" {
		return fmt.Errorf("invalid private request proof")
	}
	v := s.root.Surfaces[surfaceID]
	if v == nil || v.AttachedInstanceID != instanceID {
		return fmt.Errorf("request owner mismatch")
	}
	if v.PendingRequests[requestID] != nil {
		return nil
	}
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		return nil
	}
	for i := range held.Holds {
		h := &held.Holds[i]
		if h.Kind != "requests" || h.InstanceID != instanceID {
			continue
		}
		var data struct {
			Requests map[string]*state.RequestPromptRecord
			Order    []string
			Capture  *state.RequestCaptureRecord
		}
		if err := json.Unmarshal(h.Frozen, &data); err != nil {
			return err
		}
		request := data.Requests[requestID]
		if request == nil {
			continue
		}
		if request.InstanceID != instanceID || request.OwnerSurfaceSessionID != surfaceID || threadID != "" && request.ThreadID != threadID || turnID != "" && request.TurnID != turnID || commandID != "" && request.PendingDispatchCommandID != commandID {
			return fmt.Errorf("request hold proof mismatch")
		}
		if v.PendingRequests == nil {
			v.PendingRequests = map[string]*state.RequestPromptRecord{}
		}
		v.PendingRequests[requestID] = request
		v.PendingRequestOrder = append(v.PendingRequestOrder, requestID)
		delete(data.Requests, requestID)
		remaining := make([]string, 0, len(data.Order))
		for _, id := range data.Order {
			if id != requestID {
				remaining = append(remaining, id)
			}
		}
		data.Order = remaining
		if data.Capture != nil && data.Capture.RequestID == requestID {
			v.ActiveRequestCapture = data.Capture
			data.Capture = nil
		}
		if len(data.Requests) == 0 && data.Capture == nil {
			held.Holds = append(held.Holds[:i], held.Holds[i+1:]...)
		} else {
			raw, err := json.Marshal(data)
			if err != nil {
				return err
			}
			h.Frozen = raw
		}
		s.privateRecoveryHolds[surfaceID] = held
		return nil
	}
	return nil
}
