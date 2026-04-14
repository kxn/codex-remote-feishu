package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (a *App) handleThreadHistoryDaemonCommand(command control.DaemonCommand) []control.UIEvent {
	surfaceID := strings.TrimSpace(command.SurfaceSessionID)
	instanceID := strings.TrimSpace(command.InstanceID)
	threadID := strings.TrimSpace(command.ThreadID)
	if surfaceID == "" || instanceID == "" || threadID == "" {
		if events := a.service.HandleSurfaceThreadHistoryFailure(surfaceID, "history_query_invalid", "history 查询参数不完整。"); len(events) != 0 {
			return events
		}
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surfaceID,
			Notice: &control.Notice{
				Code: "history_query_invalid",
				Text: "history 查询参数不完整。",
			},
		}}
	}

	commandID := a.nextCommandID()
	agentCommand := agentproto.Command{
		CommandID: commandID,
		Kind:      agentproto.CommandThreadHistoryRead,
		Origin: agentproto.Origin{
			Surface:   surfaceID,
			ChatID:    command.SourceMessageID,
			MessageID: command.SourceMessageID,
		},
		Target: agentproto.Target{
			ThreadID: threadID,
		},
	}
	if err := a.sendAgentCommand(instanceID, agentCommand); err != nil {
		if events := a.service.HandleSurfaceThreadHistoryFailure(surfaceID, "history_query_dispatch_failed", "history 查询未成功发送到本地 Codex。"); len(events) != 0 {
			return events
		}
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surfaceID,
			Notice: &control.Notice{
				Code: "history_query_dispatch_failed",
				Text: "history 查询未成功发送到本地 Codex。",
			},
		}}
	}
	a.pendingThreadHistoryReads[commandID] = pendingThreadHistoryRead{
		SurfaceSessionID: surfaceID,
		InstanceID:       instanceID,
		ThreadID:         threadID,
	}
	return nil
}

func (a *App) handleThreadHistoryCommandAckLocked(instanceID string, ack agentproto.CommandAck) ([]control.UIEvent, bool) {
	commandID := strings.TrimSpace(ack.CommandID)
	pending, ok := a.pendingThreadHistoryReads[commandID]
	if !ok {
		return nil, false
	}
	if ack.Accepted {
		return nil, true
	}
	delete(a.pendingThreadHistoryReads, commandID)
	if events := a.service.HandleSurfaceThreadHistoryFailure(pending.SurfaceSessionID, "history_query_rejected", "本地 Codex 拒绝了这次 history 查询。"); len(events) != 0 {
		return events, true
	}
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: pending.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "history_query_rejected",
			Text: "本地 Codex 拒绝了这次 history 查询。",
		},
	}}, true
}

func (a *App) handleThreadHistoryEventLocked(instanceID string, event agentproto.Event) ([]control.UIEvent, bool) {
	if event.Kind != agentproto.EventThreadHistoryRead || strings.TrimSpace(event.CommandID) == "" || event.ThreadHistory == nil {
		return nil, false
	}
	pending, ok := a.pendingThreadHistoryReads[strings.TrimSpace(event.CommandID)]
	if !ok {
		return nil, false
	}
	delete(a.pendingThreadHistoryReads, strings.TrimSpace(event.CommandID))
	if pending.InstanceID != "" && pending.InstanceID != strings.TrimSpace(instanceID) {
		return nil, true
	}
	if pending.ThreadID != "" && pending.ThreadID != strings.TrimSpace(event.ThreadID) && pending.ThreadID != strings.TrimSpace(event.ThreadHistory.Thread.ThreadID) {
		if events := a.service.HandleSurfaceThreadHistoryFailure(pending.SurfaceSessionID, "history_query_stale", "history 查询结果已过期。"); len(events) != 0 {
			return events, true
		}
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: pending.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "history_query_stale",
				Text: "history 查询结果已过期。",
			},
		}}, true
	}
	a.service.RecordSurfaceThreadHistory(pending.SurfaceSessionID, *event.ThreadHistory)
	return a.service.HandleSurfaceThreadHistoryLoaded(pending.SurfaceSessionID), true
}
