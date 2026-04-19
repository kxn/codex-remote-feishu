package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const defaultCompactOwnerTTL = 10 * time.Minute

func compactOwnerFlowTrackingKey(flow *activeOwnerCardFlowRecord) string {
	if flow == nil || strings.TrimSpace(flow.MessageID) != "" {
		return ""
	}
	return strings.TrimSpace(flow.FlowID)
}

func compactOwnerCardEvent(surfaceID string, flow *activeOwnerCardFlowRecord, title, theme string, sections []control.FeishuCardTextSection) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		FeishuDirectCommandCatalog: &control.FeishuDirectCommandCatalog{
			Title:           strings.TrimSpace(title),
			MessageID:       strings.TrimSpace(flow.MessageID),
			TrackingKey:     compactOwnerFlowTrackingKey(flow),
			ThemeKey:        strings.TrimSpace(theme),
			Patchable:       true,
			SummarySections: cloneFeishuCardSections(sections),
		},
	}
}

func compactThreadLabel(threadID string, thread *state.ThreadRecord) string {
	name := ""
	if thread != nil {
		name = strings.TrimSpace(thread.Name)
		threadID = firstNonEmpty(strings.TrimSpace(thread.ThreadID), strings.TrimSpace(threadID))
	}
	threadID = strings.TrimSpace(threadID)
	switch {
	case name != "" && threadID != "":
		return name + " (" + threadID + ")"
	case name != "":
		return name
	default:
		return threadID
	}
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

func (s *Service) emitCompactOwnerDispatching(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) []control.UIEvent {
	flow := s.compactFlowForBinding(surface, binding)
	if flow == nil {
		return nil
	}
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseLoading, s.now(), defaultCompactOwnerTTL)
	sections := compactOwnerCardSections(
		binding.ThreadID,
		s.compactThreadForBinding(binding),
		"正在向本地 Codex 发起上下文压缩请求。",
		"压缩期间普通输入会排队，完成后会自动恢复。",
	)
	return []control.UIEvent{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "正在压缩上下文", "progress", sections)}
}

func (s *Service) emitCompactOwnerRunning(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) []control.UIEvent {
	flow := s.compactFlowForBinding(surface, binding)
	if flow == nil {
		return nil
	}
	refreshOwnerCardFlow(flow, ownerCardFlowPhaseRunning, s.now(), defaultCompactOwnerTTL)
	sections := compactOwnerCardSections(
		binding.ThreadID,
		s.compactThreadForBinding(binding),
		"正在压缩当前会话的上下文。",
		"压缩期间普通输入会排队，完成后会自动恢复。",
	)
	return []control.UIEvent{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "正在压缩上下文", "progress", sections)}
}

func (s *Service) emitCompactOwnerCompleted(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding) []control.UIEvent {
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
	return []control.UIEvent{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "上下文已压缩", "success", sections)}
}

func compactFailureText(problem agentproto.ErrorInfo, fallback string) string {
	if text := strings.TrimSpace(problem.Message); text != "" {
		return text
	}
	return strings.TrimSpace(fallback)
}

func (s *Service) emitCompactOwnerFailed(surface *state.SurfaceConsoleRecord, binding *compactTurnBinding, detail string) []control.UIEvent {
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
	return []control.UIEvent{compactOwnerCardEvent(surface.SurfaceSessionID, flow, "上下文压缩失败", "error", sections)}
}
