package privatechat

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// ToolSurface is a detached value read under the processor's exclusive lock.
// The host must authenticate the carrier and hold its outbound lifetime gate
// separately; a surface ID supplied by the model is never a routing authority.
type ToolSurface struct {
	Owner                                                                        Owner
	InstanceID, ThreadID, WorkspaceKey, WorkspaceRoot, InstanceSource, RouteMode string
	Managed                                                                      bool
}

func (p *Processor) ToolSurface(ctx context.Context, owner Owner, generation int64, instanceID string) (ToolSurface, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return ToolSurface{}, ErrProcessorPoisoned
	}
	if _, err := p.ensureOwner(ctx, owner, generation); err != nil {
		return ToolSurface{}, err
	}
	s := p.app.Service()
	matched := false
	for _, turn := range append(s.ActiveRemoteTurns(), s.PendingRemoteTurns()...) {
		if turn.InstanceID == instanceID && turn.SurfaceSessionID == owner.SurfaceID {
			matched = true
		}
	}
	if !matched || p.app.instances[instanceID] != owner.SurfaceID {
		return ToolSurface{}, ErrInvalidOwner
	}
	inst := s.Instance(instanceID)
	var surface *state.SurfaceConsoleRecord
	for _, candidate := range s.Surfaces() {
		if candidate.SurfaceSessionID == owner.SurfaceID {
			surface = candidate
			break
		}
	}
	if inst == nil || surface == nil || surface.AttachedInstanceID != instanceID || state.NormalizeProductMode(surface.ProductMode) != state.ProductModeNormal {
		return ToolSurface{}, ErrInvalidOwner
	}
	return ToolSurface{Owner: owner, InstanceID: instanceID, ThreadID: surface.SelectedThreadID, WorkspaceKey: inst.WorkspaceKey, WorkspaceRoot: inst.WorkspaceRoot, InstanceSource: inst.Source, RouteMode: string(surface.RouteMode), Managed: inst.Managed}, nil
}
