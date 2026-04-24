package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) RecordPageTrackingMessage(surfaceID, trackingKey, messageID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	trackingKey = strings.TrimSpace(trackingKey)
	if messageID == "" || trackingKey == "" {
		return
	}
	if flow := s.activeOwnerCardFlow(surface); flow != nil && strings.TrimSpace(flow.FlowID) == trackingKey {
		s.RecordOwnerCardFlowMessage(surfaceID, trackingKey, messageID)
		return
	}
	if episode := activeAutoContinueEpisode(surface); episode != nil && strings.TrimSpace(episode.EpisodeID) == trackingKey {
		episode.NoticeMessageID = messageID
		if record := s.lookupSurfaceMessageRecord(surface, messageID); record != nil {
			episode.NoticeAppendSeq = record.AppendSeq
		}
	}
}

func (s *Service) RecordSurfaceOutboundMessage(surfaceID, messageID string, kind state.SurfaceMessageKind, replyToMessageID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	if surface.SurfaceMessages == nil {
		surface.SurfaceMessages = map[string]*state.SurfaceMessageRecord{}
	}
	surface.SurfaceMessageSeq++
	surface.SurfaceMessages[messageID] = &state.SurfaceMessageRecord{
		MessageID:        messageID,
		Kind:             kind,
		ReplyToMessageID: strings.TrimSpace(replyToMessageID),
		AppendSeq:        surface.SurfaceMessageSeq,
		RecordedAt:       s.now().UTC(),
	}
	if episode := activeAutoContinueEpisode(surface); episode != nil && episode.NoticeMessageID == messageID {
		episode.NoticeAppendSeq = surface.SurfaceMessageSeq
	}
}

func (s *Service) recordInboundSurfaceMessage(surface *state.SurfaceConsoleRecord, messageID string, kind state.SurfaceMessageKind) {
	if surface == nil {
		return
	}
	s.RecordSurfaceOutboundMessage(surface.SurfaceSessionID, messageID, kind, "")
}

func (s *Service) lookupSurfaceMessageRecord(surface *state.SurfaceConsoleRecord, messageID string) *state.SurfaceMessageRecord {
	if surface == nil || surface.SurfaceMessages == nil {
		return nil
	}
	record := surface.SurfaceMessages[strings.TrimSpace(messageID)]
	if record == nil {
		return nil
	}
	copy := *record
	return &copy
}

func autoContinueEpisodeCanPatchTail(surface *state.SurfaceConsoleRecord, episode *state.PendingAutoContinueEpisodeRecord) bool {
	if surface == nil || episode == nil {
		return false
	}
	messageID := strings.TrimSpace(episode.NoticeMessageID)
	if messageID == "" || surface.SurfaceMessages == nil {
		return false
	}
	record := surface.SurfaceMessages[messageID]
	if record == nil {
		return false
	}
	return record.AppendSeq > 0 && record.AppendSeq == surface.SurfaceMessageSeq
}
