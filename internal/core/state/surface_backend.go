package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func NormalizeSurfaceBackend(mode ProductMode, backend agentproto.Backend) agentproto.Backend {
	if !IsHeadlessProductMode(mode) {
		return agentproto.BackendCodex
	}
	return agentproto.NormalizeBackend(backend)
}

func EffectiveSurfaceBackend(surface *SurfaceConsoleRecord, inst *InstanceRecord) agentproto.Backend {
	if surface == nil {
		return agentproto.BackendCodex
	}
	mode := NormalizeProductMode(surface.ProductMode)
	if !IsHeadlessProductMode(mode) {
		return agentproto.BackendCodex
	}
	if inst != nil {
		return NormalizeSurfaceBackend(mode, EffectiveInstanceBackend(inst))
	}
	return NormalizeSurfaceBackend(mode, surface.Backend)
}

// SurfaceModeAlias projects the stored runtime shape + backend pair back to the
// current user-visible mode names.
func SurfaceModeAlias(mode ProductMode, backend agentproto.Backend) string {
	if !IsHeadlessProductMode(mode) {
		return "vscode"
	}
	switch NormalizeSurfaceBackend(mode, backend) {
	case agentproto.BackendClaude:
		return "claude"
	default:
		return "codex"
	}
}
