package acp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (t *Translator) observePromptResponse(pending pendingRPC, payload map[string]any, result Result) Result {
	turn := pending.Turn
	if turn == nil {
		turn = t.activeTurns[xutil.FirstNonEmpty(pending.Command.Target.ThreadID, t.currentSessionID)]
	}
	if turn == nil {
		return result
	}
	if usage, ok := promptUsage(payload["usage"]); ok {
		result.Events = append(result.Events, t.updateUsage(turn.ThreadID, usage))
	}
	result.Events = append(result.Events, t.completeOpenTextItems(turn)...)
	stopReason := xutil.LookupStringFromAny(payload["stopReason"])
	status := statusFromStopReason(stopReason)
	completed := agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		CommandID:            turn.CommandID,
		ThreadID:             turn.ThreadID,
		TurnID:               turn.TurnID,
		Status:               status,
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
		Initiator:            turn.Initiator,
	}
	if unknownStopReason(stopReason) && !turn.HasObservableOutput {
		problem := agentproto.ErrorInfo{
			Severity:         agentproto.ErrorSeverityError,
			Code:             "opencode_empty_response",
			Layer:            "wrapper",
			Stage:            "observe_server",
			Operation:        "session/prompt",
			Message:          "OpenCode 返回了空响应，请检查模型端点和协议配置。",
			Details:          fmt.Sprintf("ACP session/prompt returned stopReason=%q without observable output.", strings.TrimSpace(stopReason)),
			SurfaceSessionID: pending.Command.Origin.Surface,
			CommandID:        turn.CommandID,
			ThreadID:         turn.ThreadID,
			TurnID:           turn.TurnID,
		}.Normalize()
		completed.Status = "failed"
		completed.Problem = &problem
	}
	if turn.Traffic != "" {
		completed.TrafficClass = turn.Traffic
	}
	result.Events = append(result.Events, completed)
	turn.Completed = true
	t.dropUnstartedTurnItems(turn)
	delete(t.activeTurns, turn.ThreadID)
	return result
}

func (t *Translator) dropUnstartedTurnItems(turn *turnState) {
	if turn == nil {
		return
	}
	for key, item := range t.messageItems {
		if item == nil || item.Started || item.ThreadID != turn.ThreadID || item.TurnID != turn.TurnID {
			continue
		}
		delete(t.messageItems, key)
	}
}

func (t *Translator) completeOpenTextItems(turn *turnState) []agentproto.Event {
	if turn == nil {
		return nil
	}
	type pendingItem struct {
		key  string
		item *itemState
	}
	var pending []pendingItem
	for key, item := range t.messageItems {
		if item == nil || item.ThreadID != turn.ThreadID || item.TurnID != turn.TurnID || !item.Started || item.Completed {
			continue
		}
		switch item.Kind {
		case "reasoning_summary", "agent_message":
			pending = append(pending, pendingItem{key: key, item: item})
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		left := textItemCompletionPriority(pending[i].item.Kind)
		right := textItemCompletionPriority(pending[j].item.Kind)
		if left != right {
			return left < right
		}
		return pending[i].key < pending[j].key
	})
	events := make([]agentproto.Event, 0, len(pending))
	for _, current := range pending {
		current.item.Completed = true
		events = append(events, t.annotateTurnEvent(turn, agentproto.Event{
			Kind:     agentproto.EventItemCompleted,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			ItemID:   current.item.ItemID,
			ItemKind: current.item.Kind,
			Status:   "completed",
		}))
	}
	return events
}

func textItemCompletionPriority(kind string) int {
	switch kind {
	case "reasoning_summary":
		return 0
	case "agent_message":
		return 1
	default:
		return 2
	}
}
