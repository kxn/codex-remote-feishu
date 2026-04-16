package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const maxRetainedFinalCardsPerSurface = 8

func (s *Service) RecordFinalCardMessage(surfaceID string, block render.Block, sourceMessageID, messageID, daemonLifecycleID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	if !block.Final {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		instanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	threadID := strings.TrimSpace(block.ThreadID)
	turnID := strings.TrimSpace(block.TurnID)
	if instanceID == "" || threadID == "" || turnID == "" {
		return
	}
	record := &state.FinalCardRecord{
		InstanceID:        instanceID,
		ThreadID:          threadID,
		TurnID:            turnID,
		ItemID:            strings.TrimSpace(block.ItemID),
		SourceMessageID:   strings.TrimSpace(sourceMessageID),
		MessageID:         messageID,
		DaemonLifecycleID: strings.TrimSpace(daemonLifecycleID),
		RecordedAt:        s.now().UTC(),
	}
	retained := make([]*state.FinalCardRecord, 0, len(surface.RecentFinalCards)+1)
	for _, existing := range surface.RecentFinalCards {
		if existing == nil {
			continue
		}
		if strings.TrimSpace(existing.MessageID) == record.MessageID {
			continue
		}
		if sameFinalCardIdentity(existing, record.InstanceID, record.ThreadID, record.TurnID, record.ItemID) {
			continue
		}
		retained = append(retained, existing)
	}
	retained = append(retained, record)
	if len(retained) > maxRetainedFinalCardsPerSurface {
		retained = append([]*state.FinalCardRecord(nil), retained[len(retained)-maxRetainedFinalCardsPerSurface:]...)
	}
	surface.RecentFinalCards = retained
}

func (s *Service) LookupFinalCard(surfaceID, instanceID, threadID, turnID, itemID, daemonLifecycleID string) *state.FinalCardRecord {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	itemID = strings.TrimSpace(itemID)
	daemonLifecycleID = strings.TrimSpace(daemonLifecycleID)
	for i := len(surface.RecentFinalCards) - 1; i >= 0; i-- {
		record := surface.RecentFinalCards[i]
		if record == nil {
			continue
		}
		if !sameFinalCardIdentity(record, instanceID, threadID, turnID, itemID) {
			continue
		}
		if daemonLifecycleID != "" && strings.TrimSpace(record.DaemonLifecycleID) != daemonLifecycleID {
			continue
		}
		copy := *record
		return &copy
	}
	return nil
}

func (s *Service) LookupFinalCardForBlock(surfaceID string, block render.Block, daemonLifecycleID string) *state.FinalCardRecord {
	if !block.Final {
		return nil
	}
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		if surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]; surface != nil {
			instanceID = strings.TrimSpace(surface.AttachedInstanceID)
		}
	}
	return s.LookupFinalCard(surfaceID, instanceID, block.ThreadID, block.TurnID, block.ItemID, daemonLifecycleID)
}

func clearSurfaceFinalCards(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.RecentFinalCards = nil
}

func sameFinalCardIdentity(record *state.FinalCardRecord, instanceID, threadID, turnID, itemID string) bool {
	if record == nil {
		return false
	}
	if strings.TrimSpace(record.InstanceID) != strings.TrimSpace(instanceID) {
		return false
	}
	if strings.TrimSpace(record.ThreadID) != strings.TrimSpace(threadID) {
		return false
	}
	if strings.TrimSpace(record.TurnID) != strings.TrimSpace(turnID) {
		return false
	}
	if strings.TrimSpace(itemID) == "" {
		return true
	}
	return strings.TrimSpace(record.ItemID) == strings.TrimSpace(itemID)
}
