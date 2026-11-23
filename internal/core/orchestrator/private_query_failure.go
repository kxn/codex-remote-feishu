package orchestrator

import (
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
