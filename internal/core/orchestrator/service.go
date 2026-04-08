package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"time"
)

type Config struct {
	TurnHandoffWait    time.Duration
	HeadlessLaunchWait time.Duration
	LocalPauseMaxWait  time.Duration
	DetachAbandonWait  time.Duration
}

type Service struct {
	now             func() time.Time
	config          Config
	root            *state.Root
	renderer        *renderer.Planner
	nextQueueItemID int
	nextImageID     int
	nextPromptID    int
	nextHeadlessID  int
	handoffUntil    map[string]time.Time
	pausedUntil     map[string]time.Time
	abandoningUntil map[string]time.Time
	itemBuffers     map[string]*itemBuffer
	threadRefreshes map[string]bool
	pendingTurnText map[string]*completedTextItem
	turnFileChanges map[string]*turnFileChangeSummary
	pendingRemote   map[string]*remoteTurnBinding
	activeRemote    map[string]*remoteTurnBinding
	instanceClaims  map[string]*instanceClaimRecord
	threadClaims    map[string]*threadClaimRecord
}

type itemBuffer struct {
	InstanceID string
	ThreadID   string
	TurnID     string
	ItemID     string
	ItemKind   string
	Text       string
}

type remoteTurnBinding struct {
	InstanceID       string
	SurfaceSessionID string
	QueueItemID      string
	SourceMessageID  string
	CommandID        string
	ThreadID         string
	TurnID           string
	Status           string
}

type completedTextItem struct {
	InstanceID string
	ThreadID   string
	TurnID     string
	ItemID     string
	ItemKind   string
	Text       string
}

type turnFileChangeSummary struct {
	Files map[string]*turnFileChangeEntry
}

type turnFileChangeEntry struct {
	Path         string
	MovePath     string
	AddedLines   int
	RemovedLines int
}

type instanceClaimRecord struct {
	InstanceID       string
	SurfaceSessionID string
}

type threadClaimRecord struct {
	ThreadID         string
	InstanceID       string
	SurfaceSessionID string
}

const (
	requestCaptureModeDeclineWithFeedback = "decline_with_feedback"
	defaultModel                          = "gpt-5.4"
	defaultReasoningEffort                = "xhigh"
)

func NewService(now func() time.Time, cfg Config, planner *renderer.Planner) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TurnHandoffWait <= 0 {
		cfg.TurnHandoffWait = 800 * time.Millisecond
	}
	if cfg.HeadlessLaunchWait <= 0 {
		cfg.HeadlessLaunchWait = 45 * time.Second
	}
	if cfg.LocalPauseMaxWait <= 0 {
		cfg.LocalPauseMaxWait = 15 * time.Second
	}
	if cfg.DetachAbandonWait <= 0 {
		cfg.DetachAbandonWait = 20 * time.Second
	}
	if planner == nil {
		planner = renderer.NewPlanner()
	}
	return &Service{
		now:             now,
		config:          cfg,
		root:            state.NewRoot(),
		renderer:        planner,
		handoffUntil:    map[string]time.Time{},
		pausedUntil:     map[string]time.Time{},
		abandoningUntil: map[string]time.Time{},
		itemBuffers:     map[string]*itemBuffer{},
		threadRefreshes: map[string]bool{},
		pendingTurnText: map[string]*completedTextItem{},
		turnFileChanges: map[string]*turnFileChangeSummary{},
		pendingRemote:   map[string]*remoteTurnBinding{},
		activeRemote:    map[string]*remoteTurnBinding{},
		instanceClaims:  map[string]*instanceClaimRecord{},
		threadClaims:    map[string]*threadClaimRecord{},
	}
}

func (s *Service) UpsertInstance(inst *state.InstanceRecord) {
	if inst.Threads == nil {
		inst.Threads = map[string]*state.ThreadRecord{}
	}
	if inst.CWDDefaults == nil {
		inst.CWDDefaults = map[string]state.ModelConfigRecord{}
	}
	s.root.Instances[inst.InstanceID] = inst
}

func (s *Service) ApplySurfaceAction(action control.Action) []control.UIEvent {
	surface := s.ensureSurface(action)
	if surface.Abandoning {
		switch action.Kind {
		case control.ActionStatus:
			return []control.UIEvent{{Kind: control.UIEventSnapshot, SurfaceSessionID: surface.SurfaceSessionID, Snapshot: s.buildSnapshot(surface)}}
		case control.ActionDetach:
			return notice(surface, "detach_pending", "当前仍在等待已发出的 turn 收尾，请稍后再试。")
		default:
			return notice(surface, "detach_pending", "当前会话正在等待已发出的 turn 收尾，暂时不能执行新的操作。")
		}
	}
	if blocked := s.pendingHeadlessActionBlocked(surface, action); blocked != nil {
		return blocked
	}
	switch action.Kind {
	case control.ActionListInstances:
		return s.presentInstanceSelection(surface)
	case control.ActionNewThread:
		return s.prepareNewThread(surface)
	case control.ActionKillInstance:
		return s.killHeadlessInstance(surface)
	case control.ActionRemovedCommand:
		return s.handleRemovedCommand(surface, action)
	case control.ActionAttachInstance:
		return s.attachInstance(surface, action.InstanceID)
	case control.ActionShowCommandHelp:
		return []control.UIEvent{commandCatalogEvent(surface, control.FeishuCommandHelpCatalog())}
	case control.ActionShowCommandMenu:
		return []control.UIEvent{commandCatalogEvent(surface, control.FeishuCommandMenuCatalog())}
	case control.ActionModelCommand:
		return s.handleModelCommand(surface, action)
	case control.ActionReasoningCommand:
		return s.handleReasoningCommand(surface, action)
	case control.ActionAccessCommand:
		return s.handleAccessCommand(surface, action)
	case control.ActionRespondRequest:
		return s.respondRequest(surface, action)
	case control.ActionShowThreads:
		return s.presentThreadSelection(surface, false)
	case control.ActionShowAllThreads:
		return s.presentThreadSelection(surface, true)
	case control.ActionUseThread:
		return s.useThread(surface, action.ThreadID)
	case control.ActionConfirmKickThread:
		return s.confirmKickThread(surface, action.ThreadID)
	case control.ActionCancelKickThread:
		return notice(surface, "kick_cancelled", "已取消强踢。")
	case control.ActionFollowLocal:
		return s.followLocal(surface)
	case control.ActionTextMessage:
		return s.handleText(surface, action)
	case control.ActionImageMessage:
		return s.stageImage(surface, action)
	case control.ActionReactionCreated:
		return nil
	case control.ActionMessageRecalled:
		return s.handleMessageRecalled(surface, action.TargetMessageID)
	case control.ActionSelectPrompt:
		return notice(surface, "selection_expired", "这个旧卡片已失效，请重新发送 /list、/use 或 /useall。")
	case control.ActionStop:
		return s.stopSurface(surface)
	case control.ActionStatus:
		return []control.UIEvent{{Kind: control.UIEventSnapshot, SurfaceSessionID: surface.SurfaceSessionID, Snapshot: s.buildSnapshot(surface)}}
	case control.ActionDetach:
		return s.detach(surface)
	default:
		return nil
	}
}

func (s *Service) ApplyAgentEvent(instanceID string, event agentproto.Event) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	if isInternalHelperEvent(event) {
		return nil
	}
	preface := s.flushPendingTurnTextIfTurnContinues(instanceID, event)

	switch event.Kind {
	case agentproto.EventThreadFocused:
		s.maybePromoteWorkspaceRoot(inst, event.CWD)
		inst.ObservedFocusedThreadID = event.ThreadID
		thread := s.ensureThread(inst, event.ThreadID)
		thread.Loaded = true
		if event.CWD != "" {
			thread.CWD = event.CWD
		}
		s.touchThread(thread)
		return append(preface, s.threadFocusEvents(instanceID, event.ThreadID)...)
	case agentproto.EventConfigObserved:
		s.observeConfig(inst, event.ThreadID, event.CWD, event.ConfigScope, event.Model, event.ReasoningEffort)
		return preface
	case agentproto.EventThreadDiscovered:
		s.maybePromoteWorkspaceRoot(inst, event.CWD)
		thread := s.ensureThread(inst, event.ThreadID)
		if event.TrafficClass != "" {
			thread.TrafficClass = event.TrafficClass
		}
		if event.Name != "" {
			thread.Name = event.Name
		}
		if event.Preview != "" {
			thread.Preview = event.Preview
		}
		if event.CWD != "" {
			thread.CWD = event.CWD
		}
		if event.Model != "" {
			thread.ExplicitModel = event.Model
		}
		if event.ReasoningEffort != "" {
			thread.ExplicitReasoningEffort = event.ReasoningEffort
		}
		thread.Loaded = true
		s.touchThread(thread)
		return append(preface, s.threadFocusEvents(instanceID, event.ThreadID)...)
	case agentproto.EventThreadsSnapshot:
		delete(s.threadRefreshes, instanceID)
		nextThreads := map[string]*state.ThreadRecord{}
		for threadID, thread := range inst.Threads {
			if thread == nil {
				continue
			}
			copied := *thread
			copied.Loaded = false
			nextThreads[threadID] = &copied
		}
		for _, thread := range event.Threads {
			s.maybePromoteWorkspaceRoot(inst, thread.CWD)
			current := nextThreads[thread.ThreadID]
			if current == nil {
				current = &state.ThreadRecord{ThreadID: thread.ThreadID}
			}
			current.TrafficClass = agentproto.TrafficClassPrimary
			if thread.Name != "" {
				current.Name = thread.Name
			}
			if thread.Preview != "" {
				current.Preview = thread.Preview
			}
			if thread.CWD != "" {
				current.CWD = thread.CWD
			}
			if thread.Model != "" {
				current.ExplicitModel = thread.Model
			}
			if thread.ReasoningEffort != "" {
				current.ExplicitReasoningEffort = thread.ReasoningEffort
			}
			current.Loaded = thread.Loaded
			current.Archived = thread.Archived
			if thread.State != "" {
				current.State = thread.State
			}
			current.ListOrder = thread.ListOrder
			nextThreads[thread.ThreadID] = current
		}
		inst.Threads = nextThreads
		events := append(preface, s.reconcileInstanceSurfaceThreads(instanceID)...)
		return append(events, s.threadFocusEvents(instanceID, "")...)
	case agentproto.EventLocalInteractionObserved:
		if event.ThreadID != "" {
			inst.ObservedFocusedThreadID = event.ThreadID
			thread := s.ensureThread(inst, event.ThreadID)
			if event.CWD != "" {
				thread.CWD = event.CWD
			}
			s.touchThread(thread)
		}
		events := append(preface, s.pauseForLocal(instanceID)...)
		return append(events, s.reevaluateFollowSurfaces(instanceID)...)
	case agentproto.EventTurnStarted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		inst.ActiveTurnID = event.TurnID
		inst.ActiveThreadID = event.ThreadID
		if event.ThreadID != "" {
			s.touchThread(s.ensureThread(inst, event.ThreadID))
		}
		if surface := s.surfaceForInitiator(instanceID, event); surface != nil {
			surface.ActiveTurnOrigin = event.Initiator.Kind
		}
		if event.Initiator.Kind == agentproto.InitiatorLocalUI {
			if event.ThreadID != "" {
				inst.ObservedFocusedThreadID = event.ThreadID
				thread := s.ensureThread(inst, event.ThreadID)
				thread.Loaded = true
				if event.CWD != "" {
					thread.CWD = event.CWD
				}
				s.touchThread(thread)
			}
			events := append(preface, s.pauseForLocal(instanceID)...)
			return append(events, s.reevaluateFollowSurfaces(instanceID)...)
		}
		return append(preface, s.markRemoteTurnRunning(instanceID, event.Initiator, event.ThreadID, event.TurnID)...)
	case agentproto.EventTurnCompleted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		inst.ActiveTurnID = ""
		s.clearRequestsForTurn(instanceID, event.ThreadID, event.TurnID)
		if event.ThreadID != "" {
			inst.ActiveThreadID = event.ThreadID
			s.touchThread(s.ensureThread(inst, event.ThreadID))
		}
		surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
		if surface != nil {
			surface.ActiveTurnOrigin = ""
		}
		deleteMatchingItemBuffers(s.itemBuffers, instanceID, event.ThreadID, event.TurnID)
		summary := s.takeTurnFileChangeSummary(instanceID, event.ThreadID, event.TurnID)
		events := s.flushPendingTurnTextWithSummary(instanceID, event.ThreadID, event.TurnID, true, summary)
		if event.Initiator.Kind == agentproto.InitiatorLocalUI {
			events = append(events, s.enterHandoff(instanceID)...)
			if surface != nil {
				events = append(events, s.finishSurfaceAfterWork(surface)...)
			}
			return events
		}
		return append(events, s.completeRemoteTurn(instanceID, event.ThreadID, event.TurnID, event.Status, event.ErrorMessage, event.Problem)...)
	case agentproto.EventItemStarted:
		s.trackItemStart(instanceID, event)
		return preface
	case agentproto.EventItemDelta:
		s.trackItemDelta(instanceID, event)
		return preface
	case agentproto.EventItemCompleted:
		return append(preface, s.completeItem(instanceID, event)...)
	case agentproto.EventRequestStarted:
		return append(preface, s.presentRequestPrompt(instanceID, event)...)
	case agentproto.EventRequestResolved:
		return append(preface, s.resolveRequestPrompt(instanceID, event)...)
	case agentproto.EventSystemError:
		return append(preface, s.handleProblem(instanceID, problemFromEvent(event))...)
	default:
		return preface
	}
}

func (s *Service) Tick(now time.Time) []control.UIEvent {
	if now.IsZero() {
		now = s.now()
	}
	var events []control.UIEvent
	for surfaceID, until := range s.handoffUntil {
		if now.Before(until) {
			continue
		}
		delete(s.handoffUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || surface.DispatchMode != state.DispatchModeHandoffWait {
			continue
		}
		surface.DispatchMode = state.DispatchModeNormal
		if len(surface.QueuedQueueItemIDs) == 0 {
			continue
		}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "remote_queue_resumed",
				Text: "本地操作已结束，飞书队列继续处理。",
			},
		})
		events = append(events, s.dispatchNext(surface)...)
	}
	for surfaceID, until := range s.pausedUntil {
		if now.Before(until) {
			continue
		}
		delete(s.pausedUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || surface.DispatchMode != state.DispatchModePausedForLocal {
			continue
		}
		surface.DispatchMode = state.DispatchModeNormal
		if len(surface.QueuedQueueItemIDs) == 0 {
			continue
		}
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "local_activity_watchdog_resumed",
				Text: "本地活动恢复信号超时，飞书队列已自动恢复处理。",
			},
		})
		events = append(events, s.dispatchNext(surface)...)
	}
	for surfaceID, until := range s.abandoningUntil {
		if now.Before(until) {
			continue
		}
		delete(s.abandoningUntil, surfaceID)
		surface := s.root.Surfaces[surfaceID]
		if surface == nil || !surface.Abandoning {
			continue
		}
		events = append(events, s.finalizeDetachedSurface(surface)...)
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "detach_timeout_forced",
				Text: "等待当前 turn 收尾超时，已强制断开当前实例接管。",
			},
		})
	}
	for _, surface := range s.root.Surfaces {
		if pending := surface.PendingHeadless; pending != nil && !pending.ExpiresAt.IsZero() && !now.Before(pending.ExpiresAt) {
			events = append(events, s.expirePendingHeadless(surface, pending)...)
		}
		if !requestCaptureExpired(now, surface.ActiveRequestCapture) {
			continue
		}
		clearSurfaceRequestCapture(surface)
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "request_capture_expired",
				Text: "上一条确认反馈已过期，请重新点击卡片按钮后再发送处理意见。",
			},
		})
	}
	return events
}
