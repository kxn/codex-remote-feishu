package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func cloneThreadReplayRecord(replay *state.ThreadReplayRecord) *state.ThreadReplayRecord {
	if replay == nil {
		return nil
	}
	cloned := *replay
	return &cloned
}

func (s *Service) clearThreadReplay(inst *state.InstanceRecord, threadID string) {
	if strings.TrimSpace(threadID) == "" {
		return
	}
	for _, current := range s.root.Instances {
		if current == nil || current.Threads == nil {
			continue
		}
		if thread := current.Threads[threadID]; thread != nil {
			thread.UndeliveredReplay = nil
		}
	}
	if inst == nil || inst.Threads == nil {
		return
	}
	if thread := inst.Threads[threadID]; thread != nil {
		thread.UndeliveredReplay = nil
	}
}

func (s *Service) storeThreadReplayText(inst *state.InstanceRecord, threadID, turnID, itemID, text string) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	sourceMessageID, sourceMessagePreview := s.replyAnchorForTurn(inst.InstanceID, threadID, turnID)
	thread := s.ensureThread(inst, threadID)
	s.clearThreadReplay(inst, threadID)
	snippet := previewOfText(text)
	thread.LastAssistantMessage = snippet
	thread.Preview = snippet
	s.touchThread(thread)
	thread.UndeliveredReplay = &state.ThreadReplayRecord{
		Kind:                 state.ThreadReplayAssistantFinal,
		TurnID:               turnID,
		ItemID:               itemID,
		Text:                 text,
		SourceMessageID:      sourceMessageID,
		SourceMessagePreview: sourceMessagePreview,
	}
}

func (s *Service) storeThreadReplayNotice(inst *state.InstanceRecord, threadID string, notice control.Notice) {
	s.storeThreadReplayNoticeWithSource(inst, threadID, notice, "", "")
}

func (s *Service) storeThreadReplayTurnNotice(inst *state.InstanceRecord, threadID, turnID string, notice control.Notice) {
	sourceMessageID := ""
	sourceMessagePreview := ""
	if inst != nil {
		sourceMessageID, sourceMessagePreview = s.replyAnchorForTurn(inst.InstanceID, threadID, turnID)
	}
	s.storeThreadReplayNoticeWithSource(inst, threadID, notice, sourceMessageID, sourceMessagePreview)
}

func (s *Service) storeThreadReplayNoticeWithSource(inst *state.InstanceRecord, threadID string, notice control.Notice, sourceMessageID, sourceMessagePreview string) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	if strings.TrimSpace(notice.Text) == "" && strings.TrimSpace(notice.Title) == "" {
		return
	}
	thread := s.ensureThread(inst, threadID)
	s.clearThreadReplay(inst, threadID)
	thread.UndeliveredReplay = &state.ThreadReplayRecord{
		Kind:                 state.ThreadReplayNotice,
		SourceMessageID:      strings.TrimSpace(sourceMessageID),
		SourceMessagePreview: strings.TrimSpace(sourceMessagePreview),
		NoticeCode:           notice.Code,
		NoticeTitle:          notice.Title,
		NoticeText:           notice.Text,
		NoticeThemeKey:       notice.ThemeKey,
	}
}

func (s *Service) threadReplayRecord(inst *state.InstanceRecord, threadID string) *state.ThreadReplayRecord {
	if inst == nil || inst.Threads == nil || strings.TrimSpace(threadID) == "" {
		return nil
	}
	thread := inst.Threads[threadID]
	if thread == nil || thread.UndeliveredReplay == nil {
		return nil
	}
	return cloneThreadReplayRecord(thread.UndeliveredReplay)
}

func (s *Service) takeThreadReplay(inst *state.InstanceRecord, threadID string) *state.ThreadReplayRecord {
	if strings.TrimSpace(threadID) == "" {
		return nil
	}
	replay := s.threadReplayRecord(inst, threadID)
	if replay == nil {
		for _, other := range s.root.Instances {
			if other == nil || other == inst {
				continue
			}
			replay = s.threadReplayRecord(other, threadID)
			if replay != nil {
				break
			}
		}
	}
	if replay == nil {
		return nil
	}
	s.clearThreadReplay(inst, threadID)
	return replay
}

func (s *Service) adoptThreadReplay(inst *state.InstanceRecord, threadID string) {
	if inst == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	thread := s.ensureThread(inst, threadID)
	if thread.UndeliveredReplay != nil {
		replay := cloneThreadReplayRecord(thread.UndeliveredReplay)
		s.clearThreadReplay(inst, threadID)
		thread.UndeliveredReplay = replay
		return
	}
	replay := s.takeThreadReplay(inst, threadID)
	if replay != nil {
		thread.UndeliveredReplay = replay
	}
}

func (s *Service) replayThreadUpdate(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []eventcontract.Event {
	if surface == nil || inst == nil || strings.TrimSpace(threadID) == "" || !s.surfaceOwnsThread(surface, threadID) {
		return nil
	}
	if inst.ActiveTurnID != "" && inst.ActiveThreadID == threadID {
		return nil
	}
	replay := s.takeThreadReplay(inst, threadID)
	if replay == nil {
		return nil
	}

	switch replay.Kind {
	case state.ThreadReplayAssistantFinal:
		return s.renderTextToSurfaceWithSource(surface, inst, threadID, replay.TurnID, replay.ItemID, replay.Text, true, nil, nil, nil, replay.SourceMessageID, replay.SourceMessagePreview)
	case state.ThreadReplayNotice:
		if replay.NoticeCode == "context_compacted" {
			if !s.surfaceAllowsProcessProgress(surface, "context_compaction") {
				return nil
			}
			progress := &control.ExecCommandProgress{
				ThreadID: threadID,
				Timeline: []control.ExecCommandProgressTimelineItem{
					compactCompletionProgressTimelineItem("context_compaction"),
				},
			}
			return []eventcontract.Event{{
				Kind:                eventcontract.KindExecCommandProgress,
				GatewayID:           surface.GatewayID,
				SurfaceSessionID:    surface.SurfaceSessionID,
				SourceMessageID:     replay.SourceMessageID,
				ExecCommandProgress: progress,
			}}
		}
		notice := control.Notice{
			Code:     replay.NoticeCode,
			Title:    replay.NoticeTitle,
			Text:     replay.NoticeText,
			ThemeKey: replay.NoticeThemeKey,
		}
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  replay.SourceMessageID,
			Notice:           &notice,
		}}
	default:
		return nil
	}
}
