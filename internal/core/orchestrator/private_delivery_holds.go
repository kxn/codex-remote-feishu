package orchestrator

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RecordPrivateRequestDelivery also updates a frozen recovery hold; it never
// makes the held request live or changes its native execution authority.
func (s *Service) RecordPrivateRequestDelivery(report RequestDeliveryReport) error {
	v := s.root.Surfaces[report.SurfaceSessionID]
	if v == nil {
		return fmt.Errorf("private surface missing")
	}
	if len(v.PendingRequests) != 0 {
		s.RecordRequestPromptDelivery(report)
		return nil
	}
	held, ok := s.privateRecoveryHolds[report.SurfaceSessionID]
	if !ok {
		return nil
	}
	for i := range held.Holds {
		h := &held.Holds[i]
		if h.Kind != "requests" {
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
		request := data.Requests[report.RequestID]
		if request == nil {
			return nil
		}
		markRequestVisible(request, report.MessageID, report.DeliveredAt)
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		h.Frozen = raw
		s.privateRecoveryHolds[report.SurfaceSessionID] = held
		return nil
	}
	return nil
}

// RecordPrivateProgressDelivery updates either live progress or its frozen
// recovery evidence. A receipt must already establish the platform message.
func (s *Service) RecordPrivateProgressDelivery(surface, thread, turn, item, message string, start, end int, deleted bool) error {
	v := s.root.Surfaces[surface]
	if v == nil {
		return fmt.Errorf("private surface missing")
	}
	apply := func() {
		if deleted {
			s.ClearExecCommandProgressSegmentMessage(surface, thread, turn, item, message)
		} else {
			s.RecordExecCommandProgressSegmentWindow(surface, thread, turn, item, message, start, end)
		}
	}
	if v.ActiveExecProgress != nil {
		apply()
		return nil
	}
	held, ok := s.privateRecoveryHolds[surface]
	if !ok {
		return nil
	}
	for i := range held.Holds {
		h := &held.Holds[i]
		if h.Kind != "exec_progress" {
			continue
		}
		var progress state.ExecCommandProgressRecord
		if err := json.Unmarshal(h.Frozen, &progress); err != nil {
			return err
		}
		v.ActiveExecProgress = &progress
		apply()
		v.ActiveExecProgress = nil
		raw, err := json.Marshal(progress)
		if err != nil {
			return err
		}
		h.Frozen = raw
		s.privateRecoveryHolds[surface] = held
		return nil
	}
	return nil
}
