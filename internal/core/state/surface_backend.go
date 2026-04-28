package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func NormalizeSurfaceBackend(mode ProductMode, backend agentproto.Backend) agentproto.Backend {
	if NormalizeProductMode(mode) == ProductModeVSCode {
		return agentproto.BackendCodex
	}
	return agentproto.NormalizeBackend(backend)
}

func EffectiveSurfaceBackend(surface *SurfaceConsoleRecord, inst *InstanceRecord) agentproto.Backend {
	if surface == nil {
		return agentproto.BackendCodex
	}
	mode := NormalizeProductMode(surface.ProductMode)
	if mode == ProductModeVSCode {
		return agentproto.BackendCodex
	}
	if inst != nil {
		return NormalizeSurfaceBackend(mode, EffectiveInstanceBackend(inst))
	}
	return NormalizeSurfaceBackend(mode, surface.Backend)
}

func SurfaceModeAlias(mode ProductMode, backend agentproto.Backend) string {
	if NormalizeProductMode(mode) == ProductModeVSCode {
		return "vscode"
	}
	switch NormalizeSurfaceBackend(mode, backend) {
	case agentproto.BackendClaude:
		return "claude"
	default:
		return "codex"
	}
}
