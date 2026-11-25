package orchestrator

import "github.com/kxn/codex-remote-feishu/internal/core/state"

// PrivateLiveReviewMatches only recognizes a previously projected active
// review. Live frames do not create a review or establish turn ownership.
func (s *Service) PrivateLiveReviewMatches(surface, instance, thread, root string) bool {
	if !s.config.PrivateApplication || surface == "" || instance == "" || thread == "" || root == "" {
		return false
	}
	v := s.root.Surfaces[surface]
	if v == nil || v.AttachedInstanceID != instance || v.ReviewSession == nil {
		return false
	}
	r := v.ReviewSession
	return r.Phase == state.ReviewSessionPhaseActive && r.ReviewThreadID == thread && r.ActiveTurnID == root
}
