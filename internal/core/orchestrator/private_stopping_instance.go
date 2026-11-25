package orchestrator

import "strings"

// MarkPrivateInstanceStopping projects a checkpoint-committed kill intent;
// the private processor restores it from the durable outbox on restart.
func (s *Service) MarkPrivateInstanceStopping(instance string) {
	if !s.config.PrivateApplication || strings.TrimSpace(instance) == "" {
		return
	}
	if s.privateStoppingInstances == nil {
		s.privateStoppingInstances = map[string]bool{}
	}
	s.privateStoppingInstances[instance] = true
}

func (s *Service) privateInstanceStopping(instance string) bool {
	return s.config.PrivateApplication && s.privateStoppingInstances[instance]
}
