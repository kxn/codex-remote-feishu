package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func compactCompletionNotice() control.Notice {
	return control.Notice{
		Code: "context_compacted",
		Text: "上下文已压缩。",
	}
}

func compactCompletionProgressEntryRecord(itemID string) state.ExecCommandProgressEntryRecord {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		itemID = "context_compaction"
	}
	return state.ExecCommandProgressEntryRecord{
		ItemID:  itemID,
		Kind:    "context_compaction",
		Label:   "压缩",
		Summary: "上下文已压缩。",
		Status:  "completed",
	}
}

func compactCompletionProgressTimelineItem(itemID string) control.ExecCommandProgressTimelineItem {
	entry := compactCompletionProgressEntryRecord(itemID)
	return control.ExecCommandProgressTimelineItem{
		ID:      entry.ItemID,
		Kind:    entry.Kind,
		Label:   entry.Label,
		Summary: entry.Summary,
		Status:  entry.Status,
		LastSeq: 1,
	}
}

func (r *serviceProgressRuntime) renderCompactNotice(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := r.service.root.Instances[instanceID]
	if binding := r.service.turns.compactTurns[instanceID]; binding != nil && strings.TrimSpace(binding.TurnID) != "" && binding.TurnID == strings.TrimSpace(event.TurnID) {
		if binding.ThreadID == "" || strings.TrimSpace(event.ThreadID) == "" || binding.ThreadID == event.ThreadID {
			binding.CompletionSeen = true
			if inst != nil && strings.TrimSpace(event.ThreadID) != "" {
				r.service.clearThreadReplay(inst, event.ThreadID)
			}
			surface := r.service.root.Surfaces[binding.SurfaceSessionID]
			if surface == nil {
				return nil
			}
			return r.service.emitCompactOwnerCompleted(surface, binding)
		}
	}
	notice := compactCompletionNotice()
	surface := r.service.surfaceForInitiator(instanceID, event)
	if surface == nil {
		if inst != nil && strings.TrimSpace(event.ThreadID) != "" {
			r.service.storeThreadReplayTurnNotice(inst, event.ThreadID, event.TurnID, notice)
		}
		return nil
	}
	if inst != nil && strings.TrimSpace(event.ThreadID) != "" {
		r.service.clearThreadReplay(inst, event.ThreadID)
	}
	if !r.service.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := r.service.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = firstNonEmpty(strings.TrimSpace(event.ItemID), progress.ItemID)
	execprogress.UpsertEntry(progress, compactCompletionProgressEntryRecord(event.ItemID))
	return r.service.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}
