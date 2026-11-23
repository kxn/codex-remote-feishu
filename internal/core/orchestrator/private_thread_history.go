package orchestrator

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Private history uses the same picker and renderer as the legacy daemon. Its
// query correlation lives with the picker, so a restart cannot lose the result
// or let an older query overwrite a more recently opened history card.
func (s *Service) privateHistoryQuery(surface *state.SurfaceConsoleRecord, instance *state.InstanceRecord, record *activeThreadHistoryRecord, source string) eventcontract.Event {
	record.QueryCommandID = s.nextAgentCommandID()
	record.QueryInstanceID = instance.InstanceID
	return eventcontract.Event{Kind: eventcontract.KindAgentCommand, GatewayID: surface.GatewayID, SurfaceSessionID: surface.SurfaceSessionID, SourceMessageID: source, Command: &agentproto.Command{
		CommandID: record.QueryCommandID, Kind: agentproto.CommandThreadHistoryRead,
		Origin: agentproto.Origin{Surface: surface.SurfaceSessionID, ChatID: surface.ChatID, MessageID: source}, Target: agentproto.Target{ThreadID: record.ThreadID},
	}}
}

func (s *Service) applyPrivateHistoryEvent(instance string, event agentproto.Event) ([]eventcontract.Event, bool) {
	if !s.config.PrivateApplication || event.CommandID == "" || (event.Kind != agentproto.EventThreadHistoryRead && event.Kind != agentproto.EventSystemError && event.Kind != agentproto.EventNativeCommandRejected) {
		return nil, false
	}
	for _, surface := range s.root.Surfaces {
		record := s.activeThreadHistory(surface)
		if record == nil || record.QueryCommandID != event.CommandID || record.QueryInstanceID != instance {
			continue
		}
		if event.Kind != agentproto.EventThreadHistoryRead || event.ThreadHistory == nil {
			text := event.ErrorMessage
			if text == "" {
				text = "这次历史记录查询没有完成，请重新刷新历史记录。"
			}
			return s.HandlePrivateQueryFailure(surface.SurfaceSessionID, instance, event.CommandID, errors.New(text))
		}
		record.QueryCommandID = ""
		if record.ThreadID != event.ThreadID && record.ThreadID != event.ThreadHistory.Thread.ThreadID {
			return s.HandleSurfaceThreadHistoryFailure(surface.SurfaceSessionID, "history_query_stale", "历史记录的会话已经变化，请重新发送 /history。"), true
		}
		s.RecordSurfaceThreadHistory(surface.SurfaceSessionID, *event.ThreadHistory)
		return s.HandleSurfaceThreadHistoryLoaded(surface.SurfaceSessionID), true
	}
	// A stale history response is consumed without affecting another picker.
	return nil, event.Kind == agentproto.EventThreadHistoryRead
}
