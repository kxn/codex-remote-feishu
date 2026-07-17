package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) projectProtocolNotice(instanceID string, notice agentproto.ProtocolNotice) []eventcontract.Event {
	switch strings.TrimSpace(notice.Method) {
	case "guardianWarning":
		return s.projectGuardianWarning(instanceID, notice)
	case "configWarning":
		return s.projectConfigWarning(instanceID, notice)
	default:
		return nil
	}
}

func (s *Service) projectGuardianWarning(instanceID string, notice agentproto.ProtocolNotice) []eventcontract.Event {
	surface := s.turnSurface(instanceID, notice.ThreadID, notice.TurnID)
	if surface == nil {
		return nil
	}
	summary := strings.TrimSpace(notice.Summary)
	if summary == "" {
		summary = "Codex 返回了 guardian warning，请检查当前任务是否需要调整。"
	}
	if !s.allowActiveNotice("guardian", surface.SurfaceSessionID, instanceID, notice.ThreadID, summary, 10*time.Minute) {
		return nil
	}
	payload := control.Notice{
		Code:     "codex_guardian_warning",
		Title:    "Codex guardian warning",
		Text:     summary,
		ThemeKey: "warning",
	}
	return []eventcontract.Event{surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: payload},
		eventcontract.EventMeta{},
	)}
}

func (s *Service) projectConfigWarning(instanceID string, notice agentproto.ProtocolNotice) []eventcontract.Event {
	if !isSevereConfigWarning(notice) {
		return nil
	}
	surface := s.turnSurface(instanceID, notice.ThreadID, notice.TurnID)
	if surface == nil {
		return nil
	}
	summary := strings.TrimSpace(notice.Summary)
	if summary == "" {
		summary = "Codex 配置存在会影响运行的问题，请检查配置。"
	}
	if !s.allowActiveNotice("config", surface.SurfaceSessionID, instanceID, notice.ThreadID, firstNonEmpty(notice.Path, summary), 30*time.Minute) {
		return nil
	}
	payload := control.Notice{
		Code:     "codex_config_warning",
		Title:    "Codex config warning",
		Text:     summary,
		ThemeKey: "warning",
	}
	return []eventcontract.Event{surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: payload},
		eventcontract.EventMeta{},
	)}
}

func isSevereConfigWarning(notice agentproto.ProtocolNotice) bool {
	text := strings.ToLower(strings.Join([]string{
		notice.Summary,
		notice.Details,
		notice.Path,
		notice.Range,
	}, " "))
	for _, token := range []string{
		"invalid",
		"failed",
		"failure",
		"error",
		"permission",
		"auth",
		"denied",
		"missing",
		"unreadable",
		"parse",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func (s *Service) allowActiveNotice(kind, surfaceID, instanceID, threadID, dedupeKey string, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return true
	}
	key := activeNoticeCooldownKey(kind, surfaceID, instanceID, threadID, dedupeKey)
	if key == "" {
		return false
	}
	now := s.now().UTC()
	if next := s.activeNoticeCooldowns[key]; !next.IsZero() && now.Before(next) {
		return false
	}
	s.activeNoticeCooldowns[key] = now.Add(cooldown)
	return true
}

func (s *Service) clearActiveNotice(kind, surfaceID, instanceID, threadID, dedupeKey string) {
	key := activeNoticeCooldownKey(kind, surfaceID, instanceID, threadID, dedupeKey)
	if key == "" {
		return
	}
	delete(s.activeNoticeCooldowns, key)
}

func (s *Service) clearActiveNoticePrefix(kind, surfaceID, instanceID, threadID, dedupePrefix string) {
	prefix := activeNoticeCooldownKey(kind, surfaceID, instanceID, threadID, dedupePrefix)
	if prefix == "" {
		return
	}
	for key := range s.activeNoticeCooldowns {
		if key == prefix || strings.HasPrefix(key, prefix+" ") {
			delete(s.activeNoticeCooldowns, key)
		}
	}
}

func activeNoticeCooldownKey(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(part)), " "))
		if part != "" {
			normalized = append(normalized, part)
		}
	}
	return strings.Join(normalized, "|")
}
