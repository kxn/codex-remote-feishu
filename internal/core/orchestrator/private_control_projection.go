package orchestrator

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// RestorePrivateControlProjection restores only the exact frozen control
// projection after its independently verified durable native command fact.
func (s *Service) RestorePrivateControlProjection(surfaceID, instanceID, commandID string, kind agentproto.CommandKind, parentThread, observedThread, observedTurn string) error {
	if !s.config.PrivateApplication || surfaceID == "" || instanceID == "" || commandID == "" {
		return fmt.Errorf("invalid private control projection")
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.AttachedInstanceID != instanceID {
		return fmt.Errorf("control projection owner mismatch")
	}
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok {
		return nil
	}
	target := ""
	switch kind {
	case agentproto.CommandReviewStart:
		target = "pending_review"
	case agentproto.CommandThreadCompactStart:
		target = "compact"
	default:
		return fmt.Errorf("unsupported control projection")
	}
	index := -1
	reviewIndex := -1
	hasPendingReview := false
	for i, h := range held.Holds {
		if h.Kind == "pending_review" && h.InstanceID == instanceID {
			hasPendingReview = true
		}
		if h.Kind == target && h.InstanceID == instanceID && h.CommandID == commandID {
			if index >= 0 {
				return fmt.Errorf("ambiguous control hold")
			}
			index = i
		}
		if kind == agentproto.CommandReviewStart && observedThread != "" && h.Kind == "review_session" && h.InstanceID == instanceID {
			if reviewIndex >= 0 {
				return fmt.Errorf("ambiguous review session")
			}
			reviewIndex = i
		}
	}
	if index < 0 && (kind != agentproto.CommandReviewStart || reviewIndex < 0 || hasPendingReview) {
		return nil
	}

	if kind == agentproto.CommandThreadCompactStart {
		var binding compactTurnBinding
		if err := json.Unmarshal(held.Holds[index].Frozen, &binding); err != nil {
			return err
		}
		if binding.InstanceID != instanceID || binding.SurfaceSessionID != surfaceID || binding.CommandID != commandID {
			return fmt.Errorf("compact hold mismatch")
		}
		if !s.turns.restorePrivateCompact(instanceID, &binding) {
			return fmt.Errorf("compact already bound")
		}
	} else {
		if index >= 0 {
			var frozen string
			if err := json.Unmarshal(held.Holds[index].Frozen, &frozen); err != nil || frozen != commandID {
				return fmt.Errorf("review hold mismatch")
			}
		}
		if reviewIndex >= 0 {
			var review state.ReviewSessionRecord
			if err := json.Unmarshal(held.Holds[reviewIndex].Frozen, &review); err != nil {
				return err
			}
			if parentThread == "" || review.ParentThreadID != parentThread || review.ReviewThreadID != "" && review.ReviewThreadID != observedThread || review.ActiveTurnID != "" && review.ActiveTurnID != observedTurn || review.InitialTurnID != "" && review.InitialTurnID != observedTurn {
				return fmt.Errorf("review session proof mismatch")
			}
			surface.ReviewSession = &review
		}
		if index >= 0 {
			s.turns.bindPendingReview(commandID, surfaceID)
		}
	}
	retained := make([]PrivateRecoveryHold, 0, len(held.Holds))
	for i, h := range held.Holds {
		if i != index && i != reviewIndex {
			retained = append(retained, h)
		}
	}
	held.Holds = retained
	s.privateRecoveryHolds[surfaceID] = held
	return nil
}

// ProjectVerifiedPrivateReviewThread records a review relationship established
// by the caller's exact durable review acceptance proof before projecting its
// already-journaled native turn events.
func (s *Service) ProjectVerifiedPrivateReviewThread(surfaceID, instanceID, parentThread, reviewThread string) error {
	if !s.config.PrivateApplication || surfaceID == "" || instanceID == "" || parentThread == "" || reviewThread == "" || parentThread == reviewThread {
		return fmt.Errorf("invalid private review relationship")
	}
	surface := s.root.Surfaces[surfaceID]
	inst := s.root.Instances[instanceID]
	if surface == nil || inst == nil || surface.AttachedInstanceID != instanceID || surface.ReviewSession == nil || surface.ReviewSession.ParentThreadID != parentThread || surface.ReviewSession.ReviewThreadID != "" && surface.ReviewSession.ReviewThreadID != reviewThread {
		return fmt.Errorf("private review relationship mismatch")
	}
	thread := s.ensureThread(inst, reviewThread)
	thread.Source = &agentproto.ThreadSourceRecord{Kind: "review", ParentThreadID: parentThread}
	return nil
}
