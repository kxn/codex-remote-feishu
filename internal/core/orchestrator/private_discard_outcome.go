package orchestrator

import "github.com/kxn/codex-remote-feishu/internal/core/state"

// PrivateInputDiscardVersion is an in-process reduction outcome, never a
// persisted authority; the application commits the resulting boundary itself.
func (s *Service) PrivateInputDiscardVersion(surface string) uint64 {
	if s == nil || s.privateDiscardSurface != surface {
		return 0
	}
	return s.privateDiscardVersion
}
func (s *Service) recordPrivateInputDiscard(surface *state.SurfaceConsoleRecord) {
	if s == nil || !s.config.PrivateApplication || surface == nil {
		return
	}
	s.privateDiscardSurface = surface.SurfaceSessionID
	s.privateDiscardVersion++
}
