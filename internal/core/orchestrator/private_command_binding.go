package orchestrator

// BindPrivatePromptCommand binds only the prompt that produced the pending
// turn; owner-only catalog and history requests must not replace it.
func (s *Service) BindPrivatePromptCommand(surface, command string) {
	s.bindPendingRemoteCommand(s.root.Surfaces[surface], command)
}
func (s *Service) BindPrivateSteerCommand(surface, command string) {
	s.bindPendingSteerCommand(surface, command)
}
func (s *Service) BindPrivateCompactCommand(surface, command string) {
	s.bindPendingCompactCommand(s.root.Surfaces[surface], command)
}
