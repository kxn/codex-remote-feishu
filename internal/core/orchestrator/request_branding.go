package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func requestPromptBackend(record *state.RequestPromptRecord) agentproto.Backend {
	if record == nil {
		return agentproto.BackendCodex
	}
	return agentproto.NormalizeBackend(record.Backend)
}

func requestBackendDisplayName(backend agentproto.Backend) string {
	return control.RequestBackendDisplayName(backend)
}

func requestLocalBackendDisplayName(backend agentproto.Backend) string {
	return control.RequestLocalBackendDisplayName(backend)
}

func requestFeedbackActionLabel(backend agentproto.Backend) string {
	return control.RequestFeedbackActionLabel(backend)
}

func requestWaitingContinueText(backend agentproto.Backend) string {
	return control.RequestWaitingContinueText(backend)
}
