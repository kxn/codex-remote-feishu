package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) applyThreadTokenUsageUpdate(instanceID string, event agentproto.Event) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil || strings.TrimSpace(event.ThreadID) == "" || event.TokenUsage == nil {
		return nil
	}
	thread := s.ensureThread(inst, event.ThreadID)
	thread.TokenUsage = agentproto.CloneThreadTokenUsage(event.TokenUsage)
	s.recordRemoteTurnTokenUsage(instanceID, event.ThreadID, event.TurnID, event.TokenUsage)
	return nil
}

func (s *Service) recordRemoteTurnTokenUsage(instanceID, threadID, turnID string, usage *agentproto.ThreadTokenUsage) {
	if usage == nil {
		return
	}
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		return
	}
	binding.LastUsage = usage.Last
	binding.HasLastUsage = true
}

func (s *Service) captureRemoteTurnStartTotalUsage(instanceID string, binding *remoteTurnBinding, threadID string) {
	if binding == nil || binding.HasStartTotalUsage {
		return
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return
	}
	threadID = strings.TrimSpace(firstNonEmpty(threadID, binding.ThreadID))
	if threadID == "" {
		return
	}
	thread := inst.Threads[threadID]
	if thread == nil || thread.TokenUsage == nil {
		return
	}
	binding.StartTotalUsage = thread.TokenUsage.Total
	binding.HasStartTotalUsage = true
}

func finalTurnSummaryForBinding(now time.Time, binding *remoteTurnBinding, thread *state.ThreadRecord) *control.FinalTurnSummary {
	if binding == nil || binding.StartedAt.IsZero() {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	elapsed := now.Sub(binding.StartedAt)
	if elapsed <= 0 {
		return nil
	}
	summary := &control.FinalTurnSummary{
		Elapsed:   elapsed,
		ThreadCWD: strings.TrimSpace(binding.ThreadCWD),
	}
	if thread != nil && thread.TokenUsage != nil {
		summary.ThreadUsage = finalTurnUsageFromBreakdown(thread.TokenUsage.Total)
		summary.TotalTokensInContext = thread.TokenUsage.Last.TotalTokens
		if thread.TokenUsage.Last.InputTokens > 0 {
			value := thread.TokenUsage.Last.InputTokens
			summary.ContextInputTokens = &value
		}
		if thread.TokenUsage.ModelContextWindow != nil {
			value := *thread.TokenUsage.ModelContextWindow
			summary.ModelContextWindow = &value
		}
	}
	if thread != nil && thread.TokenUsage != nil && binding.HasStartTotalUsage {
		if delta, ok := finalTurnUsageDelta(thread.TokenUsage.Total, binding.StartTotalUsage); ok {
			summary.Usage = delta
		}
	}
	if summary.Usage == nil && binding.HasLastUsage {
		summary.Usage = finalTurnUsageFromBreakdown(binding.LastUsage)
	}
	return summary
}

func finalTurnUsageFromBreakdown(usage agentproto.TokenUsageBreakdown) *control.FinalTurnUsage {
	return &control.FinalTurnUsage{
		InputTokens:           usage.InputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		OutputTokens:          usage.OutputTokens,
		ReasoningOutputTokens: usage.ReasoningOutputTokens,
		TotalTokens:           usage.TotalTokens,
	}
}

func finalTurnUsageDelta(end, start agentproto.TokenUsageBreakdown) (*control.FinalTurnUsage, bool) {
	if end.InputTokens < start.InputTokens ||
		end.CachedInputTokens < start.CachedInputTokens ||
		end.OutputTokens < start.OutputTokens ||
		end.ReasoningOutputTokens < start.ReasoningOutputTokens ||
		end.TotalTokens < start.TotalTokens {
		return nil, false
	}
	return &control.FinalTurnUsage{
		InputTokens:           end.InputTokens - start.InputTokens,
		CachedInputTokens:     end.CachedInputTokens - start.CachedInputTokens,
		OutputTokens:          end.OutputTokens - start.OutputTokens,
		ReasoningOutputTokens: end.ReasoningOutputTokens - start.ReasoningOutputTokens,
		TotalTokens:           end.TotalTokens - start.TotalTokens,
	}, true
}
