package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type PrivateHistoryKey struct {
	Backend            agentproto.Backend
	Namespace, Session string
}
type PrivateThreadPolicy struct {
	InstanceNamespaces map[string]string
	CatalogNamespaces  map[agentproto.Backend]string
	Denied             map[PrivateHistoryKey]bool
	ManagedDenied      map[PrivateHistoryKey]bool
	InternalDenied     map[PrivateHistoryKey]bool
	Ambiguous          map[PrivateHistoryKey]bool // Namespace empty: historical source has no proven root.
	Transferred        map[PrivateHistoryKey]bool
}

func (s *Service) EnablePrivateThreadPolicy() {
	if s.privateThreadPolicies == nil {
		s.privateThreadPolicies = map[string]PrivateThreadPolicy{}
	}
}

// SetPrivateThreadPolicy installs a precomputed owner-scoped snapshot. It must
// be called under the private Processor lock; policy contains no I/O closures.
func (s *Service) SetPrivateThreadPolicy(surface string, policy PrivateThreadPolicy) {
	if s.privateThreadPolicies == nil {
		s.privateThreadPolicies = map[string]PrivateThreadPolicy{}
	}
	s.privateThreadPolicies[surface] = policy
}
func (s *Service) privateThreadVisible(surface string, backend agentproto.Backend, instance, session string) bool {
	if !s.config.PrivateApplication {
		return true
	}
	if s.privateThreadPolicies == nil {
		return true
	}
	p, ok := s.privateThreadPolicies[surface]
	if !ok {
		return false
	}
	backend = agentproto.NormalizeBackend(backend)
	ns := ""
	if instance != "" {
		ns = p.InstanceNamespaces[instance]
	} else {
		ns = p.CatalogNamespaces[backend]
	}
	if ns == "" {
		return false
	}
	key := PrivateHistoryKey{Backend: backend, Namespace: ns, Session: session}
	if p.ManagedDenied[key] || p.InternalDenied[key] {
		return false
	}
	if p.Denied[key] || p.Ambiguous[PrivateHistoryKey{Backend: backend, Session: session}] {
		return p.Transferred[key]
	}
	return true
}
func (s *Service) threadVisibleForSurface(surface *state.SurfaceConsoleRecord, backend agentproto.Backend, instance, session string) bool {
	id := ""
	if surface != nil {
		id = surface.SurfaceSessionID
	}
	return s.privateThreadVisible(id, backend, instance, session)
}
