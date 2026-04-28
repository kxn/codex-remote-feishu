package agentproto

import "strings"

type Backend string

const (
	BackendCodex  Backend = "codex"
	BackendClaude Backend = "claude"
)

func NormalizeBackend(value Backend) Backend {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(BackendClaude):
		return BackendClaude
	default:
		return BackendCodex
	}
}

func DefaultCapabilitiesForBackend(backend Backend) Capabilities {
	switch NormalizeBackend(backend) {
	case BackendClaude:
		return Capabilities{
			RequestRespond: true,
		}
	default:
		return Capabilities{
			ThreadsRefresh:   true,
			TurnSteer:        true,
			RequestRespond:   true,
			ResumeByThreadID: true,
			VSCodeMode:       true,
		}
	}
}

func EffectiveCapabilitiesForBackend(backend Backend, caps Capabilities) Capabilities {
	base := DefaultCapabilitiesForBackend(backend)
	if caps.ThreadsRefresh {
		base.ThreadsRefresh = true
	}
	if caps.TurnSteer {
		base.TurnSteer = true
	}
	if caps.RequestRespond {
		base.RequestRespond = true
	}
	if caps.SessionCatalog {
		base.SessionCatalog = true
	}
	if caps.ResumeByThreadID {
		base.ResumeByThreadID = true
	}
	if caps.RequiresCWDForResume {
		base.RequiresCWDForResume = true
	}
	if caps.VSCodeMode {
		base.VSCodeMode = true
	}
	return base
}

func EffectiveHelloBackend(hello Hello) Backend {
	return NormalizeBackend(hello.Instance.Backend)
}

func EffectiveHelloCapabilities(hello Hello) Capabilities {
	if hello.CapabilitiesDeclared {
		return hello.Capabilities
	}
	return EffectiveCapabilitiesForBackend(hello.Instance.Backend, hello.Capabilities)
}
