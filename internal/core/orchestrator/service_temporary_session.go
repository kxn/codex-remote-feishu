package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const reviewTemporarySessionLabel = "临时会话 · 审阅"

type temporarySessionKind string

const (
	temporarySessionKindNone        temporarySessionKind = ""
	temporarySessionKindDetourBlank temporarySessionKind = "detour_blank"
	temporarySessionKindDetourFork  temporarySessionKind = "detour_fork"
	temporarySessionKindReview      temporarySessionKind = "review"
)

type temporarySessionContext struct {
	Kind  temporarySessionKind
	Label string
}

func temporarySessionContextForExecutionMode(mode agentproto.PromptExecutionMode) temporarySessionContext {
	switch agentproto.NormalizePromptExecutionMode(mode) {
	case agentproto.PromptExecutionModeForkEphemeral:
		return temporarySessionContext{Kind: temporarySessionKindDetourFork, Label: detourForkLabel}
	case agentproto.PromptExecutionModeStartEphemeral:
		return temporarySessionContext{Kind: temporarySessionKindDetourBlank, Label: detourBlankLabel}
	default:
		return temporarySessionContext{}
	}
}

func (ctx temporarySessionContext) isReview() bool {
	return ctx.Kind == temporarySessionKindReview
}

func (s *Service) temporarySessionContext(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) temporarySessionContext {
	if binding := s.lookupRemoteTurn(instanceID, threadID, turnID); binding != nil {
		if ctx := temporarySessionContextForExecutionMode(remoteBindingPromptDispatchPlan(binding).ExecutionMode); ctx.Kind != temporarySessionKindNone {
			return ctx
		}
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		if surface != nil {
			if session := s.activeReviewSession(surface); session != nil {
				return temporarySessionContext{Kind: temporarySessionKindReview, Label: reviewTemporarySessionLabel}
			}
		}
		return temporarySessionContext{}
	}
	if surface == nil {
		surface, _ = s.reviewSessionSurface(instanceID, threadID)
	}
	if surface != nil {
		if session := s.validReviewSession(surface); session != nil && strings.TrimSpace(session.ReviewThreadID) == threadID {
			return temporarySessionContext{Kind: temporarySessionKindReview, Label: reviewTemporarySessionLabel}
		}
	}
	if inst := s.root.Instances[instanceID]; inst != nil && threadIsReview(inst.Threads[threadID]) {
		return temporarySessionContext{Kind: temporarySessionKindReview, Label: reviewTemporarySessionLabel}
	}
	return temporarySessionContext{}
}

func (s *Service) temporarySessionLabel(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) string {
	return strings.TrimSpace(s.temporarySessionContext(surface, instanceID, threadID, turnID).Label)
}

func (s *Service) requestTemporarySessionLabel(record *state.RequestPromptRecord) string {
	if record == nil {
		return ""
	}
	return s.temporarySessionLabel(nil, record.InstanceID, record.ThreadID, record.TurnID)
}

func (s *Service) ResolveTemporarySessionLabel(surfaceID, instanceID, threadID, turnID string) string {
	if s == nil {
		return ""
	}
	var surface *state.SurfaceConsoleRecord
	surface = s.root.Surfaces[strings.TrimSpace(surfaceID)]
	return s.temporarySessionLabel(surface, instanceID, threadID, turnID)
}

func (s *Service) ShouldKeepDefaultFinalTitle(surfaceID, instanceID, threadID, turnID string) bool {
	if s == nil {
		return false
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	return s.temporarySessionContext(surface, instanceID, threadID, turnID).isReview()
}
