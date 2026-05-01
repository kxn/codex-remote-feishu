package control

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func RequestBackendDisplayName(backend agentproto.Backend) string {
	return agentproto.BackendDisplayName(backend)
}

func RequestLocalBackendDisplayName(backend agentproto.Backend) string {
	return "本地 " + RequestBackendDisplayName(backend)
}

func RequestFeedbackActionLabel(backend agentproto.Backend) string {
	return "告诉 " + RequestBackendDisplayName(backend) + " 怎么改"
}

func RequestWaitingContinueText(backend agentproto.Backend) string {
	return "等待 " + RequestBackendDisplayName(backend) + " 继续"
}
