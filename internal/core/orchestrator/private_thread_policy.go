package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type PrivateHistoryKey struct {
	Backend            agentproto.Backend
	Namespace, Session string
}
type PrivateThreadPolicy struct {
	ExactRetired       map[PrivateHistoryKey]bool
	Unavailable        string
	InstanceNamespaces map[string]string
	InstanceProfiles   map[string]string
	LegacyProfile      string
	CatalogNamespaces  map[agentproto.Backend]string
	Denied             map[PrivateHistoryKey]bool
	ManagedDenied      map[PrivateHistoryKey]bool
	InternalDenied     map[PrivateHistoryKey]bool
	Ambiguous          map[PrivateHistoryKey]bool // Namespace empty: historical source has no proven root.
	LegacyReadOnly     map[PrivateHistoryKey]bool // Presentation allowed; source execution is not settled.
	LegacyAliases      map[PrivateHistoryKey]bool // Exact migrated source evidence; never a namespace wildcard.
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

// UpdatePrivateThreadCarrier only records the exact authenticated carrier. It
// deliberately does not establish authority to enumerate native history.
func (s *Service) UpdatePrivateThreadCarrier(surface, instance, namespace, profile string) {
	if s.privateThreadPolicies == nil {
		s.privateThreadPolicies = map[string]PrivateThreadPolicy{}
	}
	p, ok := s.privateThreadPolicies[surface]
	if !ok {
		p.Unavailable = "历史目录尚未核验"
	}
	if p.InstanceNamespaces == nil {
		p.InstanceNamespaces = map[string]string{}
	}
	if p.InstanceProfiles == nil {
		p.InstanceProfiles = map[string]string{}
	}
	p.InstanceNamespaces[instance], p.InstanceProfiles[instance] = namespace, profile
	s.privateThreadPolicies[surface] = p
}

// PrivateHistoryAction identifies operations which expose or select history.
func PrivateHistoryAction(a control.Action) bool {
	switch a.Kind {
	case control.ActionListInstances, control.ActionWorkspaceRoot, control.ActionWorkspaceList, control.ActionShowAllWorkspaces, control.ActionShowRecentWorkspaces, control.ActionAttachWorkspace, control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerBack, control.ActionShowThreads, control.ActionShowAllThreads, control.ActionShowScopedThreads, control.ActionShowWorkspaceThreads, control.ActionShowAllThreadWorkspaces, control.ActionShowRecentThreadWorkspaces, control.ActionUseThread, control.ActionThreadSelectionPage, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage, control.ActionTargetPickerConfirm:
		return true
	case control.ActionTextMessage:
		text := strings.TrimSpace(a.Text)
		return text == "/list" || strings.HasPrefix(text, "/list ") || text == "/use" || strings.HasPrefix(text, "/use ")
	}
	return false
}

// RecordPrivateRetiredSession records an exact registry observation solely for
// rebuilding that previously owned carrier; it does not authorize listing.
func (s *Service) RecordPrivateRetiredSession(surface string, backend agentproto.Backend, instance, namespace, session string) {
	if s.privateThreadPolicies == nil {
		s.privateThreadPolicies = map[string]PrivateThreadPolicy{}
	}
	p, ok := s.privateThreadPolicies[surface]
	if !ok {
		p.Unavailable = "历史目录尚未核验"
	}
	if p.InstanceNamespaces == nil {
		p.InstanceNamespaces = map[string]string{}
	}
	if p.ExactRetired == nil {
		p.ExactRetired = map[PrivateHistoryKey]bool{}
	}
	p.InstanceNamespaces[instance] = namespace
	if session != "" {
		p.ExactRetired[PrivateHistoryKey{Backend: backend, Namespace: namespace, Session: session}] = true
	}
	s.privateThreadPolicies[surface] = p
}

func (s *Service) privateThreadVisible(surface string, backend agentproto.Backend, instance, session string) bool {
	if !s.config.PrivateApplication {
		return true
	}
	if s.privateThreadPolicies == nil {
		return true
	}
	p, ok := s.privateThreadPolicies[surface]
	if !ok || p.Unavailable != "" {
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
		if p.Transferred[key] {
			return true
		}
		if !p.LegacyAliases[key] {
			return false
		}
		return instance == "" || (p.LegacyProfile != "" && state.NormalizeCodexProfileID(p.InstanceProfiles[instance]) == p.LegacyProfile)
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

// privateLegacyHistoryReadOnly applies only to a new headless restore. An
// already-owned online private carrier follows its ordinary ownership checks.
func (s *Service) privateLegacyHistoryReadOnly(surface *state.SurfaceConsoleRecord, view *mergedThreadView) bool {
	if !s.config.PrivateApplication || surface == nil || view == nil {
		return false
	}
	policy, ok := s.privateThreadPolicies[surface.SurfaceSessionID]
	if !ok {
		return false
	}
	backend := agentproto.NormalizeBackend(view.Backend)
	namespace := policy.CatalogNamespaces[backend]
	if view.Inst != nil && policy.InstanceNamespaces[view.Inst.InstanceID] != "" {
		namespace = policy.InstanceNamespaces[view.Inst.InstanceID]
	}
	return policy.LegacyReadOnly[PrivateHistoryKey{Backend: backend, Namespace: namespace, Session: view.ThreadID}]
}

// privateLegacySessionReadOnly reads only the already-frozen policy and
// in-memory route, so picker draft navigation never reopens the catalog.
func (s *Service) privateLegacySessionReadOnly(surface *state.SurfaceConsoleRecord, session string) bool {
	if !s.config.PrivateApplication || surface == nil {
		return false
	}
	p, ok := s.privateThreadPolicies[surface.SurfaceSessionID]
	if !ok {
		return false
	}
	if surface.SelectedThreadID == session && surface.AttachedInstanceID != "" {
		return false
	}
	for key, held := range p.LegacyReadOnly {
		if held && key.Session == session && key.Backend == agentproto.BackendCodex {
			return true
		}
	}
	return false
}
