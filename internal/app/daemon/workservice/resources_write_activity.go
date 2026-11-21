package workservice

import "sync"

// Process-local activity only suppresses premature recovery notifications. It
// is never proof of a remote outcome, a lease, or permission to retry a write.
type resourceCallActivity struct {
	mu     sync.Mutex
	active map[string]struct{}
}

func (a *resourceCallActivity) begin(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil {
		a.active = map[string]struct{}{}
	}
	a.active[id] = struct{}{}
}
func (a *resourceCallActivity) end(id string) { a.mu.Lock(); defer a.mu.Unlock(); delete(a.active, id) }
func (s *Service) ResourceWriteCallInFlight(id string) bool {
	if s == nil || s.resourceCalls == nil {
		return false
	}
	s.resourceCalls.mu.Lock()
	defer s.resourceCalls.mu.Unlock()
	_, ok := s.resourceCalls.active[id]
	return ok
}
