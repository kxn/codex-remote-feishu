package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) clearThreadReplay(inst *state.InstanceRecord, threadID string) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	thread := inst.Threads[threadID]
	if thread == nil {
		return
	}
	thread.UndeliveredReplay = nil
}

func (s *Service) storeThreadReplayText(inst *state.InstanceRecord, threadID, turnID, itemID, text string) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	thread := s.ensureThread(inst, threadID)
	thread.Preview = previewOfText(text)
	s.touchThread(thread)
	thread.UndeliveredReplay = &state.ThreadReplayRecord{
		Kind:   state.ThreadReplayAssistantFinal,
		TurnID: turnID,
		ItemID: itemID,
		Text:   text,
	}
}

func (s *Service) storeThreadReplayNotice(inst *state.InstanceRecord, threadID string, notice control.Notice) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	if strings.TrimSpace(notice.Text) == "" && strings.TrimSpace(notice.Title) == "" {
		return
	}
	thread := s.ensureThread(inst, threadID)
	thread.UndeliveredReplay = &state.ThreadReplayRecord{
		Kind:           state.ThreadReplayNotice,
		NoticeCode:     notice.Code,
		NoticeTitle:    notice.Title,
		NoticeText:     notice.Text,
		NoticeThemeKey: notice.ThemeKey,
	}
}

func (s *Service) replayThreadUpdate(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	if surface == nil || inst == nil || strings.TrimSpace(threadID) == "" || !s.surfaceOwnsThread(surface, threadID) {
		return nil
	}
	if inst.ActiveTurnID != "" && inst.ActiveThreadID == threadID {
		return nil
	}
	thread := inst.Threads[threadID]
	if thread == nil || thread.UndeliveredReplay == nil {
		return nil
	}
	replay := thread.UndeliveredReplay
	thread.UndeliveredReplay = nil

	switch replay.Kind {
	case state.ThreadReplayAssistantFinal:
		return s.renderTextToSurface(surface, inst, threadID, replay.TurnID, replay.ItemID, replay.Text, true)
	case state.ThreadReplayNotice:
		notice := control.Notice{
			Code:     replay.NoticeCode,
			Title:    replay.NoticeTitle,
			Text:     replay.NoticeText,
			ThemeKey: replay.NoticeThemeKey,
		}
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &notice,
		}}
	default:
		return nil
	}
}
