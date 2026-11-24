package orchestrator

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"time"
)

// BeginPrivateReconnectQuery asks the exact authenticated carrier about the
// previously selected thread. It is an internal query, not a /history card.
func (s *Service) BeginPrivateReconnectQuery(surfaceID, instanceID string) []eventcontract.Event {
	if !s.config.PrivateApplication || s.PrivateDetachedInstance(surfaceID, instanceID) {
		return nil
	}
	v := s.root.Surfaces[surfaceID]
	inst := s.root.Instances[instanceID]
	if v == nil || inst == nil || !inst.Online || v.AttachedInstanceID != instanceID || v.RouteMode != state.RouteModePinned || v.SelectedThreadID == "" || inst.Threads[v.SelectedThreadID] != nil {
		return nil
	}
	if existing, exists := s.privateReconnectQueries[surfaceID]; exists {
		if existing.InstanceID == instanceID && existing.ThreadID == v.SelectedThreadID {
			return nil
		}
		delete(s.privateReconnectQueries, surfaceID)
	}
	q := PrivateReconnectQuery{InstanceID: instanceID, ThreadID: v.SelectedThreadID, CommandID: s.nextAgentCommandID(), Deadline: s.now().UTC().Add(30 * time.Second)}
	s.privateReconnectQueries[surfaceID] = q
	return []eventcontract.Event{{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: surfaceID, GatewayID: v.GatewayID, Command: &agentproto.Command{CommandID: q.CommandID, Kind: agentproto.CommandThreadHistoryRead, Origin: agentproto.Origin{Surface: surfaceID, UserID: v.ActorUserID, ChatID: v.ChatID}, Target: agentproto.Target{ThreadID: q.ThreadID}}}}
}

// PrivateReconnectQueryMatches is checked only after the native journal has
// established the command/event receipt and exact carrier binding.
func (s *Service) PrivateReconnectQueryMatches(surfaceID, instanceID, commandID string) bool {
	q, ok := s.privateReconnectQueries[surfaceID]
	v := s.root.Surfaces[surfaceID]
	return ok && v != nil && v.RouteMode == state.RouteModePinned && v.AttachedInstanceID == instanceID && v.SelectedThreadID == q.ThreadID && q.InstanceID == instanceID && q.CommandID == commandID
}

func (s *Service) CompletePrivateReconnectQuery(surfaceID, instanceID string, event agentproto.Event, replayAllowed bool) []eventcontract.Event {
	q, ok := s.privateReconnectQueries[surfaceID]
	if !ok || q.InstanceID != instanceID || q.CommandID != event.CommandID {
		return nil
	}
	delete(s.privateReconnectQueries, surfaceID)
	v := s.root.Surfaces[surfaceID]
	inst := s.root.Instances[instanceID]
	if v == nil || inst == nil || !inst.Online || v.AttachedInstanceID != instanceID || v.RouteMode != state.RouteModePinned || v.SelectedThreadID != q.ThreadID {
		return nil
	}
	if event.Kind != agentproto.EventThreadHistoryRead || event.ThreadHistory == nil || event.ThreadHistory.Thread.ThreadID != q.ThreadID || event.ThreadHistory.Thread.CWD == "" {
		return notice(v, "private_reconnect_thread_unavailable", "原会话暂时不可用，请使用 /detach 后 /list 或 /new 选择其他会话。")
	}
	thread := event.ThreadHistory.Thread
	events := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: thread.ThreadID, CWD: thread.CWD, Name: thread.Name, Preview: thread.Preview, Model: thread.Model, ReasoningEffort: thread.ReasoningEffort, AccessMode: thread.AccessMode})
	if !s.claimThread(v, inst, q.ThreadID) {
		return append(events, notice(v, "private_reconnect_thread_unavailable", "原会话当前不可接入，请使用 /detach 后 /list 或 /new 选择其他会话。")...)
	}
	if replayAllowed {
		s.restorePrivateReconnectPendingText(surfaceID)
	}
	if !replayAllowed && s.PrivateReconnectHasPendingText(surfaceID) {
		return append(events, notice(v, "private_reconnect_input_held", "之前的输入仍在核对，请使用 /detach 后 /list 或 /new 继续；不会自动转发到其他会话。")...)
	}
	if pending := s.takePendingTextInput(v); pending != nil {
		return append(events, s.replayPendingTextInput(v, pending)...)
	}
	return nil
}

// PrivateReconnectPendingText returns only the original frozen user input. It
// never reads migration's claimed-input or detached execution records.
func (s *Service) PrivateReconnectPendingText(surfaceID string) (*state.PendingTextInputRecord, bool) {
	v := s.root.Surfaces[surfaceID]
	if v == nil {
		return nil, false
	}
	if v.PendingTextInput != nil {
		return v.PendingTextInput, true
	}
	held, ok := s.privateRecoveryHolds[surfaceID]
	if !ok || held.ActiveQueueItemID != "" || len(held.QueuedQueueItemIDs) > 0 || held.OfflineStop != nil {
		return nil, false
	}
	var pending *state.PendingTextInputRecord
	for _, h := range held.Holds {
		if h.Kind != "pending_text" {
			return nil, false
		}
		if pending != nil {
			return nil, false
		}
		var x state.PendingTextInputRecord
		if json.Unmarshal(h.Frozen, &x) != nil {
			return nil, false
		}
		pending = &x
	}
	return pending, pending != nil
}
func (s *Service) PrivateReconnectHasPendingText(surfaceID string) bool {
	_, ok := s.PrivateReconnectPendingText(surfaceID)
	return ok
}
func (s *Service) restorePrivateReconnectPendingText(surfaceID string) {
	v := s.root.Surfaces[surfaceID]
	if v == nil || v.PendingTextInput != nil {
		return
	}
	pending, ok := s.PrivateReconnectPendingText(surfaceID)
	if !ok {
		return
	}
	delete(s.privateRecoveryHolds, surfaceID)
	s.restoreSurfaceDispatchNormal(v)
	v.PendingTextInput = pending
}

// PrivateReconnectInputUnavailable rejects a new input until the original
// selected thread's navigation is established. Control actions remain usable.
func (s *Service) PrivateReconnectInputUnavailable(surface string) bool {
	if _, ok := s.PrivateReplacementInputTarget(surface); ok {
		return false
	}
	v := s.root.Surfaces[surface]
	if v == nil || v.RouteMode != state.RouteModePinned || v.AttachedInstanceID == "" || v.SelectedThreadID == "" {
		return false
	}
	inst := s.root.Instances[v.AttachedInstanceID]
	return inst == nil || !inst.Online || inst.Threads[v.SelectedThreadID] == nil || !s.surfaceOwnsThread(v, v.SelectedThreadID)
}

// PrivateReconnectProfileChanged identifies a settled launch's configuration
// mismatch so the existing unavailable-input notice can be actionable.
func (s *Service) PrivateReconnectProfileChanged(surface string) bool {
	v := s.root.Surfaces[surface]
	n, ok := s.privateNavigation[surface]
	if v == nil || !ok {
		return false
	}
	c := s.headlessLaunchContract(v)
	switch n.Owner.Backend {
	case agentproto.BackendCodex:
		return state.NormalizeCodexProfileID(c.CodexProfileID) != state.NormalizeCodexProfileID(n.Actual.Codex)
	case agentproto.BackendClaude:
		return state.NormalizeClaudeProfileID(c.ClaudeProfileID) != state.NormalizeClaudeProfileID(n.Actual.Claude)
	case agentproto.BackendOpenCode:
		return state.NormalizeOpenCodeProfileID(c.OpenCodeProfileID) != state.NormalizeOpenCodeProfileID(n.Actual.OpenCode)
	}
	return true
}
