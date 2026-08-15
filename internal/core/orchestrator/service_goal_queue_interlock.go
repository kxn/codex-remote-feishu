package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type GoalInterlockPhase string

const (
	GoalInterlockNone          GoalInterlockPhase = "none"
	GoalInterlockPausePending  GoalInterlockPhase = "pause_pending"
	GoalInterlockQuiescing     GoalInterlockPhase = "quiescing"
	GoalInterlockDraining      GoalInterlockPhase = "draining"
	GoalInterlockResumePending GoalInterlockPhase = "resume_pending"
)

type GoalInterlockLease struct {
	InstanceID           string             `json:"instanceId,omitempty"`
	ThreadID             string             `json:"threadId,omitempty"`
	Phase                GoalInterlockPhase `json:"phase,omitempty"`
	GoalCreatedAt        int64              `json:"goalCreatedAt,omitempty"`
	PausedUpdatedAt      int64              `json:"pausedUpdatedAt,omitempty"`
	Objective            string             `json:"objective,omitempty"`
	TokenBudget          *int64             `json:"tokenBudget,omitempty"`
	TriggerSurfaceID     string             `json:"triggerSurfaceId,omitempty"`
	TriggerQueueItemID   string             `json:"triggerQueueItemId,omitempty"`
	PauseCommandID       string             `json:"pauseCommandId,omitempty"`
	ResumeCommandID      string             `json:"resumeCommandId,omitempty"`
	ProbeCommandID       string             `json:"probeCommandId,omitempty"`
	GetCommandID         string             `json:"getCommandId,omitempty"`
	ExternalMutationSeen bool               `json:"externalMutationSeen,omitempty"`
	UpdatedAt            time.Time          `json:"updatedAt,omitempty"`
}

func (s *Service) GoalInterlockLeases() []GoalInterlockLease {
	if s == nil || len(s.goalInterlocks) == 0 {
		return nil
	}
	leases := make([]GoalInterlockLease, 0, len(s.goalInterlocks))
	for _, lease := range s.goalInterlocks {
		if lease == nil {
			continue
		}
		cloned := *lease
		if lease.TokenBudget != nil {
			budget := *lease.TokenBudget
			cloned.TokenBudget = &budget
		}
		leases = append(leases, cloned)
	}
	return leases
}

func (s *Service) RestoreGoalInterlockLeases(leases []GoalInterlockLease) {
	s.goalInterlocks = map[string]*GoalInterlockLease{}
	s.goalInterlockByCommand = map[string]string{}
	s.goalProbeByCommand = map[string]string{}
	s.goalGetByCommand = map[string]string{}
	for _, lease := range leases {
		cloned := lease
		if lease.TokenBudget != nil {
			budget := *lease.TokenBudget
			cloned.TokenBudget = &budget
		}
		s.setGoalInterlockLease(&cloned)
		if cloned.PauseCommandID != "" {
			s.goalInterlockByCommand[cloned.PauseCommandID] = goalInterlockKey(cloned.InstanceID, cloned.ThreadID)
		}
		if cloned.ResumeCommandID != "" {
			s.goalInterlockByCommand[cloned.ResumeCommandID] = goalInterlockKey(cloned.InstanceID, cloned.ThreadID)
		}
		if cloned.ProbeCommandID != "" {
			s.goalProbeByCommand[cloned.ProbeCommandID] = goalInterlockKey(cloned.InstanceID, cloned.ThreadID)
		}
		if cloned.GetCommandID != "" {
			s.goalGetByCommand[cloned.GetCommandID] = goalInterlockKey(cloned.InstanceID, cloned.ThreadID)
		}
	}
}

// ReconcileGoalInterlocks 在 daemon 重启/实例重连后重发丢失的 pause/probe/get 命令，
// 让非 draining lease 继续推进；draining lease 直接恢复执行。
func (s *Service) ReconcileGoalInterlocks() []eventcontract.Event {
	if s == nil || len(s.goalInterlocks) == 0 {
		return nil
	}
	var events []eventcontract.Event
	for _, lease := range s.goalInterlocks {
		if lease == nil {
			continue
		}
		inst := s.root.Instances[lease.InstanceID]
		if inst == nil || !inst.Online {
			continue
		}
		switch lease.Phase {
		case GoalInterlockPausePending:
			thread := inst.Threads[lease.ThreadID]
			if thread != nil && thread.ThreadGoal != nil && thread.ThreadGoal.Status == "paused" {
				lease.Phase = GoalInterlockQuiescing
				events = append(events, s.probeGoalThreadIdle(lease)...)
				continue
			}
			if s.threadHasActiveGoal(lease.InstanceID, lease.ThreadID) {
				events = append(events, s.resendGoalPause(lease)...)
			}
		case GoalInterlockQuiescing:
			events = append(events, s.probeGoalThreadIdle(lease)...)
		case GoalInterlockResumePending:
			events = append(events, s.resendGoalGet(lease)...)
		}
	}
	return events
}

func (s *Service) resendGoalPause(lease *GoalInterlockLease) []eventcontract.Event {
	commandID := s.nextAgentCommandID()
	if s.goalInterlockByCommand != nil && lease.PauseCommandID != "" {
		delete(s.goalInterlockByCommand, lease.PauseCommandID)
	}
	lease.PauseCommandID = commandID
	if s.goalInterlockByCommand == nil {
		s.goalInterlockByCommand = map[string]string{}
	}
	s.goalInterlockByCommand[commandID] = goalInterlockKey(lease.InstanceID, lease.ThreadID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: lease.TriggerSurfaceID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandThreadGoalSet,
			Target:    agentproto.Target{ThreadID: lease.ThreadID},
			Goal: agentproto.GoalCommand{
				Status:  "paused",
				Purpose: "queue_interlock",
			},
		},
	}}
}

func (s *Service) resendGoalGet(lease *GoalInterlockLease) []eventcontract.Event {
	commandID := s.nextAgentCommandID()
	if s.goalGetByCommand != nil && lease.GetCommandID != "" {
		delete(s.goalGetByCommand, lease.GetCommandID)
	}
	lease.GetCommandID = commandID
	if s.goalGetByCommand == nil {
		s.goalGetByCommand = map[string]string{}
	}
	s.goalGetByCommand[commandID] = goalInterlockKey(lease.InstanceID, lease.ThreadID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: lease.TriggerSurfaceID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandThreadGoalGet,
			Target:    agentproto.Target{ThreadID: lease.ThreadID},
		},
	}}
}

func goalInterlockKey(instanceID, threadID string) string {
	return strings.TrimSpace(instanceID) + "\x00" + strings.TrimSpace(threadID)
}

func (s *Service) goalInterlockLease(instanceID, threadID string) *GoalInterlockLease {
	if s.goalInterlocks == nil {
		return nil
	}
	return s.goalInterlocks[goalInterlockKey(instanceID, threadID)]
}

func (s *Service) setGoalInterlockLease(lease *GoalInterlockLease) {
	if lease == nil || strings.TrimSpace(lease.InstanceID) == "" || strings.TrimSpace(lease.ThreadID) == "" {
		return
	}
	if s.goalInterlocks == nil {
		s.goalInterlocks = map[string]*GoalInterlockLease{}
	}
	s.goalInterlocks[goalInterlockKey(lease.InstanceID, lease.ThreadID)] = lease
}

func (s *Service) clearGoalInterlockLease(instanceID, threadID string) {
	if s.goalInterlocks == nil {
		return
	}
	key := goalInterlockKey(instanceID, threadID)
	lease := s.goalInterlocks[key]
	if lease != nil {
		if s.goalInterlockByCommand != nil && lease.PauseCommandID != "" {
			delete(s.goalInterlockByCommand, lease.PauseCommandID)
		}
		if s.goalInterlockByCommand != nil && lease.ResumeCommandID != "" {
			delete(s.goalInterlockByCommand, lease.ResumeCommandID)
		}
		if s.goalProbeByCommand != nil && lease.ProbeCommandID != "" {
			delete(s.goalProbeByCommand, lease.ProbeCommandID)
		}
		if s.goalGetByCommand != nil && lease.GetCommandID != "" {
			delete(s.goalGetByCommand, lease.GetCommandID)
		}
	}
	delete(s.goalInterlocks, key)
}

func (s *Service) revokeGoalInterlockForSurface(surfaceID string) {
	if s == nil || len(s.goalInterlocks) == 0 {
		return
	}
	surfaceID = strings.TrimSpace(surfaceID)
	for key, lease := range s.goalInterlocks {
		if lease != nil && strings.TrimSpace(lease.TriggerSurfaceID) == surfaceID {
			s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
			_ = key
		}
	}
}

func (s *Service) revokeGoalInterlockForInstance(instanceID string) {
	if s == nil || len(s.goalInterlocks) == 0 {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	for key, lease := range s.goalInterlocks {
		if lease != nil && strings.TrimSpace(lease.InstanceID) == instanceID {
			s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
			_ = key
		}
	}
}

// beginGoalInterlock 创建 pause_pending lease 并发 thread/goal/set(paused)，
// 用于让普通用户队列优先于 Goal continuation。
func (s *Service) beginGoalInterlock(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, item *state.QueueItemRecord) []eventcontract.Event {
	if surface == nil || inst == nil || item == nil {
		return nil
	}
	threadID := queuedItemExecutionThreadID(item)
	if threadID == "" || !s.threadHasActiveGoal(inst.InstanceID, threadID) {
		return nil
	}
	if s.goalInterlockLease(inst.InstanceID, threadID) != nil {
		return nil
	}
	thread := inst.Threads[threadID]
	lease := &GoalInterlockLease{
		InstanceID:         inst.InstanceID,
		ThreadID:           threadID,
		Phase:              GoalInterlockPausePending,
		TriggerSurfaceID:   surface.SurfaceSessionID,
		TriggerQueueItemID: item.ID,
		UpdatedAt:          s.now(),
	}
	if thread != nil && thread.ThreadGoal != nil {
		lease.GoalCreatedAt = thread.ThreadGoal.CreatedAt
		lease.PausedUpdatedAt = thread.ThreadGoal.UpdatedAt
		lease.Objective = thread.ThreadGoal.Objective
		lease.TokenBudget = agentproto.CloneThreadGoalUpdate(thread.ThreadGoal).TokenBudget
	}
	commandID := s.nextAgentCommandID()
	lease.PauseCommandID = commandID
	s.setGoalInterlockLease(lease)
	if s.goalInterlockByCommand == nil {
		s.goalInterlockByCommand = map[string]string{}
	}
	s.goalInterlockByCommand[commandID] = goalInterlockKey(inst.InstanceID, threadID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandThreadGoalSet,
			Target:    agentproto.Target{ThreadID: threadID},
			Goal: agentproto.GoalCommand{
				Status:  "paused",
				Purpose: "queue_interlock",
			},
		},
	}}
}

func (s *Service) maybeBeginGoalInterlockForQueueItem(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, item *state.QueueItemRecord) (bool, []eventcontract.Event) {
	if surface == nil || inst == nil || item == nil {
		return false, nil
	}
	threadID := queuedItemExecutionThreadID(item)
	if threadID == "" || s.goalInterlockLease(inst.InstanceID, threadID) != nil {
		return false, nil
	}
	key := goalInterlockKey(inst.InstanceID, threadID)
	if until, ok := s.goalPauseBackoff[key]; ok && s.now().Before(until) {
		if noticeAt := s.goalPauseNoticeAt[key]; noticeAt.IsZero() || s.now().After(noticeAt) {
			s.goalPauseNoticeAt[key] = s.now().Add(time.Minute)
			return true, goalInterlockDiagnosticNotice(surface.SurfaceSessionID, "goal_pause_backoff", "Goal 暂停失败", "Codex 暂停 Goal 失败，队列暂缓派发。请稍后重试，或先手动暂停/清除 Goal。")
		}
		return true, nil
	}
	delete(s.goalPauseBackoff, key)
	delete(s.goalPauseNoticeAt, key)
	return false, s.beginGoalInterlock(surface, inst, item)
}

func (s *Service) nextAgentCommandID() string {
	s.nextRequestCommandID++
	return "relay-goal-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + itoa(s.nextRequestCommandID)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func (s *Service) applyGoalCommandResult(instanceID string, event agentproto.Event) []eventcontract.Event {
	if s.goalCommandResultKnown(event.CommandID) {
		if inst := s.root.Instances[instanceID]; inst != nil {
			switch {
			case event.ThreadGoal != nil:
				if update := agentproto.NormalizeThreadGoalUpdate(event.ThreadGoal); update != nil {
					thread := s.ensureThread(inst, update.ThreadID)
					thread.ThreadGoal = agentproto.CloneThreadGoalUpdate(update)
					s.touchThread(thread)
				}
			case event.GoalCleared:
				if thread := inst.Threads[event.ThreadID]; thread != nil {
					thread.ThreadGoal = nil
					s.touchThread(thread)
				}
			}
		}
	}
	if events := s.applyGoalUserCommandResult(instanceID, event); len(events) != 0 {
		return events
	}
	if event.CommandID == "" {
		return nil
	}
	key := ""
	if s.goalInterlockByCommand != nil {
		key = s.goalInterlockByCommand[event.CommandID]
	}
	if key != "" {
		return s.applyGoalInterlockCommandResult(key, event)
	}
	if s.goalProbeByCommand != nil {
		key = s.goalProbeByCommand[event.CommandID]
	}
	if key != "" {
		return s.applyGoalProbeResult(key, event)
	}
	if s.goalGetByCommand != nil {
		key = s.goalGetByCommand[event.CommandID]
	}
	if key != "" {
		return s.applyGoalGetResult(key, event)
	}
	return nil
}

func (s *Service) goalCommandResultKnown(commandID string) bool {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return false
	}
	if s.goalUserCommands != nil {
		if _, ok := s.goalUserCommands[commandID]; ok {
			return true
		}
	}
	if s.goalInterlockByCommand != nil {
		if _, ok := s.goalInterlockByCommand[commandID]; ok {
			return true
		}
	}
	if s.goalProbeByCommand != nil {
		if _, ok := s.goalProbeByCommand[commandID]; ok {
			return true
		}
	}
	if s.goalGetByCommand != nil {
		if _, ok := s.goalGetByCommand[commandID]; ok {
			return true
		}
	}
	return false
}

func (s *Service) applyGoalInterlockCommandResult(key string, event agentproto.Event) []eventcontract.Event {
	lease := s.goalInterlocks[key]
	if lease == nil {
		return nil
	}
	lease.UpdatedAt = s.now()
	if event.ErrorMessage != "" {
		events := goalInterlockDiagnosticNotice(lease.TriggerSurfaceID, "goal_pause_failed", "Goal 暂停失败", "Codex 未能暂停当前 Goal，队列暂不派发。请稍后重试或检查 Codex 状态。")
		if s.goalPauseBackoff == nil {
			s.goalPauseBackoff = map[string]time.Time{}
		}
		if s.goalPauseNoticeAt == nil {
			s.goalPauseNoticeAt = map[string]time.Time{}
		}
		s.goalPauseBackoff[key] = s.now().Add(time.Minute)
		s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
		return events
	}
	switch lease.Phase {
	case GoalInterlockPausePending:
		if event.ThreadGoal != nil && event.ThreadGoal.Status == "paused" {
			lease.Phase = GoalInterlockQuiescing
			lease.PausedUpdatedAt = event.ThreadGoal.UpdatedAt
			return s.probeGoalThreadIdle(lease)
		}
		// Goal 未停在 paused（如已被外部 complete/blocked）：无需互锁，直接放行。
		s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
		return nil
	case GoalInterlockResumePending:
		if event.ThreadGoal != nil && event.ThreadGoal.Status == "active" {
			s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
			return nil
		}
		events := goalInterlockDiagnosticNotice(lease.TriggerSurfaceID, "goal_resume_failed", "Goal 恢复失败", "队列已排空，但 Codex 未确认恢复 Goal。请稍后重试或检查 Codex 状态。")
		s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
		return events
	default:
		return nil
	}
}

func (s *Service) probeGoalThreadIdle(lease *GoalInterlockLease) []eventcontract.Event {
	commandID := s.nextAgentCommandID()
	if s.goalProbeByCommand != nil && lease.ProbeCommandID != "" {
		delete(s.goalProbeByCommand, lease.ProbeCommandID)
	}
	lease.ProbeCommandID = commandID
	lease.Phase = GoalInterlockQuiescing
	if s.goalProbeByCommand == nil {
		s.goalProbeByCommand = map[string]string{}
	}
	s.goalProbeByCommand[commandID] = goalInterlockKey(lease.InstanceID, lease.ThreadID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: lease.TriggerSurfaceID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandThreadRead,
			Target:    agentproto.Target{ThreadID: lease.ThreadID},
		},
	}}
}

func (s *Service) applyGoalProbeResult(key string, event agentproto.Event) []eventcontract.Event {
	lease := s.goalInterlocks[key]
	if lease == nil || lease.Phase != GoalInterlockQuiescing {
		return nil
	}
	lease.UpdatedAt = s.now()
	if event.ErrorMessage != "" || event.RuntimeStatus == nil {
		return nil // fail-closed：保持 quiescing，不派发普通队列。
	}
	switch event.RuntimeStatus.Type {
	case agentproto.ThreadRuntimeStatusTypeIdle:
		lease.Phase = GoalInterlockDraining
		return s.pumpGoalInterlockWaiter(lease)
	case agentproto.ThreadRuntimeStatusTypeActive:
		return nil
	default:
		return nil
	}
}

func (s *Service) pumpGoalInterlockWaiter(lease *GoalInterlockLease) []eventcontract.Event {
	if lease == nil {
		return nil
	}
	surface := s.root.Surfaces[lease.TriggerSurfaceID]
	if surface == nil {
		return nil
	}
	if inst := s.root.Instances[lease.InstanceID]; inst != nil && inst.ActiveTurnID != "" {
		return nil
	}
	return s.dispatchNext(surface)
}

func (s *Service) pumpAndResumeGoalInterlock(instanceID, threadID string) []eventcontract.Event {
	var events []eventcontract.Event
	lease := s.goalInterlockLease(instanceID, threadID)
	events = append(events, s.pumpGoalInterlockWaiter(lease)...)
	events = append(events, s.maybeResumeGoalInterlock(instanceID, threadID)...)
	return events
}

func (s *Service) maybeResumeGoalInterlock(instanceID, threadID string) []eventcontract.Event {
	lease := s.goalInterlockLease(instanceID, threadID)
	if lease == nil || lease.Phase != GoalInterlockDraining || lease.ExternalMutationSeen {
		return nil
	}
	surface := s.root.Surfaces[lease.TriggerSurfaceID]
	if surface == nil {
		s.clearGoalInterlockLease(instanceID, threadID)
		return nil
	}
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 0 || s.surfaceHasLiveRemoteWork(surface) {
		return nil
	}
	lease.Phase = GoalInterlockResumePending
	commandID := s.nextAgentCommandID()
	lease.GetCommandID = commandID
	if s.goalGetByCommand == nil {
		s.goalGetByCommand = map[string]string{}
	}
	s.goalGetByCommand[commandID] = goalInterlockKey(instanceID, threadID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: lease.TriggerSurfaceID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandThreadGoalGet,
			Target:    agentproto.Target{ThreadID: threadID},
		},
	}}
}

func (s *Service) applyGoalGetResult(key string, event agentproto.Event) []eventcontract.Event {
	lease := s.goalInterlocks[key]
	if lease == nil || lease.Phase != GoalInterlockResumePending {
		return nil
	}
	if event.ErrorMessage != "" || event.GoalMissing || event.ThreadGoal == nil {
		events := goalInterlockDiagnosticNotice(lease.TriggerSurfaceID, "goal_resume_failed", "Goal 恢复失败", "无法确认 Goal 状态，已放弃自动恢复。请稍后重试或检查 Codex 状态。")
		s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
		return events
	}
	if !goalInterlockFingerprintMatches(lease, event.ThreadGoal) {
		events := goalInterlockDiagnosticNotice(lease.TriggerSurfaceID, "goal_resume_failed", "Goal 已变化", "Goal 在队列执行期间被修改，已放弃自动恢复。")
		s.clearGoalInterlockLease(lease.InstanceID, lease.ThreadID)
		return events
	}
	commandID := s.nextAgentCommandID()
	lease.ResumeCommandID = commandID
	if s.goalInterlockByCommand == nil {
		s.goalInterlockByCommand = map[string]string{}
	}
	s.goalInterlockByCommand[commandID] = goalInterlockKey(lease.InstanceID, lease.ThreadID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: lease.TriggerSurfaceID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandThreadGoalSet,
			Target:    agentproto.Target{ThreadID: lease.ThreadID},
			Goal: agentproto.GoalCommand{
				Status:  "active",
				Purpose: "queue_interlock",
			},
		},
	}}
}

func goalInterlockFingerprintMatches(lease *GoalInterlockLease, goal *agentproto.ThreadGoalUpdate) bool {
	if goal == nil {
		return false
	}
	if lease.GoalCreatedAt != 0 && goal.CreatedAt != 0 && lease.GoalCreatedAt != goal.CreatedAt {
		return false
	}
	if lease.Objective != "" && lease.Objective != goal.Objective {
		return false
	}
	if lease.TokenBudget != nil {
		if goal.TokenBudget == nil || *lease.TokenBudget != *goal.TokenBudget {
			return false
		}
	}
	return true
}

func (s *Service) revokeGoalInterlockOnExternalMutation(instanceID, threadID string) {
	lease := s.goalInterlockLease(instanceID, threadID)
	if lease == nil {
		return
	}
	lease.ExternalMutationSeen = true
	if lease.Phase == GoalInterlockDraining {
		return
	}
	s.clearGoalInterlockLease(instanceID, threadID)
}

func goalInterlockDiagnosticNotice(surfaceID, code, title, text string) []eventcontract.Event {
	if surfaceID == "" {
		return nil
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: title,
			Text:  text,
		},
	}}
}
