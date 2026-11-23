package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// FrozenCommandInputs describes the exact old-core selection after a command ID
// has been bound. It is persisted with the effect, not recomputed when sending.
// SourceKind distinguishes actual SDK inputs from enabled automatic continuation.
type FrozenCommandInputs struct {
	CommandID, SurfaceID, InstanceID, ThreadID, TurnID string
	Items                                              []FrozenCommandInput
}

type FrozenCommandInput struct {
	QueueItemID           string
	SourceKind            state.QueueItemSourceKind
	AutoContinueEpisodeID string
	SourceMessageIDs      []string
	DispatchPlan          agentproto.PromptDispatchPlan
}

// FrozenCommandInputs returns nil for a command with no pending input binding,
// such as a catalog read. It never selects a queue head or guesses from the most
// recent message. The caller must obtain any control-action provenance separately
// from the committed action/effect relationship.
func (s *Service) FrozenCommandInputs(surfaceID, commandID string) (*FrozenCommandInputs, error) {
	if !s.config.PrivateApplication || strings.TrimSpace(commandID) == "" {
		return nil, fmt.Errorf("private application and bound command required")
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surfaceFeishuRoomID(surface) != "" {
		return nil, fmt.Errorf("private surface required")
	}
	var selected *FrozenCommandInputs
	var selectionErr error
	selectItems := func(instance, owner, command, thread, turn string, ids []string) {
		if command != commandID {
			return
		}
		if owner != surfaceID || selected != nil {
			selectionErr = fmt.Errorf("ambiguous or foreign command input binding")
			return
		}
		selected = &FrozenCommandInputs{CommandID: commandID, SurfaceID: surfaceID, InstanceID: instance, ThreadID: thread, TurnID: turn}
		seen := make(map[string]bool)
		for _, id := range ids {
			if id == "" || seen[id] {
				selectionErr = fmt.Errorf("invalid command queue selection")
				return
			}
			seen[id] = true
			item := surface.QueueItems[id]
			if item == nil || item.ID != id || item.SurfaceSessionID != surfaceID {
				selectionErr = fmt.Errorf("command input no longer belongs to private surface")
				return
			}
			kind := item.SourceKind
			if kind == "" { // Historical pre-source-kind queue records were human input.
				kind = state.QueueItemSourceUser
			}
			if kind != state.QueueItemSourceUser && kind != state.QueueItemSourceAutoContinue && kind != state.QueueItemSourceAutoWhip {
				selectionErr = fmt.Errorf("unknown command input source kind")
				return
			}
			frozen := FrozenCommandInput{QueueItemID: id, SourceKind: kind, AutoContinueEpisodeID: item.AutoContinueEpisodeID, DispatchPlan: item.FrozenDispatchPlan}
			// The main source is first, followed by the original media/cluster
			// sources. Neither queue identifiers nor SDK message IDs encode time.
			messages := append([]string{item.SourceMessageID}, item.SourceMessageIDs...)
			messageSet := make(map[string]bool)
			for _, message := range messages {
				if message != "" && !messageSet[message] {
					messageSet[message] = true
					frozen.SourceMessageIDs = append(frozen.SourceMessageIDs, message)
				}
			}
			selected.Items = append(selected.Items, frozen)
		}
	}
	s.turns.forEachPendingRemote(func(b *remoteTurnBinding) {
		if b.QueueItemID != "" {
			selectItems(b.InstanceID, b.SurfaceSessionID, b.CommandID, b.ThreadID, b.TurnID, []string{b.QueueItemID})
		}
	})
	s.turns.forEachPendingSteer(func(_ string, b *pendingSteerBinding) {
		ids := b.QueueItemIDs
		if len(ids) == 0 && b.QueueItemID != "" {
			ids = []string{b.QueueItemID}
		}
		selectItems(b.InstanceID, b.SurfaceSessionID, b.CommandID, b.ThreadID, b.TurnID, ids)
	})
	s.turns.forEachPendingShell(func(_ string, b *pendingShellBinding) {
		if b.QueueItemID != "" {
			selectItems(b.InstanceID, b.SurfaceSessionID, b.CommandID, b.ThreadID, b.TurnID, []string{b.QueueItemID})
		}
	})
	if selectionErr != nil {
		return nil, selectionErr
	}
	return selected, nil
}
