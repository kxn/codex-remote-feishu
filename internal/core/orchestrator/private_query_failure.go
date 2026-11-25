package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// HandlePrivateQueryFailure settles only a proven negative completion for the
// exact requester. Both native prewrite rejection and host-proven not-sent use
// this transition; neither fabricates a successful native response.
func (s *Service) HandlePrivateQueryFailure(surfaceID, instanceID, commandID string, cause error) ([]eventcontract.Event, bool) {
	if !s.config.PrivateApplication || surfaceID == "" || instanceID == "" || commandID == "" {
		return nil, false
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil, false
	}
	reason := "本次请求未完成，请重试。"
	if cause != nil && cause.Error() != "" {
		reason = cause.Error()
	}
	if record := s.activeThreadHistory(surface); record != nil && record.QueryCommandID == commandID && record.QueryInstanceID == instanceID {
		record.QueryCommandID = ""
		return s.HandleSurfaceThreadHistoryFailure(surfaceID, "history_query_failed", reason), true
	}
	if runtime := s.surfaceUIRuntimeState(surface); runtime != nil {
		if pending, ok := runtime.PrivateMCPOAuth[commandID]; ok && pending.InstanceID == instanceID {
			delete(runtime.PrivateMCPOAuth, commandID)
			return MCPOAuthFailureNotice(surfaceID, pending.ServerName, reason), true
		}
	}
	return nil, false
}

// HandlePrivateOwnershipQueryFailure settles a local ownership rejection for
// an exact read. It never treats the native result as a failure receipt.
func (s *Service) HandlePrivateOwnershipQueryFailure(surfaceID, instanceID, commandID string, cause error) ([]eventcontract.Event, bool) {
	if !s.config.PrivateApplication || surfaceID == "" || instanceID == "" || commandID == "" {
		return nil, false
	}
	if q, ok := s.goalUserCommands[commandID]; ok && q.SurfaceID == surfaceID && q.InstanceID == instanceID {
		return s.failGoalUserCommand(surfaceID, commandID, cause), true
	}
	if key := s.goalProbeByCommand[commandID]; key != "" {
		lease := s.goalInterlocks[key]
		if lease != nil && lease.InstanceID == instanceID && lease.TriggerSurfaceID == surfaceID && lease.ProbeCommandID == commandID {
			delete(s.goalProbeByCommand, commandID)
			lease.ProbeCommandID = ""
			lease.ExternalMutationSeen = true
			lease.Phase = GoalInterlockOwnershipLost
			lease.UpdatedAt = s.now()
			return goalInterlockDiagnosticNotice(surfaceID, "goal_owner_changed", "会话归属已变化", "原会话已被其他载体接管，排队输入不会自动发送。请使用 /detach 后重新选择会话。"), true
		}
	}
	if key := s.goalGetByCommand[commandID]; key != "" {
		lease := s.goalInterlocks[key]
		if lease != nil && lease.InstanceID == instanceID && lease.TriggerSurfaceID == surfaceID && lease.GetCommandID == commandID {
			if surface := s.root.Surfaces[surfaceID]; surface != nil && len(surface.QueuedQueueItemIDs) > 0 {
				delete(s.goalGetByCommand, commandID)
				lease.GetCommandID = ""
				lease.Phase = GoalInterlockOwnershipLost
				lease.ExternalMutationSeen = true
				lease.UpdatedAt = s.now()
				return goalInterlockDiagnosticNotice(surfaceID, "goal_owner_changed", "会话归属已变化", "原会话已被其他载体接管，排队输入不会自动发送。请使用 /detach 后重新选择会话。"), true
			}
			s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
			return goalInterlockDiagnosticNotice(surfaceID, "goal_owner_changed", "会话归属已变化", "无法确认原会话的 Goal，已放弃自动恢复。请重新选择会话。"), true
		}
	}
	if s.PrivateReconnectQueryMatches(surfaceID, instanceID, commandID) {
		delete(s.privateReconnectQueries, surfaceID)
		return notice(s.root.Surfaces[surfaceID], "private_reconnect_thread_unavailable", "原会话的归属已变化，无法读取；请使用 /detach 后 /list 或 /new 选择其他会话。"), true
	}
	return s.HandlePrivateQueryFailure(surfaceID, instanceID, commandID, cause)
}

// HandlePrivateReadObservationTimeout ends only this local wait. The original
// native dispatch remains unknown until actual evidence arrives.
func (s *Service) HandlePrivateReadObservationTimeout(surfaceID, instanceID, commandID string, cause error) ([]eventcontract.Event, bool) {
	if !s.config.PrivateApplication || surfaceID == "" || instanceID == "" || commandID == "" {
		return nil, false
	}
	if q, ok := s.goalUserCommands[commandID]; ok && q.SurfaceID == surfaceID && q.InstanceID == instanceID && q.Action == "get" {
		return s.failGoalUserCommand(surfaceID, commandID, fmt.Errorf("等待 Goal 查询结果超时，查询可能仍在进行；请重新查询。")), true
	}
	if key := s.goalProbeByCommand[commandID]; key != "" {
		lease := s.goalInterlocks[key]
		if lease != nil && lease.InstanceID == instanceID && lease.TriggerSurfaceID == surfaceID && lease.ProbeCommandID == commandID {
			delete(s.goalProbeByCommand, commandID)
			lease.ProbeCommandID = ""
			lease.Phase = GoalInterlockObservationTimeout
			lease.UpdatedAt = s.now()
			return goalInterlockDiagnosticNotice(surfaceID, "goal_observation_timeout", "等待会话状态超时", "尚未确认原会话空闲，排队输入会保留且不会自动发送。可发送 /goal retry 重新确认，或使用 /detach 退出。"), true
		}
	}
	if key := s.goalGetByCommand[commandID]; key != "" {
		lease := s.goalInterlocks[key]
		if lease != nil && lease.InstanceID == instanceID && lease.TriggerSurfaceID == surfaceID && lease.GetCommandID == commandID {
			if surface := s.root.Surfaces[surfaceID]; lease.Phase == GoalInterlockVerificationGetPending || (surface != nil && len(surface.QueuedQueueItemIDs) > 0) {
				return s.requireGoalVerification(lease, "等待 Goal 状态超时，排队输入仍保留。请发送 /goal retry 重新只读确认，或使用 /detach 退出。"), true
			}
			s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
			return goalInterlockDiagnosticNotice(surfaceID, "goal_observation_timeout", "等待 Goal 状态超时", "未能确认 Goal 状态，已停止自动恢复；可重新查询 Goal。"), true
		}
	}
	if s.PrivateReconnectQueryMatches(surfaceID, instanceID, commandID) {
		delete(s.privateReconnectQueries, surfaceID)
		return notice(s.root.Surfaces[surfaceID], "private_reconnect_query_timeout", "等待原会话查询结果超时，原输入会保留。可使用 /detach 后重新选择会话再试。"), true
	}
	if surface := s.root.Surfaces[surfaceID]; surface != nil {
		if runtime := s.surfaceUIRuntimeState(surface); runtime != nil {
			if pending, ok := runtime.PrivateMCPOAuth[commandID]; ok && pending.InstanceID == instanceID {
				delete(runtime.PrivateMCPOAuth, commandID)
				return MCPOAuthNotice(surfaceID, "mcp_oauth_observation_timeout", "等待认证链接超时，原请求结果尚未确认；可以继续聊天，如需退出等待可使用 /detach 后重新选择会话。", pending.ServerName), true
			}
		}
	}
	// History callbacks are exact-command and give the user a retry.
	return s.HandlePrivateQueryFailure(surfaceID, instanceID, commandID, fmt.Errorf("等待查询结果超时，查询可能仍在进行；请重试。"))
}
