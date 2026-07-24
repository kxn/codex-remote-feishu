package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceRouteThreadClaimPolicy int

const (
	surfaceRouteThreadClaimNone surfaceRouteThreadClaimPolicy = iota
	surfaceRouteThreadClaimKnown
	surfaceRouteThreadClaimVisible
)

type surfaceRouteCoreState struct {
	AttachedInstanceID   string
	WorkspaceKey         string
	RouteMode            state.RouteMode
	SelectedThreadID     string
	PreparedThreadCWD    string
	PreparedFromThreadID string
	ThreadClaimPolicy    surfaceRouteThreadClaimPolicy
}

func surfaceUsesWorkspaceClaimsRaw(surface *state.SurfaceConsoleRecord) bool {
	return surface != nil && state.IsHeadlessProductMode(state.NormalizeProductMode(surface.ProductMode))
}

func (s *Service) surfaceCurrentWorkspaceKeyRaw(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || !surfaceUsesWorkspaceClaimsRaw(surface) {
		return ""
	}
	if key := normalizeWorkspaceClaimKey(surface.ClaimedWorkspaceKey); key != "" {
		return key
	}
	if pending := surface.PendingHeadless; pending != nil {
		if key := normalizeWorkspaceClaimKey(firstNonEmpty(pending.WorkspaceKey, pending.ThreadCWD)); key != "" {
			return key
		}
	}
	if key := normalizeWorkspaceClaimKey(surface.PreparedThreadCWD); key != "" {
		return key
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		return instanceWorkspaceClaimKey(inst)
	}
	return ""
}

func (s *Service) transitionSurfaceRouteCore(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, next surfaceRouteCoreState) bool {
	next, inst, ok := s.normalizeSurfaceRouteCoreState(surface, inst, next)
	if !ok || !s.canTransitionSurfaceRouteCore(surface, inst, next) {
		return false
	}

	currentAttachedInstanceID := strings.TrimSpace(surface.AttachedInstanceID)
	sameAttachment := next.AttachedInstanceID != "" && currentAttachedInstanceID != "" && currentAttachedInstanceID == next.AttachedInstanceID
	currentWorkspaceKey := s.surfaceCurrentWorkspaceKeyRaw(surface)
	sameWorkspaceClaim := sameAttachment && surfaceUsesWorkspaceClaimsRaw(surface) && currentWorkspaceKey != "" && currentWorkspaceKey == next.WorkspaceKey

	s.releaseSurfaceThreadClaim(surface)
	if !sameAttachment || next.AttachedInstanceID == "" {
		s.releaseSurfaceInstanceClaim(surface)
	}
	if !sameWorkspaceClaim || next.AttachedInstanceID == "" || !surfaceUsesWorkspaceClaimsRaw(surface) {
		s.releaseSurfaceWorkspaceClaim(surface)
	}

	surface.AttachedInstanceID = next.AttachedInstanceID
	surface.RouteMode = next.RouteMode
	surface.SelectedThreadID = next.SelectedThreadID
	surface.PreparedThreadCWD = next.PreparedThreadCWD
	surface.PreparedFromThreadID = next.PreparedFromThreadID
	if next.PreparedThreadCWD == "" {
		surface.PreparedAt = time.Time{}
	}

	switch {
	case next.AttachedInstanceID != "" && surfaceUsesWorkspaceClaimsRaw(surface):
		surface.ClaimedWorkspaceKey = next.WorkspaceKey
		if !sameWorkspaceClaim {
			s.bindWorkspaceClaim(surface, next.WorkspaceKey)
		}
	case next.WorkspaceKey != "":
		surface.ClaimedWorkspaceKey = next.WorkspaceKey
	default:
		surface.ClaimedWorkspaceKey = ""
	}

	if next.AttachedInstanceID != "" && !sameAttachment {
		s.bindInstanceClaim(surface, next.AttachedInstanceID)
	}
	if next.SelectedThreadID != "" {
		s.bindThreadClaim(surface, next.AttachedInstanceID, next.SelectedThreadID)
	}
	return true
}

func (s *Service) normalizeSurfaceRouteCoreState(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, next surfaceRouteCoreState) (surfaceRouteCoreState, *state.InstanceRecord, bool) {
	if surface == nil {
		return surfaceRouteCoreState{}, nil, false
	}

	next.AttachedInstanceID = strings.TrimSpace(next.AttachedInstanceID)
	next.WorkspaceKey = normalizeWorkspaceClaimKey(next.WorkspaceKey)
	next.SelectedThreadID = strings.TrimSpace(next.SelectedThreadID)
	next.PreparedThreadCWD = strings.TrimSpace(next.PreparedThreadCWD)
	next.PreparedFromThreadID = strings.TrimSpace(next.PreparedFromThreadID)

	if next.AttachedInstanceID == "" {
		next.RouteMode = state.RouteModeUnbound
		next.SelectedThreadID = ""
		next.PreparedThreadCWD = ""
		next.PreparedFromThreadID = ""
		next.ThreadClaimPolicy = surfaceRouteThreadClaimNone
		if !surfaceUsesWorkspaceClaimsRaw(surface) {
			next.WorkspaceKey = ""
		}
		return next, nil, true
	}

	if inst == nil || strings.TrimSpace(inst.InstanceID) != next.AttachedInstanceID {
		inst = s.root.Instances[next.AttachedInstanceID]
	}
	if inst == nil {
		return surfaceRouteCoreState{}, nil, false
	}

	if !surfaceUsesWorkspaceClaimsRaw(surface) {
		next.WorkspaceKey = ""
	} else if next.WorkspaceKey == "" {
		if key := normalizeWorkspaceClaimKey(surface.ClaimedWorkspaceKey); key != "" {
			next.WorkspaceKey = key
		}
		if next.WorkspaceKey == "" && next.PreparedThreadCWD != "" {
			next.WorkspaceKey = normalizeWorkspaceClaimKey(next.PreparedThreadCWD)
		}
		if next.WorkspaceKey == "" {
			next.WorkspaceKey = instanceWorkspaceClaimKey(inst)
		}
	}

	switch next.RouteMode {
	case state.RouteModePinned:
		if next.SelectedThreadID == "" || next.PreparedThreadCWD != "" || next.PreparedFromThreadID != "" {
			return surfaceRouteCoreState{}, nil, false
		}
	case state.RouteModeFollowLocal:
		if next.PreparedThreadCWD != "" || next.PreparedFromThreadID != "" {
			return surfaceRouteCoreState{}, nil, false
		}
	case state.RouteModeNewThreadReady:
		if next.SelectedThreadID != "" || next.PreparedThreadCWD == "" {
			return surfaceRouteCoreState{}, nil, false
		}
	case state.RouteModeUnbound:
		if next.SelectedThreadID != "" || next.PreparedThreadCWD != "" || next.PreparedFromThreadID != "" {
			return surfaceRouteCoreState{}, nil, false
		}
	default:
		return surfaceRouteCoreState{}, nil, false
	}

	if next.SelectedThreadID == "" {
		next.ThreadClaimPolicy = surfaceRouteThreadClaimNone
	} else if next.ThreadClaimPolicy == surfaceRouteThreadClaimNone {
		return surfaceRouteCoreState{}, nil, false
	}
	if surfaceUsesWorkspaceClaimsRaw(surface) && next.WorkspaceKey == "" {
		return surfaceRouteCoreState{}, nil, false
	}
	return next, inst, true
}

func (s *Service) canTransitionSurfaceRouteCore(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, next surfaceRouteCoreState) bool {
	if surface == nil {
		return false
	}
	if next.AttachedInstanceID == "" {
		return true
	}
	currentAttachedInstanceID := strings.TrimSpace(surface.AttachedInstanceID)
	sameAttachment := currentAttachedInstanceID != "" && currentAttachedInstanceID == next.AttachedInstanceID
	if surfaceUsesWorkspaceClaimsRaw(surface) && !sameAttachment {
		claimOwner := s.workspaceClaimOwner(next.WorkspaceKey)
		if claimOwner.valid() && !claimOwner.same(s.workspaceClaimOwnerForSurface(surface)) {
			return false
		}
	}
	if !sameAttachment {
		if owner := s.instanceClaimSurface(next.AttachedInstanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
			return false
		}
	}
	if next.SelectedThreadID == "" {
		return true
	}
	if surfaceUsesWorkspaceClaimsRaw(surface) && inst != nil && headlessThreadWorkspaceMustMatch(inst) {
		if thread := inst.Threads[next.SelectedThreadID]; thread != nil && threadVisible(thread) && !threadBelongsToInstanceWorkspace(inst, thread) {
			return false
		}
	}
	if owner := s.threadClaimSurface(next.SelectedThreadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return false
	}
	switch next.ThreadClaimPolicy {
	case surfaceRouteThreadClaimKnown:
		return true
	case surfaceRouteThreadClaimVisible:
		if inst == nil || !threadVisible(inst.Threads[next.SelectedThreadID]) {
			return false
		}
		if headlessThreadWorkspaceMustMatch(inst) {
			return threadBelongsToInstanceWorkspace(inst, inst.Threads[next.SelectedThreadID])
		}
		return true
	default:
		return false
	}
}
