package orchestrator

// PrivateHasOnlineAttachedCarrier reports only authenticated live core state;
// restored historical instance metadata is not considered online.
func (s *Service) PrivateHasOnlineAttachedCarrier(surfaceID string) bool {
	if !s.config.PrivateApplication {
		return false
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.AttachedInstanceID == "" {
		return false
	}
	instance := s.root.Instances[surface.AttachedInstanceID]
	return instance != nil && instance.Online
}
