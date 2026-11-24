package orchestrator

import (
	"fmt"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PrivateNavigationSnapshot records only an already selected thread's navigation
// identity. It does not assert that its carrier or any execution is alive.
type PrivateNativeProfiles struct{ Codex, Claude, OpenCode string }

type PrivateNavigationSnapshot struct {
	Owner                                              agentproto.NativeOwner
	ThreadID, CWD, WorkspaceKey                        string
	CodexProfileID, ClaudeProfileID, OpenCodeProfileID string
	Actual                                             PrivateNativeProfiles
}

func (s *Service) exportPrivateNavigation(v *state.SurfaceConsoleRecord) *PrivateNavigationSnapshot {
	if v == nil || v.RouteMode != state.RouteModePinned || v.AttachedInstanceID == "" || v.SelectedThreadID == "" {
		return nil
	}
	if inst := s.root.Instances[v.AttachedInstanceID]; inst != nil {
		if owner, ok := s.privateNativeOwners[v.SurfaceSessionID]; ok && owner.InstanceID == inst.InstanceID && owner.Backend == v.Backend {
			actual, known := s.privateNativeProfiles[v.SurfaceSessionID]
			if !known || !privateProfilesCompatible(v, actual) {
				return nil
			}
			if t := inst.Threads[v.SelectedThreadID]; t != nil && filepath.IsAbs(t.CWD) && s.surfaceOwnsThread(v, t.ThreadID) {
				return &PrivateNavigationSnapshot{Owner: owner, ThreadID: t.ThreadID, CWD: t.CWD, WorkspaceKey: t.WorkspaceKey, CodexProfileID: v.CodexProfileID, ClaudeProfileID: v.ClaudeProfileID, OpenCodeProfileID: v.OpenCodeProfileID, Actual: actual}
			}
		}
	}
	if x, ok := s.privateNavigation[v.SurfaceSessionID]; ok && x.Owner.InstanceID == v.AttachedInstanceID && x.ThreadID == v.SelectedThreadID && x.CodexProfileID == v.CodexProfileID && x.ClaudeProfileID == v.ClaudeProfileID && x.OpenCodeProfileID == v.OpenCodeProfileID {
		return &x
	}
	return nil
}

func (s *Service) restorePrivateNavigation(c PrivateCheckpoint) error {
	delete(s.privateNavigation, c.SurfaceID)
	x := c.Navigation
	if x == nil {
		return nil
	}
	if x.Owner.Validate() != nil || x.Owner.InstanceID != c.AttachedInstanceID || x.Owner.Backend != c.Backend || x.ThreadID == "" || x.ThreadID != c.SelectedThreadID || c.RouteMode != state.RouteModePinned || !filepath.IsAbs(x.CWD) || filepath.Clean(x.CWD) != x.CWD || x.CodexProfileID != c.CodexProfileID || x.ClaudeProfileID != c.ClaudeProfileID || x.OpenCodeProfileID != c.OpenCodeProfileID {
		return fmt.Errorf("invalid private navigation snapshot")
	}
	s.privateNavigation[c.SurfaceID] = *x
	return nil
}

// AuthenticatePrivateNavigation binds the snapshot to the exact live carrier
// before rebuilding navigation; it never restores active/online execution.
func (s *Service) AuthenticatePrivateNavigation(surface string, owner agentproto.NativeOwner) []eventcontract.Event {
	s.privateNativeOwners[surface] = owner
	x, ok := s.privateNavigation[surface]
	actual, known := s.privateNativeProfiles[surface]
	v := s.root.Surfaces[surface]
	inst := s.root.Instances[owner.InstanceID]
	if !ok || !known || x.Actual != actual || x.Owner != owner || v == nil || inst == nil || !inst.Online || v.AttachedInstanceID != owner.InstanceID || v.SelectedThreadID != x.ThreadID || v.RouteMode != state.RouteModePinned {
		return nil
	}
	events := s.ApplyAgentEvent(owner.InstanceID, agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: x.ThreadID, CWD: x.CWD})
	if !s.claimThread(v, inst, x.ThreadID) {
		return events
	}
	delete(s.privateReconnectQueries, surface)
	return events
}

// RestorePrivateNavigationFromEvent accepts an already verified, applied event
// from the exact authenticated carrier; callers must verify journal provenance.
func (s *Service) RestorePrivateNavigationFromEvent(surface string, owner agentproto.NativeOwner, event agentproto.Event) []eventcontract.Event {
	v := s.root.Surfaces[surface]
	inst := s.root.Instances[owner.InstanceID]
	actual, known := s.privateNativeProfiles[surface]
	if !known || v == nil || !privateProfilesCompatible(v, actual) || inst == nil || v.RouteMode != state.RouteModePinned || v.AttachedInstanceID != owner.InstanceID || v.SelectedThreadID == "" || (event.NativeBinding != nil && event.NativeBinding.Owner != owner) || event.Kind != agentproto.EventThreadDiscovered || event.ThreadID != v.SelectedThreadID || !filepath.IsAbs(event.CWD) || filepath.Clean(event.CWD) != event.CWD {
		return nil
	}
	s.privateNavigation[surface] = PrivateNavigationSnapshot{Owner: owner, ThreadID: event.ThreadID, CWD: event.CWD, WorkspaceKey: state.ResolveWorkspaceKey("", v.ClaimedWorkspaceKey, event.CWD), CodexProfileID: v.CodexProfileID, ClaudeProfileID: v.ClaudeProfileID, OpenCodeProfileID: v.OpenCodeProfileID, Actual: actual}
	return s.AuthenticatePrivateNavigation(surface, owner)
}

// SetPrivateNativeProfiles receives the frozen host record under the exact native lease.
func (s *Service) SetPrivateNativeProfiles(surface string, profiles PrivateNativeProfiles) {
	s.privateNativeProfiles[surface] = profiles
}

func privateProfilesCompatible(v *state.SurfaceConsoleRecord, p PrivateNativeProfiles) bool {
	if v == nil {
		return false
	}
	switch v.Backend {
	case agentproto.BackendCodex:
		return v.CodexProfileID == "" || v.CodexProfileID == p.Codex
	case agentproto.BackendClaude:
		return v.ClaudeProfileID == "" || v.ClaudeProfileID == p.Claude
	case agentproto.BackendOpenCode:
		return v.OpenCodeProfileID == "" || v.OpenCodeProfileID == p.OpenCode
	default:
		return false
	}
}
