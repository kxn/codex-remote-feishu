package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func EffectiveInstanceBackend(inst *InstanceRecord) agentproto.Backend {
	if inst == nil {
		return agentproto.BackendCodex
	}
	return agentproto.NormalizeBackend(inst.Backend)
}

func EffectiveInstanceCapabilities(inst *InstanceRecord) agentproto.Capabilities {
	if inst == nil {
		return agentproto.Capabilities{}
	}
	if inst.CapabilitiesDeclared {
		return inst.Capabilities
	}
	return agentproto.EffectiveCapabilitiesForBackend(inst.Backend, inst.Capabilities)
}

func InstanceSupportsThreadsRefresh(inst *InstanceRecord) bool {
	return inst != nil && EffectiveInstanceCapabilities(inst).ThreadsRefresh
}
