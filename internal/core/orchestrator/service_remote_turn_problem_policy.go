package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	codexProblemOther                         = "other"
	codexProblemResponseStreamDisconnected    = "responseStreamDisconnected"
	codexProblemResponseTooManyFailedAttempts = "responseTooManyFailedAttempts"
	codexProblemServerOverloaded              = "serverOverloaded"
)

// Auto-continue eligibility is owned by our local codex/gateway error taxonomy.
// Terminal turn handling must not depend on upstream willRetry.
func isAutoContinueEligibleProblem(problem *agentproto.ErrorInfo) bool {
	if problem == nil || !isAutoContinueEligibleProblemLayer(problem.Layer) {
		return false
	}
	switch strings.TrimSpace(problem.Code) {
	case codexProblemOther,
		codexProblemResponseStreamDisconnected,
		codexProblemResponseTooManyFailedAttempts,
		codexProblemServerOverloaded:
		return true
	default:
		return false
	}
}

func isAutoContinueEligibleProblemLayer(layer string) bool {
	switch strings.TrimSpace(layer) {
	case "", "codex", "gateway":
		return true
	default:
		return false
	}
}
