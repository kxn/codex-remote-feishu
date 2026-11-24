package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// recordPrivateOfflineStop preserves the user's request without pretending
// that an offline native turn has stopped or sending to a guessed target.
func (s *Service) recordPrivateOfflineStop(action control.Action) []eventcontract.Event {
	v := s.root.Surfaces[action.SurfaceSessionID]
	if v == nil {
		return nil
	}
	cp, e := s.ExportPrivateCheckpoint(v.SurfaceSessionID)
	if e != nil {
		return notice(v, "private_recovery_hold", "无法保存停止请求，请稍后重试。")
	}
	intent := PrivateOfflineStop{InstanceID: v.AttachedInstanceID, SourceMessageID: action.MessageID}
	intent.QueueItemID = cp.ActiveQueueItemID
	matches := 0
	for _, h := range cp.Holds {
		if h.Kind != "active_remote" && h.Kind != "pending_remote" {
			continue
		}
		if intent.QueueItemID != "" && h.QueueItemID != intent.QueueItemID {
			continue
		}
		if intent.InstanceID == "" {
			intent.InstanceID = h.InstanceID
		}
		if h.InstanceID == "" || h.InstanceID != intent.InstanceID {
			return notice(v, "private_recovery_hold", "旧执行归属不一致，暂时无法提交停止请求。")
		}
		matches++
		if matches > 1 {
			return notice(v, "private_recovery_hold", "旧执行归属不唯一，暂时无法提交停止请求。")
		}
		intent.ThreadID = h.ThreadID
		intent.TurnID = h.TurnID
		intent.CommandID = h.CommandID
		if intent.QueueItemID == "" {
			intent.QueueItemID = h.QueueItemID
		}
	}
	events := s.discardDrafts(v)
	if matches == 0 && cp.ActiveQueueItemID == "" {
		if len(cp.Holds) == 0 {
			delete(s.privateRecoveryHolds, v.SurfaceSessionID)
		}
		return append(events, notice(v, "stop_no_active_turn", "已清空排队输入；当前没有可识别的运行执行。")...)
	}
	if intent.InstanceID == "" {
		return append(events, notice(v, "stop_no_active_turn", "已清空排队输入；当前没有可识别的运行环境。")...)
	}
	s.privateOfflineStops[v.SurfaceSessionID] = intent
	return append(events, notice(v, "stop_pending_offline", "已记录停止请求。连接恢复后会向原执行发送；停止结果尚未确认。")...)
}

// ResumePrivateOfflineStop is called only while handling the exact authenticated
// carrier. It persists the interrupt as a normal effect with the checkpoint.
func (s *Service) ResumePrivateOfflineStop(surfaceID, instanceID string) []eventcontract.Event {
	intent, ok := s.privateOfflineStops[surfaceID]
	if !ok || intent.InstanceID != instanceID || s.PrivateDetachedInstance(surfaceID, instanceID) {
		return nil
	}
	binding := s.activeRemoteBindingForSurface(s.root.Surfaces[surfaceID])
	if binding == nil || binding.InstanceID != instanceID || (intent.CommandID == "" && intent.QueueItemID == "") || (intent.CommandID != "" && binding.CommandID != intent.CommandID) || (intent.QueueItemID != "" && binding.QueueItemID != intent.QueueItemID) {
		return nil
	}
	if (intent.ThreadID != "" && binding.ThreadID != intent.ThreadID) || (intent.TurnID != "" && binding.TurnID != intent.TurnID) {
		return nil
	}
	intent.ThreadID = binding.ThreadID
	intent.TurnID = binding.TurnID
	if intent.ThreadID == "" || intent.TurnID == "" {
		return nil
	}
	v := s.root.Surfaces[surfaceID]
	if v == nil {
		return nil
	}
	delete(s.privateOfflineStops, surfaceID)
	return []eventcontract.Event{{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: surfaceID, Command: &agentproto.Command{Kind: agentproto.CommandTurnInterrupt, Origin: agentproto.Origin{Surface: surfaceID, UserID: v.ActorUserID, ChatID: v.ChatID, MessageID: intent.SourceMessageID}, Target: agentproto.Target{ThreadID: intent.ThreadID, TurnID: intent.TurnID}}}}
}

// A correlated native terminal settles only the matching stop request; it must
// not cause a fresh interrupt after the turn has already ended.
func (s *Service) ResolvePrivateOfflineStopTerminal(surfaceID, instanceID, commandID, threadID, turnID string) {
	intent, ok := s.privateOfflineStops[surfaceID]
	if !ok || intent.InstanceID != instanceID || threadID == "" || turnID == "" {
		return
	}
	if intent.CommandID == "" || commandID != intent.CommandID {
		return
	}
	if intent.ThreadID != "" && intent.ThreadID != threadID {
		return
	}
	if intent.TurnID != "" && intent.TurnID != turnID {
		return
	}
	delete(s.privateOfflineStops, surfaceID)
}
