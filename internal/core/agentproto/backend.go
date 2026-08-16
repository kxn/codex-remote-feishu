package agentproto

import "strings"

type Backend string

const (
	BackendCodex    Backend = "codex"
	BackendClaude   Backend = "claude"
	BackendOpenCode Backend = "opencode"
)

func NormalizeBackend(value Backend) Backend {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(BackendClaude):
		return BackendClaude
	case string(BackendOpenCode):
		return BackendOpenCode
	default:
		return BackendCodex
	}
}

func ParseBackend(value Backend) (Backend, bool) {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "":
		return BackendCodex, true
	case string(BackendCodex):
		return BackendCodex, true
	case string(BackendClaude):
		return BackendClaude, true
	case string(BackendOpenCode):
		return BackendOpenCode, true
	default:
		return "", false
	}
}

func BackendDisplayName(backend Backend) string {
	switch NormalizeBackend(backend) {
	case BackendClaude:
		return "Claude"
	case BackendOpenCode:
		return "OpenCode"
	default:
		return "Codex"
	}
}

func DefaultCapabilitiesForBackend(backend Backend) Capabilities {
	switch NormalizeBackend(backend) {
	case BackendClaude:
		return Capabilities{
			ThreadsRefresh:       true,
			TurnSteer:            true,
			RequestRespond:       true,
			SessionCatalog:       true,
			ResumeByThreadID:     true,
			RequiresCWDForResume: true,
		}
	case BackendOpenCode:
		return Capabilities{
			ThreadsRefresh:       true,
			RequestRespond:       true,
			SessionCatalog:       true,
			ResumeByThreadID:     true,
			RequiresCWDForResume: true,
		}
	default:
		return Capabilities{
			ThreadsRefresh:     true,
			TurnSteer:          true,
			ThreadShellCommand: true,
			RequestRespond:     true,
			ResumeByThreadID:   true,
			VSCodeMode:         true,
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
	if caps.ThreadShellCommand {
		base.ThreadShellCommand = true
	}
	if caps.RequestRespond {
		base.RequestRespond = true
	}
	if caps.SessionCatalog {
		base.SessionCatalog = true
	}
	if caps.ModelCatalog {
		base.ModelCatalog = true
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
	if backend, ok := ParseBackend(hello.Instance.Backend); ok {
		return backend
	}
	return Backend(strings.TrimSpace(string(hello.Instance.Backend)))
}

func EffectiveHelloCapabilities(hello Hello) Capabilities {
	if hello.CapabilitiesDeclared {
		return hello.Capabilities
	}
	if backend, ok := ParseBackend(hello.Instance.Backend); ok {
		return EffectiveCapabilitiesForBackend(backend, hello.Capabilities)
	}
	return hello.Capabilities
}
