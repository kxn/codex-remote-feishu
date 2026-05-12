package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const defaultCompactOwnerTTL = 10 * time.Minute

func compactOwnerFlowTrackingKey(flow *activeOwnerCardFlowRecord) string {
	if flow == nil || strings.TrimSpace(flow.MessageID) != "" {
		return ""
	}
	return strings.TrimSpace(flow.FlowID)
}

func compactOwnerCardEvent(surfaceID string, flow *activeOwnerCardFlowRecord, title, theme string, sections []control.FeishuCardTextSection) eventcontract.Event {
	bodySections, noticeSections := compactOwnerCardSplitSections(sections)
	sealed := flow != nil && (flow.Phase == ownerCardFlowPhaseCompleted || flow.Phase == ownerCardFlowPhaseCancelled || flow.Phase == ownerCardFlowPhaseError)
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		Title:          strings.TrimSpace(title),
		MessageID:      strings.TrimSpace(flow.MessageID),
		TrackingKey:    compactOwnerFlowTrackingKey(flow),
		ThemeKey:       strings.TrimSpace(theme),
		Patchable:      true,
		BodySections:   bodySections,
		NoticeSections: noticeSections,
		Sealed:         sealed,
	})
	return eventcontract.NewEventFromPayload(
		eventcontract.PagePayload{View: view},
		eventcontract.EventMeta{
			Target: eventcontract.TargetRef{
				SurfaceSessionID: strings.TrimSpace(surfaceID),
			},
		},
	)
}

func compactOwnerCardSplitSections(sections []control.FeishuCardTextSection) ([]control.FeishuCardTextSection, []control.FeishuCardTextSection) {
	if len(sections) == 0 {
		return nil, nil
	}
	body := make([]control.FeishuCardTextSection, 0, 1)
	notice := make([]control.FeishuCardTextSection, 0, len(sections))
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		if normalized.Label == "当前会话" && len(body) == 0 {
			body = append(body, normalized)
			continue
		}
		notice = append(notice, normalized)
	}
	return body, notice
}

func compactThreadLabel(_ string, thread *state.ThreadRecord) string {
	return displayThreadTitle(nil, thread)
}

func compactOwnerCardSections(threadID string, thread *state.ThreadRecord, lines ...string) []control.FeishuCardTextSection {
	sections := make([]control.FeishuCardTextSection, 0, 2)
	if label := compactThreadLabel(threadID, thread); label != "" {
		sections = append(sections, control.FeishuCardTextSection{
			Label: "当前会话",
			Lines: []string{label},
		})
	}
	bodyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			bodyLines = append(bodyLines, trimmed)
		}
	}
	if len(bodyLines) != 0 {
		sections = append(sections, control.FeishuCardTextSection{Lines: bodyLines})
	}
	return sections
}

func (s *Service) compactFlowForBinding(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) *activeOwnerCardFlowRecord {
	if surface == nil || binding == nil {
		return nil
	}
	flow := s.activeOwnerCardFlow(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindCompact {
		return nil
	}
	if strings.TrimSpace(binding.FlowID) == "" || strings.TrimSpace(flow.FlowID) != strings.TrimSpace(binding.FlowID) {
		return nil
	}
	return flow
}

func (s *Service) compactThreadForBinding(binding *compactTurnBinding) *state.ThreadRecord {
	if binding == nil {
		return nil
	}
	inst := s.root.Instances[strings.TrimSpace(binding.InstanceID)]
	if inst == nil {
		return nil
	}
	return inst.Threads[strings.TrimSpace(binding.ThreadID)]
}

func (s *Service) emitCompactOwnerDispatching(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) []eventcontract.Event {
	flow := s.compactFlowForBinding(surface, binding)
	if flow == nil {
		return nil
	}
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseLoading, s.now(), defaultCompactOwnerTTL)
	sections := compactOwnerCardSections(
		binding.ThreadID,
		s.compactThreadForBinding(binding),
		"正在向本地 Codex 发起上下文压缩请求。",
	)
	return []eventcontract.Event{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "正在压缩上下文", "progress", sections)}
}

func (s *Service) emitCompactOwnerRunning(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) []eventcontract.Event {
	flow := s.compactFlowForBinding(surface, binding)
	if flow == nil {
		return nil
	}
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseRunning, s.now(), defaultCompactOwnerTTL)
	sections := compactOwnerCardSections(
		binding.ThreadID,
		s.compactThreadForBinding(binding),
		"正在压缩当前会话的上下文。",
	)
	return []eventcontract.Event{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "正在压缩上下文", "progress", sections)}
}

func (s *Service) emitCompactOwnerCompleted(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) []eventcontract.Event {
	flow := s.compactFlowForBinding(surface, binding)
	if flow == nil {
		return nil
	}
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseCompleted, s.now(), defaultCompactOwnerTTL)
	sections := compactOwnerCardSections(
		binding.ThreadID,
		s.compactThreadForBinding(binding),
		"当前会话的上下文已压缩完成。",
	)
	return []eventcontract.Event{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "上下文已压缩", "success", sections)}
}

func compactFailureText(problem agentproto.ErrorInfo, fallback string) string {
	if text := strings.TrimSpace(problem.Message); text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}

func (s *Service) emitCompactOwnerFailed(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding, detail string) []eventcontract.Event {
	flow := s.compactFlowForBinding(surface, binding)
	if flow == nil {
		return nil
	}
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseError, s.now(), defaultCompactOwnerTTL)
	sections := compactOwnerCardSections(
		binding.ThreadID,
		s.compactThreadForBinding(binding),
		detail,
		"现在可以重新发送 /compact，或继续普通输入。",
	)
	return []eventcontract.Event{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "上下文压缩失败", "error", sections)}
}
