package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type Config struct {
	TurnHandoffWait    time.Duration
	HeadlessLaunchWait time.Duration
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
	itemBuffers     map[string]*itemBuffer
	threadRefreshes map[string]bool
	pendingTurnText map[string]*completedTextItem
	pendingRemote   map[string]*remoteTurnBinding
	activeRemote    map[string]*remoteTurnBinding
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
	if planner == nil {
		planner = renderer.NewPlanner()
	}
	return &Service{
		now:             now,
		config:          cfg,
		root:            state.NewRoot(),
		renderer:        planner,
		handoffUntil:    map[string]time.Time{},
		itemBuffers:     map[string]*itemBuffer{},
		threadRefreshes: map[string]bool{},
		pendingTurnText: map[string]*completedTextItem{},
		pendingRemote:   map[string]*remoteTurnBinding{},
		activeRemote:    map[string]*remoteTurnBinding{},
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
	switch action.Kind {
	case control.ActionListInstances:
		return s.presentInstanceSelection(surface)
	case control.ActionNewInstance:
		return s.startHeadlessInstance(surface)
	case control.ActionKillInstance:
		return s.killHeadlessInstance(surface)
	case control.ActionAttachInstance:
		return s.attachInstance(surface, action.InstanceID)
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
	case control.ActionFollowLocal:
		surface.SelectedThreadID = ""
		surface.RouteMode = state.RouteModeFollowLocal
		return s.threadSelectionEvents(surface, "", string(surface.RouteMode), "跟随当前 VS Code", "")
	case control.ActionTextMessage:
		return s.handleText(surface, action)
	case control.ActionImageMessage:
		return s.stageImage(surface, action)
	case control.ActionReactionCreated:
		return nil
	case control.ActionMessageRecalled:
		return s.handleMessageRecalled(surface, action.TargetMessageID)
	case control.ActionSelectPrompt:
		return s.resolveSelectionOption(surface, action.PromptID, action.OptionID)
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
		events := append(preface, s.threadFocusEvents(instanceID, "")...)
		return append(events, s.handlePendingHeadlessThreadSnapshot(instanceID)...)
	case agentproto.EventLocalInteractionObserved:
		if event.ThreadID != "" {
			inst.ObservedFocusedThreadID = event.ThreadID
			thread := s.ensureThread(inst, event.ThreadID)
			if event.CWD != "" {
				thread.CWD = event.CWD
			}
			s.touchThread(thread)
		}
		return append(preface, s.pauseForLocal(instanceID)...)
	case agentproto.EventTurnStarted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		inst.ActiveTurnID = event.TurnID
		inst.ActiveThreadID = event.ThreadID
		if event.ThreadID != "" {
			s.touchThread(s.ensureThread(inst, event.ThreadID))
		}
		if surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID); surface != nil {
			surface.ActiveTurnOrigin = event.Initiator.Kind
		}
		if event.Initiator.Kind == agentproto.InitiatorLocalUI {
			if event.ThreadID != "" {
				inst.ObservedFocusedThreadID = event.ThreadID
				thread := s.ensureThread(inst, event.ThreadID)
				events := []control.UIEvent{}
				_ = thread
				for _, surface := range s.findAttachedSurfaces(instanceID) {
					events = append(events, s.bindSurfaceToThread(surface, inst, event.ThreadID)...)
				}
				return append(append(preface, s.pauseForLocal(instanceID)...), events...)
			}
			return append(preface, s.pauseForLocal(instanceID)...)
		}
		return append(preface, s.markRemoteTurnRunning(instanceID, event.ThreadID, event.TurnID)...)
	case agentproto.EventTurnCompleted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		inst.ActiveTurnID = ""
		s.clearRequestsForTurn(instanceID, event.ThreadID, event.TurnID)
		if event.ThreadID != "" {
			inst.ActiveThreadID = event.ThreadID
			s.touchThread(s.ensureThread(inst, event.ThreadID))
		}
		if surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID); surface != nil {
			surface.ActiveTurnOrigin = ""
		}
		deleteMatchingItemBuffers(s.itemBuffers, instanceID, event.ThreadID, event.TurnID)
		events := s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, true)
		if event.Initiator.Kind == agentproto.InitiatorLocalUI {
			return append(events, s.enterHandoff(instanceID)...)
		}
		return append(events, s.completeRemoteTurn(instanceID, event.ThreadID, event.TurnID, event.Status, event.ErrorMessage)...)
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
	for _, surface := range s.root.Surfaces {
		if pending := surface.PendingHeadless; pending != nil && !pending.ExpiresAt.IsZero() && !now.Before(pending.ExpiresAt) {
			surface.PendingHeadless = nil
			events = append(events, control.UIEvent{
				Kind:             control.UIEventDaemonCommand,
				SurfaceSessionID: surface.SurfaceSessionID,
				DaemonCommand: &control.DaemonCommand{
					Kind:             control.DaemonCommandKillHeadless,
					SurfaceSessionID: surface.SurfaceSessionID,
					InstanceID:       pending.InstanceID,
					ThreadID:         pending.ThreadID,
					ThreadTitle:      pending.ThreadTitle,
					ThreadCWD:        pending.ThreadCWD,
				},
			})
			events = append(events, control.UIEvent{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code:  "headless_start_timeout",
					Title: "Headless 实例超时",
					Text:  "headless 实例启动超时，已自动取消，请重新发送 /newinstance。",
				},
			})
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

func (s *Service) ensureSurface(action control.Action) *state.SurfaceConsoleRecord {
	surface := s.root.Surfaces[action.SurfaceSessionID]
	if surface != nil {
		if action.ChatID != "" {
			surface.ChatID = action.ChatID
		}
		if action.ActorUserID != "" {
			surface.ActorUserID = action.ActorUserID
		}
		if surface.PendingRequests == nil {
			surface.PendingRequests = map[string]*state.RequestPromptRecord{}
		}
		return surface
	}

	surface = &state.SurfaceConsoleRecord{
		SurfaceSessionID: action.SurfaceSessionID,
		Platform:         "feishu",
		ChatID:           action.ChatID,
		ActorUserID:      action.ActorUserID,
		RouteMode:        state.RouteModeUnbound,
		DispatchMode:     state.DispatchModeNormal,
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
	}
	s.root.Surfaces[action.SurfaceSessionID] = surface
	return surface
}

func (s *Service) ensureThread(inst *state.InstanceRecord, threadID string) *state.ThreadRecord {
	if inst.Threads == nil {
		inst.Threads = map[string]*state.ThreadRecord{}
	}
	thread := inst.Threads[threadID]
	if thread != nil {
		return thread
	}
	thread = &state.ThreadRecord{ThreadID: threadID}
	inst.Threads[threadID] = thread
	return thread
}

func (s *Service) presentInstanceSelection(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	instances := make([]*state.InstanceRecord, 0, len(s.root.Instances))
	for _, inst := range s.root.Instances {
		if inst.Online {
			instances = append(instances, inst)
		}
	}
	if len(instances) == 0 {
		surface.SelectionPrompt = nil
		return notice(surface, "no_online_instances", "当前没有在线实例。请先在 VS Code 中打开 Codex 会话。")
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].WorkspaceKey == instances[j].WorkspaceKey {
			return instances[i].InstanceID < instances[j].InstanceID
		}
		return instances[i].WorkspaceKey < instances[j].WorkspaceKey
	})

	options := make([]control.SelectionOption, 0, len(instances))
	recordOptions := make([]state.SelectionOptionRecord, 0, len(instances))
	for i, inst := range instances {
		label := inst.ShortName
		if label == "" {
			label = filepath.Base(inst.WorkspaceKey)
		}
		if label == "" {
			label = inst.InstanceID
		}
		subtitle := inst.WorkspaceKey
		options = append(options, control.SelectionOption{
			Index:     i + 1,
			OptionID:  inst.InstanceID,
			Label:     label,
			Subtitle:  subtitle,
			IsCurrent: surface.AttachedInstanceID == inst.InstanceID,
		})
		recordOptions = append(recordOptions, state.SelectionOptionRecord{
			Index:    i + 1,
			OptionID: inst.InstanceID,
			Label:    label,
			Subtitle: subtitle,
			Current:  surface.AttachedInstanceID == inst.InstanceID,
		})
	}
	s.nextPromptID++
	createdAt := s.now()
	expiresAt := createdAt.Add(10 * time.Minute)
	prompt := &state.SelectionPromptRecord{
		PromptID:  fmt.Sprintf("prompt-%d", s.nextPromptID),
		Kind:      "attach_instance",
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Title:     "在线实例",
		Options:   recordOptions,
	}
	surface.SelectionPrompt = prompt
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			PromptID:  prompt.PromptID,
			Kind:      control.SelectionPromptAttachInstance,
			CreatedAt: createdAt,
			ExpiresAt: expiresAt,
			Title:     prompt.Title,
			Options:   options,
		},
	}}
}

func (s *Service) startHeadlessInstance(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.AttachedInstanceID != "" {
		return notice(surface, "headless_requires_detach", "当前会话已接管实例，请先 /detach 再创建 headless 实例。")
	}
	if surface.PendingHeadless != nil {
		return notice(surface, "headless_already_starting", "当前会话已有 headless 实例创建中，请等待完成或执行 /killinstance 取消。")
	}
	surface.SelectionPrompt = nil
	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  instanceID,
		RequestedAt: s.now(),
		ExpiresAt:   s.now().Add(s.config.HeadlessLaunchWait),
		Status:      state.HeadlessLaunchStarting,
	}
	return []control.UIEvent{
		{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_starting",
				Title: "创建 Headless 实例",
				Text:  "正在创建 headless 实例，稍后会自动加载可恢复会话。",
			},
		},
		{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandStartHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
			},
		},
	}
}

func (s *Service) presentHeadlessResumeSelection(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []control.UIEvent {
	if surface == nil || inst == nil {
		return nil
	}
	threads := visibleThreads(inst)
	if len(threads) == 0 {
		surface.SelectionPrompt = nil
		return nil
	}
	limit := len(threads)
	hint := ""
	if limit > 5 {
		limit = 5
		hint = "只显示最近 5 个已知会话。"
	}
	threads = threads[:limit]
	options := make([]control.SelectionOption, 0, len(threads))
	recordOptions := make([]state.SelectionOptionRecord, 0, len(threads))
	for i, thread := range threads {
		label := displayThreadTitle(inst, thread, thread.ThreadID)
		subtitle := threadSelectionSubtitle(thread, thread.ThreadID)
		optionID := fmt.Sprintf("headless-%d", i+1)
		options = append(options, control.SelectionOption{
			Index:    i + 1,
			OptionID: optionID,
			Label:    label,
			Subtitle: subtitle,
		})
		recordOptions = append(recordOptions, state.SelectionOptionRecord{
			Index:            i + 1,
			OptionID:         optionID,
			Label:            label,
			Subtitle:         subtitle,
			TargetThreadID:   thread.ThreadID,
			TargetThreadCWD:  thread.CWD,
			TargetThreadName: thread.Name,
			TargetPreview:    thread.Preview,
		})
	}
	s.nextPromptID++
	createdAt := s.now()
	expiresAt := createdAt.Add(10 * time.Minute)
	surface.SelectionPrompt = &state.SelectionPromptRecord{
		PromptID:  fmt.Sprintf("prompt-%d", s.nextPromptID),
		Kind:      "new_instance_thread",
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Title:     "选择要恢复的会话",
		Hint:      hint,
		Options:   recordOptions,
	}
	return []control.UIEvent{
		{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_ready_select_thread",
				Title: "Headless 实例已就绪",
				Text:  "请选择一个要恢复的会话；选定后下一条消息会在该会话继续。",
			},
		},
		{
			Kind:             control.UIEventSelectionPrompt,
			SurfaceSessionID: surface.SurfaceSessionID,
			SelectionPrompt: &control.SelectionPrompt{
				PromptID:  surface.SelectionPrompt.PromptID,
				Kind:      control.SelectionPromptNewInstance,
				CreatedAt: createdAt,
				ExpiresAt: expiresAt,
				Title:     surface.SelectionPrompt.Title,
				Hint:      hint,
				Options:   options,
			},
		},
	}
}

func (s *Service) attachInstance(surface *state.SurfaceConsoleRecord, instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return notice(surface, "instance_not_found", "实例不存在。")
	}

	events := s.discardDrafts(surface)
	clearSurfaceRequestCapture(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.AttachedInstanceID = instanceID
	surface.SelectionPrompt = nil
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal

	initialThreadID := inst.ObservedFocusedThreadID
	if initialThreadID == "" {
		initialThreadID = inst.ActiveThreadID
	}
	if !threadVisible(inst.Threads[initialThreadID]) {
		initialThreadID = ""
	}
	if initialThreadID != "" {
		surface.SelectedThreadID = initialThreadID
		surface.RouteMode = state.RouteModePinned
	} else {
		surface.SelectedThreadID = ""
		surface.RouteMode = state.RouteModeUnbound
	}
	lastTitle := ""
	lastPreview := ""
	if surface.SelectedThreadID != "" {
		lastTitle = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
		lastPreview = threadPreview(inst.Threads[surface.SelectedThreadID])
	}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  surface.SelectedThreadID,
		RouteMode: string(surface.RouteMode),
		Title:     lastTitle,
		Preview:   lastPreview,
	}

	title := "未绑定会话"
	if surface.SelectedThreadID != "" {
		title = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "attached",
			Text: fmt.Sprintf("已接管 %s。当前输入目标：%s", inst.DisplayName, title),
		},
	})
	events = append(events, s.maybeRequestThreadRefresh(surface, inst, surface.SelectedThreadID)...)
	return events
}

func (s *Service) attachHeadlessInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || inst == nil || pending == nil {
		return nil
	}
	events := s.discardDrafts(surface)
	clearSurfaceRequestCapture(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.AttachedInstanceID = inst.InstanceID
	surface.SelectionPrompt = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(surface.RouteMode),
		Title:     "",
		Preview:   "",
	}
	pending.Status = state.HeadlessLaunchSelecting
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "headless_attached",
			Title: "Headless 实例已就绪",
			Text:  "已创建并接管 headless 实例，正在加载可恢复会话列表。",
		},
	})
	events = append(events, control.UIEvent{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandThreadsRefresh,
			Origin: agentproto.Origin{
				Surface: surface.SurfaceSessionID,
				UserID:  surface.ActorUserID,
				ChatID:  surface.ChatID,
			},
		},
	})
	return events
}

func (s *Service) handlePendingHeadlessThreadSnapshot(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		pending := surface.PendingHeadless
		if pending == nil || pending.InstanceID != instanceID || pending.Status != state.HeadlessLaunchSelecting {
			continue
		}
		if len(visibleThreads(inst)) == 0 {
			events = append(events, s.failPendingHeadlessSelection(surface, pending)...)
			continue
		}
		events = append(events, s.presentHeadlessResumeSelection(surface, inst)...)
	}
	return events
}

func (s *Service) failPendingHeadlessSelection(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || pending == nil {
		return nil
	}
	events := s.discardDrafts(surface)
	s.clearRemoteOwnership(surface)
	surface.AttachedInstanceID = ""
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.DispatchMode = state.DispatchModeNormal
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.SelectionPrompt = nil
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	clearSurfaceRequests(surface)
	surface.LastSelection = nil
	events = append(events,
		control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       pending.InstanceID,
			},
		},
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "no_recoverable_threads",
				Title: "没有可恢复会话",
				Text:  "headless 实例已启动，但没有发现可恢复的会话，已自动结束该实例。",
			},
		},
	)
	return events
}

func (s *Service) completeHeadlessThreadSelection(surface *state.SurfaceConsoleRecord, option state.SelectionOptionRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	pending := surface.PendingHeadless
	inst := s.root.Instances[surface.AttachedInstanceID]
	if pending == nil || inst == nil || pending.InstanceID != inst.InstanceID || !isHeadlessInstance(inst) {
		return notice(surface, "selection_expired", "之前的 headless 会话选择已过期，请重新发送 /newinstance。")
	}
	if strings.TrimSpace(option.TargetThreadID) == "" || strings.TrimSpace(option.TargetThreadCWD) == "" {
		return notice(surface, "headless_selection_invalid", "这个会话缺少可恢复的工作目录，无法恢复。")
	}
	thread := s.ensureThread(inst, option.TargetThreadID)
	if strings.TrimSpace(thread.CWD) == "" {
		thread.CWD = option.TargetThreadCWD
	}
	if strings.TrimSpace(thread.Name) == "" {
		thread.Name = option.TargetThreadName
	}
	if strings.TrimSpace(thread.Preview) == "" {
		thread.Preview = option.TargetPreview
	}
	thread.Loaded = true
	s.touchThread(thread)
	s.retargetManagedHeadlessInstance(inst, thread.CWD)
	surface.PendingHeadless = nil
	return s.useThread(surface, option.TargetThreadID)
}

func (s *Service) presentThreadSelection(surface *state.SurfaceConsoleRecord, showAll bool) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	threads := visibleThreads(inst)
	if len(threads) == 0 {
		surface.SelectionPrompt = nil
		return notice(surface, "no_visible_threads", "当前还没有可用会话。")
	}
	sortVisibleThreads(threads)
	limit := len(threads)
	title := "全部会话"
	hint := ""
	if !showAll {
		title = "最近会话"
		if limit > 5 {
			limit = 5
			hint = "发送 `/useall` 查看全部会话。"
		}
	}
	threads = threads[:limit]
	options := make([]control.SelectionOption, 0, len(threads))
	recordOptions := make([]state.SelectionOptionRecord, 0, len(threads))
	for i, thread := range threads {
		label := displayThreadTitle(inst, thread, thread.ThreadID)
		subtitle := threadSelectionSubtitle(thread, thread.ThreadID)
		options = append(options, control.SelectionOption{
			Index:     i + 1,
			OptionID:  thread.ThreadID,
			Label:     label,
			Subtitle:  subtitle,
			IsCurrent: surface.SelectedThreadID == thread.ThreadID,
		})
		recordOptions = append(recordOptions, state.SelectionOptionRecord{
			Index:    i + 1,
			OptionID: thread.ThreadID,
			Label:    label,
			Subtitle: subtitle,
			Current:  surface.SelectedThreadID == thread.ThreadID,
		})
	}
	s.nextPromptID++
	createdAt := s.now()
	expiresAt := createdAt.Add(10 * time.Minute)
	surface.SelectionPrompt = &state.SelectionPromptRecord{
		PromptID:  fmt.Sprintf("prompt-%d", s.nextPromptID),
		Kind:      "use_thread",
		CreatedAt: createdAt,
		ExpiresAt: expiresAt,
		Title:     title,
		Hint:      hint,
		Options:   recordOptions,
	}
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			PromptID:  surface.SelectionPrompt.PromptID,
			Kind:      control.SelectionPromptUseThread,
			CreatedAt: createdAt,
			ExpiresAt: expiresAt,
			Title:     title,
			Hint:      hint,
			Options:   options,
		},
	}}
}

func (s *Service) useThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	surface.SelectedThreadID = threadID
	surface.RouteMode = state.RouteModePinned
	surface.SelectionPrompt = nil
	inst := s.root.Instances[surface.AttachedInstanceID]
	title := threadID
	preview := ""
	if inst != nil {
		thread := s.ensureThread(inst, threadID)
		s.touchThread(thread)
		title = displayThreadTitle(inst, thread, threadID)
		preview = threadPreview(thread)
	}
	events := s.threadSelectionEvents(surface, threadID, string(surface.RouteMode), title, preview)
	if len(events) != 0 {
		return events
	}
	return notice(surface, "selection_unchanged", fmt.Sprintf("当前输入目标保持为：%s", title))
}

func (s *Service) handleModelCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{{
			Kind:             control.UIEventSnapshot,
			SurfaceSessionID: surface.SurfaceSessionID,
			Snapshot:         s.buildSnapshot(surface),
		}}
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.Model = ""
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		return notice(surface, "surface_override_cleared", "已清除飞书临时模型覆盖。之后从飞书发送的消息将恢复使用底层真实配置。")
	}
	if len(parts) > 3 {
		return notice(surface, "surface_override_usage", "用法：`/model` 查看当前配置；`/model <模型>`；`/model <模型> <推理强度>`；`/model clear`。")
	}
	override := surface.PromptOverride
	override.Model = parts[1]
	if len(parts) == 3 {
		if !looksLikeReasoningEffort(parts[2]) {
			return notice(surface, "surface_override_usage", "推理强度建议使用 `low`、`medium`、`high` 或 `xhigh`。")
		}
		override.ReasoningEffort = strings.ToLower(parts[2])
	}
	surface.PromptOverride = override
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时模型覆盖。"))
}

func (s *Service) handleReasoningCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{{
			Kind:             control.UIEventSnapshot,
			SurfaceSessionID: surface.SurfaceSessionID,
			Snapshot:         s.buildSnapshot(surface),
		}}
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		return notice(surface, "surface_override_reasoning_cleared", "已清除飞书临时推理强度覆盖。")
	}
	if len(parts) != 2 || !looksLikeReasoningEffort(parts[1]) {
		return notice(surface, "surface_override_usage", "用法：`/reasoning` 查看当前配置；`/reasoning <推理强度>`；`/reasoning clear`。")
	}
	surface.PromptOverride.ReasoningEffort = strings.ToLower(parts[1])
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时推理强度覆盖。"))
}

func (s *Service) handleAccessCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{{
			Kind:             control.UIEventSnapshot,
			SurfaceSessionID: surface.SurfaceSessionID,
			Snapshot:         s.buildSnapshot(surface),
		}}
	}
	if len(parts) != 2 {
		return notice(surface, "surface_access_usage", "用法：`/access` 查看当前配置；`/access full`；`/access confirm`；`/access clear`。")
	}
	if isClearCommand(parts[1]) {
		surface.PromptOverride.AccessMode = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
		return notice(surface, "surface_access_reset", formatOverrideNotice(summary, "已恢复飞书默认执行权限。"))
	}
	mode := agentproto.NormalizeAccessMode(parts[1])
	if mode == "" {
		return notice(surface, "surface_access_usage", "执行权限建议使用 `full` 或 `confirm`。")
	}
	surface.PromptOverride.AccessMode = mode
	surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_access_updated", formatOverrideNotice(summary, "已更新飞书执行权限模式。"))
}

func (s *Service) handleText(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	text := strings.TrimSpace(action.Text)
	if text == "" {
		return nil
	}

	if selectionPromptExpired(s.now(), surface.SelectionPrompt) {
		expiredKind := surface.SelectionPrompt.Kind
		surface.SelectionPrompt = nil
		if isDigits(text) {
			if expiredKind == "new_instance_thread" {
				return notice(surface, "selection_expired", "之前的 headless 会话选择已过期，请重新发送 /use 查看可恢复会话，或执行 /killinstance 取消。")
			}
			return notice(surface, "selection_expired", "之前的序号选择已过期，请重新发送 /list、/use 或 /useall。")
		}
	}

	if surface.SelectionPrompt != nil && isDigits(text) {
		return s.resolveSelection(surface, text)
	}

	if surface.PendingHeadless != nil {
		return notice(surface, headlessPendingNoticeCode(surface.PendingHeadless), headlessPendingNoticeText(surface.PendingHeadless))
	}
	if surface.SelectionPrompt != nil && surface.SelectionPrompt.Kind == "new_instance_thread" {
		return notice(surface, "headless_selection_waiting", "请先选择一个要恢复的会话，或执行 /killinstance 取消。")
	}

	if surface.ActiveRequestCapture != nil {
		return s.consumeCapturedRequestFeedback(surface, action, text)
	}
	if pending := activePendingRequest(surface); pending != nil {
		return notice(surface, "request_pending", "当前有待确认请求。请先点击卡片上的“允许一次”、“拒绝”或“告诉 Codex 怎么改”。")
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}

	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	inputs, stagedMessageIDs := s.consumeStagedInputs(surface)
	inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: text})
	if createThread {
		_ = createThread
	}
	return s.enqueueQueueItem(surface, action.MessageID, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)
}

func (s *Service) stageImage(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface.PendingHeadless != nil {
		return notice(surface, headlessPendingNoticeCode(surface.PendingHeadless), headlessPendingNoticeText(surface.PendingHeadless))
	}
	if surface.SelectionPrompt != nil && surface.SelectionPrompt.Kind == "new_instance_thread" {
		return notice(surface, "headless_selection_waiting", "请先选择一个要恢复的会话，再发送图片。")
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", "当前有待确认请求。请先处理确认卡片，再发送图片。")
	}
	s.nextImageID++
	image := &state.StagedImageRecord{
		ImageID:          fmt.Sprintf("img-%d", s.nextImageID),
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  action.MessageID,
		LocalPath:        action.LocalPath,
		MIMEType:         action.MIMEType,
		State:            state.ImageStaged,
	}
	surface.StagedImages[image.ImageID] = image
	return []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     image.ImageID,
			SourceMessageID: image.SourceMessageID,
			Status:          string(image.State),
			QueueOn:         true,
		},
	}}
}

func (s *Service) handleMessageRecalled(surface *state.SurfaceConsoleRecord, targetMessageID string) []control.UIEvent {
	targetMessageID = strings.TrimSpace(targetMessageID)
	if surface == nil || targetMessageID == "" {
		return nil
	}
	if activeID := surface.ActiveQueueItemID; activeID != "" {
		if item := surface.QueueItems[activeID]; item != nil && queueItemHasSourceMessage(item, targetMessageID) {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return []control.UIEvent{{
					Kind:             control.UIEventNotice,
					SurfaceSessionID: surface.SurfaceSessionID,
					Notice: &control.Notice{
						Code:     "message_recall_too_late",
						Title:    "无法撤回排队",
						Text:     "这条输入已经开始执行，不能通过撤回取消。若要中断当前 turn，请发送 `/stop`。",
						ThemeKey: "system",
					},
				}}
			}
		}
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued || !queueItemHasSourceMessage(item, targetMessageID) {
			continue
		}
		item.Status = state.QueueItemDiscarded
		s.markImagesForMessages(surface, queueItemSourceMessageIDs(item), state.ImageDiscarded)
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		return s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsDown:  true,
		}, queueItemSourceMessageIDs(item))
	}
	for _, image := range surface.StagedImages {
		if image.SourceMessageID == targetMessageID && image.State == state.ImageStaged {
			image.State = state.ImageCancelled
			return s.pendingInputEvents(surface, control.PendingInputState{
				QueueItemID: image.ImageID,
				Status:      string(image.State),
				QueueOff:    true,
				ThumbsDown:  true,
			}, []string{image.SourceMessageID})
		}
	}
	return nil
}

func (s *Service) stopSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	discarded := countPendingDrafts(surface)
	inst := s.root.Instances[surface.AttachedInstanceID]
	notice := control.Notice{
		Code:     "stop_no_active_turn",
		Title:    "没有正在运行的推理",
		Text:     "当前没有正在运行的推理。",
		ThemeKey: "system",
	}
	if inst != nil && inst.ActiveTurnID != "" {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandTurnInterrupt,
				Origin: agentproto.Origin{
					Surface: surface.SurfaceSessionID,
					UserID:  surface.ActorUserID,
					ChatID:  surface.ChatID,
				},
				Target: agentproto.Target{
					ThreadID: inst.ActiveThreadID,
					TurnID:   inst.ActiveTurnID,
				},
			},
		})
		notice = control.Notice{
			Code:     "stop_requested",
			Title:    "已发送停止请求",
			Text:     "已向当前运行中的 turn 发送停止请求。",
			ThemeKey: "system",
		}
	} else if surface.ActiveQueueItemID != "" {
		notice = control.Notice{
			Code:     "stop_not_interruptible",
			Title:    "当前还不能停止",
			Text:     "当前请求正在派发，尚未进入可中断状态。",
			ThemeKey: "system",
		}
	}

	events = append(events, s.discardDrafts(surface)...)
	surface.SelectionPrompt = nil
	clearSurfaceRequests(surface)
	if discarded > 0 {
		notice.Text += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	})
	return events
}

func (s *Service) killHeadlessInstance(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.PendingHeadless != nil {
		pending := surface.PendingHeadless
		events := s.discardDrafts(surface)
		s.clearRemoteOwnership(surface)
		if surface.AttachedInstanceID == pending.InstanceID {
			surface.AttachedInstanceID = ""
			surface.SelectedThreadID = ""
			surface.RouteMode = state.RouteModeUnbound
			surface.DispatchMode = state.DispatchModeNormal
			surface.PromptOverride = state.ModelConfigRecord{}
			surface.ActiveQueueItemID = ""
			clearSurfaceRequests(surface)
			surface.LastSelection = nil
		}
		surface.PendingHeadless = nil
		surface.SelectionPrompt = nil
		return append(events,
			control.UIEvent{
				Kind:             control.UIEventDaemonCommand,
				SurfaceSessionID: surface.SurfaceSessionID,
				DaemonCommand: &control.DaemonCommand{
					Kind:             control.DaemonCommandKillHeadless,
					SurfaceSessionID: surface.SurfaceSessionID,
					InstanceID:       pending.InstanceID,
					ThreadID:         pending.ThreadID,
					ThreadTitle:      pending.ThreadTitle,
					ThreadCWD:        pending.ThreadCWD,
				},
			},
			control.UIEvent{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code:  "headless_cancelled",
					Title: "取消 Headless 实例",
					Text:  "已取消当前 headless 实例创建流程。",
				},
			},
		)
	}
	if surface.SelectionPrompt != nil && surface.SelectionPrompt.Kind == "new_instance_thread" {
		surface.SelectionPrompt = nil
		return notice(surface, "headless_cancelled", "已取消 headless 实例创建。")
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "headless_not_found", "当前没有可结束的 headless 实例。")
	}
	if !isHeadlessInstance(inst) {
		return notice(surface, "headless_kill_forbidden", "当前接管的是 VS Code 实例，不能使用 /killinstance。")
	}
	instanceID := inst.InstanceID
	threadID := surface.SelectedThreadID
	threadTitle := displayThreadTitle(inst, inst.Threads[threadID], threadID)
	threadCWD := ""
	if thread := inst.Threads[threadID]; thread != nil {
		threadCWD = thread.CWD
	}
	events := s.discardDrafts(surface)
	s.clearRemoteOwnership(surface)
	surface.AttachedInstanceID = ""
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.DispatchMode = state.DispatchModeNormal
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.SelectionPrompt = nil
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	clearSurfaceRequests(surface)
	surface.LastSelection = nil
	events = append(events,
		control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
				ThreadID:         threadID,
				ThreadTitle:      threadTitle,
				ThreadCWD:        threadCWD,
			},
		},
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_kill_requested",
				Title: "结束 Headless 实例",
				Text:  "已请求结束当前 headless 实例，并断开当前接管。",
			},
		},
	)
	return events
}

func (s *Service) detach(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.PendingHeadless != nil {
		return notice(surface, "headless_pending", "当前 headless 创建流程尚未完成；如需取消，请执行 /killinstance。")
	}
	if surface.AttachedInstanceID == "" {
		if surface.SelectionPrompt != nil && surface.SelectionPrompt.Kind == "new_instance_thread" {
			return notice(surface, "headless_pending", "当前没有已接管实例；如需取消 headless 创建，请执行 /killinstance。")
		}
		return notice(surface, "detached", "当前没有接管中的实例。")
	}
	events := s.discardDrafts(surface)
	s.clearRemoteOwnership(surface)
	surface.AttachedInstanceID = ""
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.DispatchMode = state.DispatchModeNormal
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.SelectionPrompt = nil
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	clearSurfaceRequests(surface)
	surface.LastSelection = nil
	return append(events, notice(surface, "detached", "已断开当前实例接管。")...)
}

func (s *Service) resolveSelection(surface *state.SurfaceConsoleRecord, text string) []control.UIEvent {
	index, _ := strconv.Atoi(text)
	if surface.SelectionPrompt == nil {
		return nil
	}
	if selectionPromptExpired(s.now(), surface.SelectionPrompt) {
		kind := surface.SelectionPrompt.Kind
		surface.SelectionPrompt = nil
		if kind == "new_instance_thread" {
			return notice(surface, "selection_expired", "之前的 headless 会话选择已过期，请重新发送 /use 查看可恢复会话，或执行 /killinstance 取消。")
		}
		return notice(surface, "selection_expired", "之前的序号选择已过期，请重新发送 /list、/use 或 /useall。")
	}
	for _, option := range surface.SelectionPrompt.Options {
		if option.Index != index || option.Disabled {
			continue
		}
		switch surface.SelectionPrompt.Kind {
		case "attach_instance":
			return s.attachInstance(surface, option.OptionID)
		case "use_thread":
			return s.useThread(surface, option.OptionID)
		case "new_instance_thread":
			return s.completeHeadlessThreadSelection(surface, option)
		}
	}
	return notice(surface, "selection_invalid", "无效的序号。")
}

func (s *Service) resolveSelectionOption(surface *state.SurfaceConsoleRecord, promptID, optionID string) []control.UIEvent {
	if surface.SelectionPrompt == nil || promptID == "" || optionID == "" || surface.SelectionPrompt.PromptID != promptID {
		return notice(surface, "selection_expired", selectionExpiredText(surface.SelectionPrompt))
	}
	if selectionPromptExpired(s.now(), surface.SelectionPrompt) {
		text := selectionExpiredText(surface.SelectionPrompt)
		surface.SelectionPrompt = nil
		return notice(surface, "selection_expired", text)
	}
	for _, option := range surface.SelectionPrompt.Options {
		if option.OptionID != optionID || option.Disabled {
			continue
		}
		switch surface.SelectionPrompt.Kind {
		case "attach_instance":
			return s.attachInstance(surface, option.OptionID)
		case "use_thread":
			return s.useThread(surface, option.OptionID)
		case "new_instance_thread":
			return s.completeHeadlessThreadSelection(surface, option)
		}
	}
	return notice(surface, "selection_invalid", "这个按钮对应的选项无效。")
}

func (s *Service) respondRequest(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || action.RequestID == "" {
		return nil
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	request := surface.PendingRequests[action.RequestID]
	if request == nil {
		return notice(surface, "request_expired", "这个确认请求已经结束或过期了。")
	}
	requestType := normalizeRequestType(firstNonEmpty(action.RequestType, request.RequestType))
	if requestType == "" {
		requestType = "approval"
	}
	if requestType != "approval" {
		return notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
	optionID := normalizeRequestOptionID(firstNonEmpty(action.RequestOptionID, requestOptionIDFromApproved(action.Approved)))
	if optionID == "" {
		return notice(surface, "request_invalid", "这个确认按钮缺少有效的处理选项。")
	}
	if !requestHasOption(request, optionID) {
		return notice(surface, "request_invalid", "这个确认按钮对应的选项无效或当前不可用。")
	}
	if optionID == "captureFeedback" {
		surface.ActiveRequestCapture = &state.RequestCaptureRecord{
			RequestID:   request.RequestID,
			RequestType: request.RequestType,
			InstanceID:  request.InstanceID,
			ThreadID:    request.ThreadID,
			TurnID:      request.TurnID,
			Mode:        requestCaptureModeDeclineWithFeedback,
			CreatedAt:   s.now(),
			ExpiresAt:   s.now().Add(10 * time.Minute),
		}
		return notice(surface, "request_capture_started", "已进入反馈模式。接下来一条普通文本会作为对当前确认请求的处理意见，不会进入普通消息队列。")
	}
	decision := decisionForRequestOption(optionID)
	if decision == "" {
		return notice(surface, "request_invalid", "这个确认按钮对应的决策暂不支持。")
	}
	clearSurfaceRequestCaptureByRequestID(surface, request.RequestID)
	return []control.UIEvent{{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandRequestRespond,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    surface.ActorUserID,
				ChatID:    surface.ChatID,
				MessageID: action.MessageID,
			},
			Target: agentproto.Target{
				ThreadID:               request.ThreadID,
				TurnID:                 request.TurnID,
				UseActiveTurnIfOmitted: request.TurnID == "",
			},
			Request: agentproto.Request{
				RequestID: request.RequestID,
				Response: map[string]any{
					"type":     requestType,
					"decision": decision,
				},
			},
		},
	}}
}

func (s *Service) presentRequestPrompt(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.RequestID == "" {
		return nil
	}
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	if surface.PendingRequests == nil {
		surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	}
	requestType := normalizeRequestType(metadataString(event.Metadata, "requestType"))
	if requestType == "" {
		requestType = "approval"
	}
	if requestType != "approval" {
		return notice(surface, "request_unsupported", fmt.Sprintf("飞书端暂不支持处理 %s 类型的请求。", requestType))
	}
	inst := s.root.Instances[instanceID]
	var thread *state.ThreadRecord
	if inst != nil {
		thread = inst.Threads[event.ThreadID]
	}
	threadTitle := displayThreadTitle(inst, thread, event.ThreadID)
	title := firstNonEmpty(metadataString(event.Metadata, "title"), "需要确认")
	body := strings.TrimSpace(metadataString(event.Metadata, "body"))
	if body == "" {
		body = "本地 Codex 正在等待你的确认。"
	}
	options := buildApprovalRequestOptions(event.Metadata)
	record := &state.RequestPromptRecord{
		RequestID:   event.RequestID,
		RequestType: requestType,
		InstanceID:  instanceID,
		ThreadID:    event.ThreadID,
		TurnID:      event.TurnID,
		Title:       title,
		Body:        body,
		Options:     options,
		CreatedAt:   s.now(),
	}
	surface.PendingRequests[event.RequestID] = record
	return []control.UIEvent{{
		Kind:             control.UIEventRequestPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		RequestPrompt: &control.RequestPrompt{
			RequestID:   record.RequestID,
			RequestType: record.RequestType,
			Title:       record.Title,
			Body:        record.Body,
			ThreadID:    record.ThreadID,
			ThreadTitle: threadTitle,
			Options:     requestPromptOptionsToControl(record.Options),
		},
	}}
}

func (s *Service) resolveRequestPrompt(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.RequestID != "" {
		for _, surface := range s.findAttachedSurfaces(instanceID) {
			if surface.PendingRequests == nil {
				continue
			}
			delete(surface.PendingRequests, event.RequestID)
			clearSurfaceRequestCaptureByRequestID(surface, event.RequestID)
		}
		return nil
	}
	s.clearRequestsForTurn(instanceID, event.ThreadID, event.TurnID)
	return nil
}

func (s *Service) consumeCapturedRequestFeedback(surface *state.SurfaceConsoleRecord, action control.Action, text string) []control.UIEvent {
	capture := surface.ActiveRequestCapture
	if requestCaptureExpired(s.now(), capture) {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "request_capture_expired", "上一条确认反馈已过期，请重新点击卡片按钮后再发送处理意见。")
	}
	if capture == nil || capture.Mode != requestCaptureModeDeclineWithFeedback {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "request_capture_expired", "当前反馈模式已失效，请重新处理确认卡片。")
	}
	request := surface.PendingRequests[capture.RequestID]
	if request == nil {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "request_expired", "这个确认请求已经结束或过期了。请重新发送消息。")
	}
	inst := s.root.Instances[request.InstanceID]
	if inst == nil {
		clearSurfaceRequestCapture(surface)
		return notice(surface, "not_attached", "当前接管实例不可用，请重新接管后再发送消息。")
	}

	threadID := request.ThreadID
	cwd := inst.WorkspaceRoot
	routeMode := state.RouteModePinned
	if thread := inst.Threads[threadID]; threadVisible(thread) && thread.CWD != "" {
		cwd = thread.CWD
	}
	if threadID == "" {
		var createThread bool
		threadID, cwd, routeMode, createThread = freezeRoute(inst, surface)
		_ = createThread
	}

	clearSurfaceRequestCapture(surface)
	events := []control.UIEvent{{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandRequestRespond,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    surface.ActorUserID,
				ChatID:    surface.ChatID,
				MessageID: action.MessageID,
			},
			Target: agentproto.Target{
				ThreadID:               request.ThreadID,
				TurnID:                 request.TurnID,
				UseActiveTurnIfOmitted: request.TurnID == "",
			},
			Request: agentproto.Request{
				RequestID: request.RequestID,
				Response: map[string]any{
					"type":     "approval",
					"decision": "decline",
				},
			},
		},
	}}
	events = append(events, notice(surface, "request_feedback_queued", "已记录处理意见。当前确认会先被拒绝，随后继续处理你的下一步要求。")...)
	events = append(events, s.enqueueQueueItem(surface, action.MessageID, nil, []agentproto.Input{{Type: agentproto.InputText, Text: text}}, threadID, cwd, routeMode, surface.PromptOverride, true)...)
	return events
}

func (s *Service) enqueueQueueItem(surface *state.SurfaceConsoleRecord, sourceMessageID string, relatedMessageIDs []string, inputs []agentproto.Input, threadID, cwd string, routeMode state.RouteMode, overrides state.ModelConfigRecord, front bool) []control.UIEvent {
	s.nextQueueItemID++
	itemID := fmt.Sprintf("queue-%d", s.nextQueueItemID)
	inst := s.root.Instances[surface.AttachedInstanceID]
	sourceMessageIDs := uniqueStrings(append([]string{sourceMessageID}, relatedMessageIDs...))
	item := &state.QueueItemRecord{
		ID:                 itemID,
		SurfaceSessionID:   surface.SurfaceSessionID,
		SourceMessageID:    sourceMessageID,
		SourceMessageIDs:   sourceMessageIDs,
		Inputs:             inputs,
		FrozenThreadID:     threadID,
		FrozenCWD:          cwd,
		FrozenOverride:     s.resolveFrozenPromptOverride(inst, surface, threadID, cwd, overrides),
		RouteModeAtEnqueue: routeMode,
		Status:             state.QueueItemQueued,
	}
	surface.QueueItems[item.ID] = item
	if front {
		surface.QueuedQueueItemIDs = append([]string{item.ID}, surface.QueuedQueueItemIDs...)
	} else {
		surface.QueuedQueueItemIDs = append(surface.QueuedQueueItemIDs, item.ID)
	}
	position := len(surface.QueuedQueueItemIDs)
	if front {
		position = 1
	}
	events := []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     item.ID,
			SourceMessageID: item.SourceMessageID,
			Status:          string(item.Status),
			QueuePosition:   position,
			QueueOn:         true,
		},
	}}
	return append(events, s.dispatchNext(surface)...)
}

func (s *Service) consumeStagedInputs(surface *state.SurfaceConsoleRecord) ([]agentproto.Input, []string) {
	keys := make([]string, 0, len(surface.StagedImages))
	for imageID := range surface.StagedImages {
		keys = append(keys, imageID)
	}
	sort.Strings(keys)

	var inputs []agentproto.Input
	var sourceMessageIDs []string
	for _, imageID := range keys {
		image := surface.StagedImages[imageID]
		if image.State != state.ImageStaged {
			continue
		}
		inputs = append(inputs, agentproto.Input{
			Type:     agentproto.InputLocalImage,
			Path:     image.LocalPath,
			MIMEType: image.MIMEType,
		})
		image.State = state.ImageBound
		sourceMessageIDs = append(sourceMessageIDs, image.SourceMessageID)
	}
	return inputs, sourceMessageIDs
}

func freezeRoute(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord) (threadID, cwd string, routeMode state.RouteMode, createThread bool) {
	switch {
	case surface.SelectedThreadID != "":
		threadID = surface.SelectedThreadID
		if thread := inst.Threads[threadID]; threadVisible(thread) {
			cwd = thread.CWD
			return threadID, cwd, state.RouteModePinned, false
		}
	case surface.RouteMode == state.RouteModeFollowLocal && inst.ObservedFocusedThreadID != "":
		threadID = inst.ObservedFocusedThreadID
		if thread := inst.Threads[threadID]; threadVisible(thread) {
			cwd = thread.CWD
			return threadID, cwd, state.RouteModeFollowLocal, false
		}
	default:
		return "", inst.WorkspaceRoot, surface.RouteMode, true
	}
	return "", inst.WorkspaceRoot, surface.RouteMode, true
}

func (s *Service) dispatchNext(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.DispatchMode != state.DispatchModeNormal || surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) == 0 {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.pendingRemote[inst.InstanceID] != nil {
		return nil
	}

	queueID := surface.QueuedQueueItemIDs[0]
	surface.QueuedQueueItemIDs = surface.QueuedQueueItemIDs[1:]
	item := surface.QueueItems[queueID]
	if item == nil || item.Status != state.QueueItemQueued {
		return nil
	}
	item.Status = state.QueueItemDispatching
	surface.ActiveQueueItemID = item.ID
	s.pendingRemote[inst.InstanceID] = &remoteTurnBinding{
		InstanceID:       inst.InstanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
		QueueItemID:      item.ID,
		SourceMessageID:  item.SourceMessageID,
		ThreadID:         item.FrozenThreadID,
		Status:           string(item.Status),
	}

	command := &agentproto.Command{
		Kind: agentproto.CommandPromptSend,
		Origin: agentproto.Origin{
			Surface:   surface.SurfaceSessionID,
			UserID:    surface.ActorUserID,
			ChatID:    surface.ChatID,
			MessageID: item.SourceMessageID,
		},
		Target: agentproto.Target{
			ThreadID:              item.FrozenThreadID,
			CWD:                   item.FrozenCWD,
			CreateThreadIfMissing: item.FrozenThreadID == "",
		},
		Prompt: agentproto.Prompt{
			Inputs: item.Inputs,
		},
		Overrides: agentproto.PromptOverrides{
			Model:           item.FrozenOverride.Model,
			ReasoningEffort: item.FrozenOverride.ReasoningEffort,
			AccessMode:      item.FrozenOverride.AccessMode,
		},
	}

	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, true)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command:          command,
	})
	return events
}

func (s *Service) markRemoteTurnRunning(instanceID, threadID, turnID string) []control.UIEvent {
	binding := s.promotePendingRemote(instanceID, threadID, turnID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	if item.FrozenThreadID == "" {
		item.FrozenThreadID = threadID
		if item.FrozenThreadID != "" {
			surface.SelectedThreadID = item.FrozenThreadID
			surface.RouteMode = state.RouteModePinned
		}
	}
	item.Status = state.QueueItemRunning
	events := []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     item.ID,
			SourceMessageID: item.SourceMessageID,
			Status:          string(item.Status),
		},
	}}
	if item.FrozenThreadID != "" {
		inst := s.root.Instances[instanceID]
		events = append(events, s.bindSurfaceToThread(surface, inst, item.FrozenThreadID)...)
	}
	return events
}

func (s *Service) completeRemoteTurn(instanceID, threadID, turnID, status, errorMessage string) []control.UIEvent {
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil {
		s.clearRemoteTurn(instanceID, turnID)
		return nil
	}
	if status == "failed" {
		item.Status = state.QueueItemFailed
	} else {
		item.Status = state.QueueItemCompleted
	}
	surface.ActiveQueueItemID = ""
	events := appendPendingInputTyping(s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		QueueOff:    true,
	}, queueItemSourceMessageIDs(item)), item.SourceMessageID, false)
	if errorMessage != "" {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "turn_failed",
				Text: errorMessage,
			},
		})
	}
	events = append(events, s.dispatchNext(surface)...)
	s.clearRemoteTurn(instanceID, turnID)
	return events
}

func (s *Service) renderTextItem(instanceID, threadID, turnID, itemID, text string, final bool) []control.UIEvent {
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[instanceID]
	events := s.bindSurfaceToThread(surface, inst, threadID)
	blocks := s.renderer.PlanAssistantBlocks(surface.SurfaceSessionID, instanceID, threadID, turnID, itemID, text)
	thread := (*state.ThreadRecord)(nil)
	if inst != nil {
		thread = inst.Threads[threadID]
	}
	title := displayThreadTitle(inst, thread, threadID)
	themeKey := threadID
	if themeKey == "" {
		themeKey = title
	}
	for i := range blocks {
		block := blocks[i]
		block.ThreadTitle = title
		block.ThemeKey = themeKey
		block.Final = final
		events = append(events, control.UIEvent{
			Kind:             control.UIEventBlockCommitted,
			SurfaceSessionID: surface.SurfaceSessionID,
			Block:            &block,
		})
	}
	if thread != nil {
		thread.Preview = previewOfText(text)
		s.touchThread(thread)
	}
	return events
}

func (s *Service) trackItemStart(instanceID string, event agentproto.Event) {
	if event.ItemID == "" || !tracksTextItem(event.ItemKind) {
		return
	}
	buf := s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	if text, _ := event.Metadata["text"].(string); text != "" {
		buf.Text = text
	}
}

func (s *Service) trackItemDelta(instanceID string, event agentproto.Event) {
	if event.ItemID == "" || event.Delta == "" || !tracksTextItem(event.ItemKind) {
		return
	}
	buf := s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	buf.Text += event.Delta
}

func (s *Service) completeItem(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.ItemID == "" {
		return nil
	}
	key := itemBufferKey(instanceID, event.ThreadID, event.TurnID, event.ItemID)
	buf := s.itemBuffers[key]
	if buf == nil {
		buf = s.ensureItemBuffer(instanceID, event.ThreadID, event.TurnID, event.ItemID, event.ItemKind)
	}
	if buf.ItemKind == "" {
		buf.ItemKind = event.ItemKind
	}
	if text, _ := event.Metadata["text"].(string); text != "" {
		if buf.Text == "" || strings.TrimSpace(buf.Text) != strings.TrimSpace(text) {
			buf.Text = text
		}
		if buf.ItemKind == "" {
			buf.ItemKind = "agent_message"
		}
	}
	delete(s.itemBuffers, key)
	if !rendersTextItem(buf.ItemKind) || strings.TrimSpace(buf.Text) == "" {
		return nil
	}
	if buf.ItemKind == "agent_message" {
		return s.storePendingTurnText(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.ItemKind, buf.Text)
	}
	return s.renderTextItem(instanceID, event.ThreadID, event.TurnID, event.ItemID, buf.Text, false)
}

func (s *Service) storePendingTurnText(instanceID, threadID, turnID, itemID, itemKind, text string) []control.UIEvent {
	key := turnRenderKey(instanceID, threadID, turnID)
	previous := s.pendingTurnText[key]
	s.pendingTurnText[key] = &completedTextItem{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
		ItemID:     itemID,
		ItemKind:   itemKind,
		Text:       text,
	}
	if previous == nil {
		return nil
	}
	return s.renderTextItem(previous.InstanceID, previous.ThreadID, previous.TurnID, previous.ItemID, previous.Text, false)
}

func (s *Service) flushPendingTurnText(instanceID, threadID, turnID string, final bool) []control.UIEvent {
	key := turnRenderKey(instanceID, threadID, turnID)
	pending := s.pendingTurnText[key]
	if pending == nil {
		return nil
	}
	delete(s.pendingTurnText, key)
	return s.renderTextItem(pending.InstanceID, pending.ThreadID, pending.TurnID, pending.ItemID, pending.Text, final)
}

func (s *Service) flushPendingTurnTextIfTurnContinues(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.ThreadID == "" || event.TurnID == "" {
		return nil
	}
	if event.Kind == agentproto.EventTurnCompleted {
		return nil
	}
	key := turnRenderKey(instanceID, event.ThreadID, event.TurnID)
	pending := s.pendingTurnText[key]
	if pending == nil {
		return nil
	}
	switch event.Kind {
	case agentproto.EventItemStarted, agentproto.EventItemDelta, agentproto.EventItemCompleted:
		if event.ItemID == pending.ItemID {
			return nil
		}
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	case agentproto.EventRequestStarted, agentproto.EventRequestResolved:
		return s.flushPendingTurnText(instanceID, event.ThreadID, event.TurnID, false)
	default:
		return nil
	}
}

func (s *Service) normalizeTurnInitiator(instanceID string, event agentproto.Event) agentproto.Initiator {
	if event.Initiator.Kind != agentproto.InitiatorLocalUI && event.Initiator.Kind != agentproto.InitiatorUnknown {
		return event.Initiator
	}
	if binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID); binding != nil {
		return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: binding.SurfaceSessionID}
	}
	return event.Initiator
}

func queuedItemMatchesTurn(inst *state.InstanceRecord, item *state.QueueItemRecord, threadID string) bool {
	if item == nil {
		return false
	}
	if item.FrozenThreadID != "" {
		return threadID == "" || threadID == item.FrozenThreadID
	}
	if inst == nil {
		return threadID == ""
	}
	return threadID == "" || threadID == inst.ActiveThreadID
}

func (s *Service) pendingRemoteBinding(instanceID, threadID string) *remoteTurnBinding {
	binding := s.pendingRemote[instanceID]
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || (item.Status != state.QueueItemDispatching && item.Status != state.QueueItemRunning) {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	if !queuedItemMatchesTurn(s.root.Instances[instanceID], item, threadID) {
		return nil
	}
	return binding
}

func (s *Service) promotePendingRemote(instanceID, threadID, turnID string) *remoteTurnBinding {
	binding := s.pendingRemoteBinding(instanceID, threadID)
	if binding == nil {
		return s.activeRemoteBinding(instanceID, turnID)
	}
	delete(s.pendingRemote, instanceID)
	if threadID != "" {
		binding.ThreadID = threadID
	}
	binding.TurnID = turnID
	binding.Status = string(state.QueueItemRunning)
	s.activeRemote[instanceID] = binding
	return binding
}

func (s *Service) activeRemoteBinding(instanceID, turnID string) *remoteTurnBinding {
	binding := s.activeRemote[instanceID]
	if binding == nil {
		return nil
	}
	if turnID != "" && binding.TurnID != "" && binding.TurnID != turnID {
		return nil
	}
	return binding
}

func (s *Service) lookupRemoteTurn(instanceID, threadID, turnID string) *remoteTurnBinding {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		if threadID == "" || binding.ThreadID == "" || binding.ThreadID == threadID {
			return binding
		}
	}
	return s.pendingRemoteBinding(instanceID, threadID)
}

func (s *Service) clearRemoteTurn(instanceID, turnID string) {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		delete(s.activeRemote, instanceID)
	}
	if binding := s.pendingRemote[instanceID]; binding != nil && (turnID == "" || binding.TurnID == turnID) {
		delete(s.pendingRemote, instanceID)
	}
}

func (s *Service) clearRemoteOwnership(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.AttachedInstanceID == "" {
		return
	}
	if binding := s.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.pendingRemote, surface.AttachedInstanceID)
	}
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.activeRemote, surface.AttachedInstanceID)
	}
}

func (s *Service) remoteBindingForSurface(surface *state.SurfaceConsoleRecord) *remoteTurnBinding {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	if binding := s.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	return nil
}

func clearSurfaceRequests(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	clearSurfaceRequestCapture(surface)
}

func clearSurfaceRequestsForTurn(surface *state.SurfaceConsoleRecord, threadID, turnID string) {
	if surface == nil {
		return
	}
	if len(surface.PendingRequests) != 0 {
		for requestID, request := range surface.PendingRequests {
			if request == nil {
				delete(surface.PendingRequests, requestID)
				continue
			}
			if turnID != "" && request.TurnID != "" && request.TurnID != turnID {
				continue
			}
			if threadID != "" && request.ThreadID != "" && request.ThreadID != threadID {
				continue
			}
			delete(surface.PendingRequests, requestID)
		}
	}
	clearSurfaceRequestCaptureForTurn(surface, threadID, turnID)
}

func clearSurfaceRequestCapture(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceRequestCaptureByRequestID(surface *state.SurfaceConsoleRecord, requestID string) {
	if surface == nil || surface.ActiveRequestCapture == nil {
		return
	}
	if requestID == "" || surface.ActiveRequestCapture.RequestID != requestID {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceRequestCaptureForTurn(surface *state.SurfaceConsoleRecord, threadID, turnID string) {
	if surface == nil || surface.ActiveRequestCapture == nil {
		return
	}
	capture := surface.ActiveRequestCapture
	if turnID != "" && capture.TurnID != "" && capture.TurnID != turnID {
		return
	}
	if threadID != "" && capture.ThreadID != "" && capture.ThreadID != threadID {
		return
	}
	surface.ActiveRequestCapture = nil
}

func (s *Service) clearRequestsForTurn(instanceID, threadID, turnID string) {
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		clearSurfaceRequestsForTurn(surface, threadID, turnID)
	}
}

func (s *Service) clearTurnArtifacts(instanceID, threadID, turnID string) {
	deleteMatchingItemBuffers(s.itemBuffers, instanceID, threadID, turnID)
	if turnID == "" {
		return
	}
	delete(s.pendingTurnText, turnRenderKey(instanceID, threadID, turnID))
	s.clearRequestsForTurn(instanceID, threadID, turnID)
}

func (s *Service) turnSurface(instanceID, threadID, turnID string) *state.SurfaceConsoleRecord {
	if binding := s.lookupRemoteTurn(instanceID, threadID, turnID); binding != nil {
		if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
			return surface
		}
	}
	return s.findAttachedSurface(instanceID)
}

func (s *Service) pauseForLocal(instanceID string) []control.UIEvent {
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		if surface.DispatchMode == state.DispatchModePausedForLocal {
			continue
		}
		surface.DispatchMode = state.DispatchModePausedForLocal
		events = append(events, notice(surface, "local_activity_detected", "检测到本地 VS Code 正在使用，飞书消息将继续排队。")...)
	}
	return events
}

func (s *Service) enterHandoff(instanceID string) []control.UIEvent {
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		if surface.DispatchMode != state.DispatchModePausedForLocal {
			continue
		}
		if len(surface.QueuedQueueItemIDs) == 0 {
			surface.DispatchMode = state.DispatchModeNormal
			delete(s.handoffUntil, surface.SurfaceSessionID)
			continue
		}
		surface.DispatchMode = state.DispatchModeHandoffWait
		s.handoffUntil[surface.SurfaceSessionID] = s.now().Add(s.config.TurnHandoffWait)
	}
	return events
}

func (s *Service) buildSnapshot(surface *state.SurfaceConsoleRecord) *control.Snapshot {
	snapshot := &control.Snapshot{
		SurfaceSessionID: surface.SurfaceSessionID,
		ActorUserID:      surface.ActorUserID,
	}
	if pending := surface.PendingHeadless; pending != nil {
		snapshot.PendingHeadless = control.PendingHeadlessSummary{
			InstanceID:  pending.InstanceID,
			ThreadID:    pending.ThreadID,
			ThreadTitle: pending.ThreadTitle,
			ThreadCWD:   pending.ThreadCWD,
			Status:      string(pending.Status),
			PID:         pending.PID,
			ExpiresAt:   pending.ExpiresAt,
			RequestedAt: pending.RequestedAt,
		}
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		selected := inst.Threads[surface.SelectedThreadID]
		if !threadVisible(selected) {
			selected = nil
		}
		selectedTitle := ""
		selectedPreview := ""
		if selected != nil {
			selectedTitle = displayThreadTitle(inst, selected, surface.SelectedThreadID)
			selectedPreview = threadPreview(selected)
		}
		snapshot.Attachment = control.AttachmentSummary{
			InstanceID:            inst.InstanceID,
			DisplayName:           inst.DisplayName,
			Source:                inst.Source,
			Managed:               inst.Managed,
			PID:                   inst.PID,
			SelectedThreadID:      surface.SelectedThreadID,
			SelectedThreadTitle:   selectedTitle,
			SelectedThreadPreview: selectedPreview,
			RouteMode:             string(surface.RouteMode),
		}
		snapshot.NextPrompt = s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	}

	for _, inst := range s.root.Instances {
		snapshot.Instances = append(snapshot.Instances, control.InstanceSummary{
			InstanceID:              inst.InstanceID,
			DisplayName:             inst.DisplayName,
			WorkspaceRoot:           inst.WorkspaceRoot,
			WorkspaceKey:            inst.WorkspaceKey,
			Source:                  inst.Source,
			Managed:                 inst.Managed,
			PID:                     inst.PID,
			Online:                  inst.Online,
			State:                   threadStateForInstance(inst),
			ObservedFocusedThreadID: inst.ObservedFocusedThreadID,
		})
		if inst.InstanceID != surface.AttachedInstanceID {
			continue
		}
		for _, thread := range visibleThreads(inst) {
			snapshot.Threads = append(snapshot.Threads, control.ThreadSummary{
				ThreadID:          thread.ThreadID,
				Name:              thread.Name,
				DisplayTitle:      displayThreadTitle(inst, thread, thread.ThreadID),
				Preview:           thread.Preview,
				CWD:               thread.CWD,
				State:             thread.State,
				Model:             thread.ExplicitModel,
				ReasoningEffort:   thread.ExplicitReasoningEffort,
				Loaded:            thread.Loaded,
				IsObservedFocused: inst.ObservedFocusedThreadID == thread.ThreadID,
				IsSelected:        surface.SelectedThreadID == thread.ThreadID,
			})
		}
	}
	sort.Slice(snapshot.Instances, func(i, j int) bool {
		return snapshot.Instances[i].WorkspaceKey < snapshot.Instances[j].WorkspaceKey
	})
	return snapshot
}

func (s *Service) resolveNextPromptSummary(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, frozenThreadID, frozenCWD string, override state.ModelConfigRecord) control.PromptRouteSummary {
	if inst == nil || surface == nil {
		return control.PromptRouteSummary{}
	}
	threadID := frozenThreadID
	cwd := frozenCWD
	routeMode := surface.RouteMode
	createThread := false
	if threadID == "" && cwd == "" {
		threadID, cwd, routeMode, createThread = freezeRoute(inst, surface)
	} else {
		createThread = threadID == ""
	}
	if promptOverrideIsEmpty(override) {
		override = surface.PromptOverride
	}
	threadTitle := ""
	if threadID != "" {
		threadTitle = displayThreadTitle(inst, inst.Threads[threadID], threadID)
	}
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return control.PromptRouteSummary{
		RouteMode:                      string(routeMode),
		ThreadID:                       threadID,
		ThreadTitle:                    threadTitle,
		CWD:                            cwd,
		CreateThread:                   createThread,
		BaseModel:                      resolution.BaseModel.Value,
		BaseReasoningEffort:            resolution.BaseReasoningEffort.Value,
		BaseModelSource:                resolution.BaseModel.Source,
		BaseReasoningEffortSource:      resolution.BaseReasoningEffort.Source,
		OverrideModel:                  resolution.Override.Model,
		OverrideReasoningEffort:        resolution.Override.ReasoningEffort,
		OverrideAccessMode:             resolution.Override.AccessMode,
		EffectiveModel:                 resolution.EffectiveModel.Value,
		EffectiveReasoningEffort:       resolution.EffectiveReasoningEffort.Value,
		EffectiveAccessMode:            resolution.EffectiveAccessMode,
		EffectiveModelSource:           resolution.EffectiveModel.Source,
		EffectiveReasoningEffortSource: resolution.EffectiveReasoningEffort.Source,
		EffectiveAccessModeSource:      resolution.EffectiveAccessModeSource,
	}
}

type configValue struct {
	Value  string
	Source string
}

type promptConfigResolution struct {
	Override                  state.ModelConfigRecord
	BaseModel                 configValue
	BaseReasoningEffort       configValue
	EffectiveModel            configValue
	EffectiveReasoningEffort  configValue
	EffectiveAccessMode       string
	EffectiveAccessModeSource string
}

func promptOverrideIsEmpty(value state.ModelConfigRecord) bool {
	return strings.TrimSpace(value.Model) == "" &&
		strings.TrimSpace(value.ReasoningEffort) == "" &&
		strings.TrimSpace(value.AccessMode) == ""
}

func compactPromptOverride(value state.ModelConfigRecord) state.ModelConfigRecord {
	value.AccessMode = agentproto.NormalizeAccessMode(value.AccessMode)
	if promptOverrideIsEmpty(value) {
		return state.ModelConfigRecord{}
	}
	return value
}

func (s *Service) resolveFrozenPromptOverride(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) state.ModelConfigRecord {
	resolution := s.resolvePromptConfig(inst, surface, threadID, cwd, override)
	return state.ModelConfigRecord{
		Model:           resolution.EffectiveModel.Value,
		ReasoningEffort: resolution.EffectiveReasoningEffort.Value,
		AccessMode:      resolution.EffectiveAccessMode,
	}
}

func (s *Service) resolvePromptConfig(inst *state.InstanceRecord, surface *state.SurfaceConsoleRecord, threadID, cwd string, override state.ModelConfigRecord) promptConfigResolution {
	if surface != nil && promptOverrideIsEmpty(override) {
		override = surface.PromptOverride
	}
	override = compactPromptOverride(override)
	baseModel, baseEffort := resolveBasePromptConfig(inst, threadID, cwd)
	effectiveModel := baseModel
	if override.Model != "" {
		effectiveModel = configValue{Value: override.Model, Source: "surface_override"}
	} else if effectiveModel.Value == "" {
		effectiveModel = configValue{Value: defaultModel, Source: "surface_default"}
	}
	effectiveEffort := baseEffort
	if override.ReasoningEffort != "" {
		effectiveEffort = configValue{Value: override.ReasoningEffort, Source: "surface_override"}
	} else if effectiveEffort.Value == "" {
		effectiveEffort = configValue{Value: defaultReasoningEffort, Source: "surface_default"}
	}
	effectiveAccessMode := agentproto.EffectiveAccessMode(override.AccessMode)
	effectiveAccessModeSource := "surface_default"
	if agentproto.NormalizeAccessMode(override.AccessMode) != "" {
		effectiveAccessModeSource = "surface_override"
	}
	return promptConfigResolution{
		Override:                  override,
		BaseModel:                 baseModel,
		BaseReasoningEffort:       baseEffort,
		EffectiveModel:            effectiveModel,
		EffectiveReasoningEffort:  effectiveEffort,
		EffectiveAccessMode:       effectiveAccessMode,
		EffectiveAccessModeSource: effectiveAccessModeSource,
	}
}

func resolveBasePromptConfig(inst *state.InstanceRecord, threadID, cwd string) (configValue, configValue) {
	model := configValue{Source: "unknown"}
	effort := configValue{Source: "unknown"}
	if inst == nil {
		return model, effort
	}
	if thread := inst.Threads[threadID]; thread != nil {
		if cwd == "" {
			cwd = thread.CWD
		}
		if thread.ExplicitModel != "" {
			model = configValue{Value: thread.ExplicitModel, Source: "thread"}
		}
		if thread.ExplicitReasoningEffort != "" {
			effort = configValue{Value: thread.ExplicitReasoningEffort, Source: "thread"}
		}
	}
	if cwd != "" {
		if defaults, ok := inst.CWDDefaults[cwd]; ok {
			if model.Value == "" && defaults.Model != "" {
				model = configValue{Value: defaults.Model, Source: "cwd_default"}
			}
			if effort.Value == "" && defaults.ReasoningEffort != "" {
				effort = configValue{Value: defaults.ReasoningEffort, Source: "cwd_default"}
			}
		}
	}
	return model, effort
}

func (s *Service) findAttachedSurface(instanceID string) *state.SurfaceConsoleRecord {
	for _, surface := range s.root.Surfaces {
		if surface.AttachedInstanceID == instanceID {
			return surface
		}
	}
	return nil
}

func (s *Service) findAttachedSurfaces(instanceID string) []*state.SurfaceConsoleRecord {
	var surfaces []*state.SurfaceConsoleRecord
	for _, surface := range s.root.Surfaces {
		if surface.AttachedInstanceID == instanceID {
			surfaces = append(surfaces, surface)
		}
	}
	return surfaces
}

func (s *Service) SurfaceSnapshot(surfaceID string) *control.Snapshot {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return nil
	}
	return s.buildSnapshot(surface)
}

func (s *Service) AttachedInstanceID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.AttachedInstanceID
}

func (s *Service) SurfaceChatID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.ChatID
}

func (s *Service) SurfaceActorUserID(surfaceID string) string {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return ""
	}
	return surface.ActorUserID
}

func (s *Service) BindPendingRemoteCommand(surfaceID, commandID string) {
	if commandID == "" {
		return
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.AttachedInstanceID == "" {
		return
	}
	binding := s.pendingRemote[surface.AttachedInstanceID]
	if binding == nil || binding.SurfaceSessionID != surfaceID {
		return
	}
	if surface.ActiveQueueItemID != "" && binding.QueueItemID != surface.ActiveQueueItemID {
		return
	}
	binding.CommandID = commandID
}

func (s *Service) failSurfaceActiveQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, notice *control.Notice, tryDispatchNext bool) []control.UIEvent {
	if surface == nil || item == nil {
		return nil
	}
	item.Status = state.QueueItemFailed
	if surface.ActiveQueueItemID == item.ID {
		surface.ActiveQueueItemID = ""
	}
	if binding := s.remoteBindingForSurface(surface); binding != nil {
		s.clearTurnArtifacts(binding.InstanceID, binding.ThreadID, binding.TurnID)
	}
	s.clearRemoteOwnership(surface)

	events := []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     item.ID,
			SourceMessageID: item.SourceMessageID,
			Status:          string(item.Status),
			TypingOff:       true,
		},
	}}
	if notice != nil && (strings.TrimSpace(notice.Code) != "" || strings.TrimSpace(notice.Title) != "" || strings.TrimSpace(notice.Text) != "") {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	if tryDispatchNext {
		events = append(events, s.dispatchNext(surface)...)
	}
	return events
}

func (s *Service) HandleCommandDispatchFailure(surfaceID string, err error) []control.UIEvent {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.ActiveQueueItemID == "" {
		return nil
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.Status != state.QueueItemDispatching {
		return nil
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             "dispatch_failed",
		Layer:            "daemon",
		Stage:            "dispatch_command",
		Message:          "消息未成功发送到本地 Codex。",
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	notice := NoticeForProblem(problem)
	notice.Code = "dispatch_failed"
	return s.failSurfaceActiveQueueItem(surface, item, &notice, true)
}

func (s *Service) HandleCommandRejected(instanceID string, ack agentproto.CommandAck) []control.UIEvent {
	if ack.CommandID == "" {
		return nil
	}
	binding := s.pendingRemote[instanceID]
	if binding == nil || binding.CommandID != ack.CommandID {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || item.Status != state.QueueItemDispatching {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	notice := NoticeForProblem(commandAckProblem(surface.SurfaceSessionID, ack))
	notice.Code = "command_rejected"
	return s.failSurfaceActiveQueueItem(surface, item, &notice, true)
}

func (s *Service) HandleHeadlessLaunchStarted(surfaceID, instanceID string, pid int) []control.UIEvent {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID {
		return nil
	}
	surface.PendingHeadless.PID = pid
	return nil
}

func (s *Service) HandleHeadlessLaunchFailed(surfaceID, instanceID string, err error) []control.UIEvent {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID {
		return nil
	}
	pending := surface.PendingHeadless
	surface.PendingHeadless = nil
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             "headless_start_failed",
		Layer:            "daemon",
		Stage:            "headless_start",
		Operation:        "new_instance",
		Message:          "无法创建 headless 实例。",
		SurfaceSessionID: surface.SurfaceSessionID,
		ThreadID:         pending.ThreadID,
		Retryable:        true,
	})
	notice := NoticeForProblem(problem)
	notice.Code = "headless_start_failed"
	notice.Title = "Headless 实例创建失败"
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	}}
}

func (s *Service) Instance(instanceID string) *state.InstanceRecord {
	return s.root.Instances[instanceID]
}

func (s *Service) Instances() []*state.InstanceRecord {
	instances := make([]*state.InstanceRecord, 0, len(s.root.Instances))
	for _, instance := range s.root.Instances {
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].WorkspaceKey == instances[j].WorkspaceKey {
			return instances[i].InstanceID < instances[j].InstanceID
		}
		return instances[i].WorkspaceKey < instances[j].WorkspaceKey
	})
	return instances
}

func (s *Service) Surfaces() []*state.SurfaceConsoleRecord {
	surfaces := make([]*state.SurfaceConsoleRecord, 0, len(s.root.Surfaces))
	for _, surface := range s.root.Surfaces {
		surfaces = append(surfaces, surface)
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].SurfaceSessionID < surfaces[j].SurfaceSessionID
	})
	return surfaces
}

type RemoteTurnStatus struct {
	InstanceID       string `json:"instanceId"`
	SurfaceSessionID string `json:"surfaceSessionId"`
	QueueItemID      string `json:"queueItemId"`
	SourceMessageID  string `json:"sourceMessageId,omitempty"`
	CommandID        string `json:"commandId,omitempty"`
	ThreadID         string `json:"threadId,omitempty"`
	TurnID           string `json:"turnId,omitempty"`
	Status           string `json:"status"`
}

func (s *Service) PendingRemoteTurns() []RemoteTurnStatus {
	values := make([]RemoteTurnStatus, 0, len(s.pendingRemote))
	for _, binding := range s.pendingRemote {
		if binding == nil {
			continue
		}
		values = append(values, RemoteTurnStatus{
			InstanceID:       binding.InstanceID,
			SurfaceSessionID: binding.SurfaceSessionID,
			QueueItemID:      binding.QueueItemID,
			SourceMessageID:  binding.SourceMessageID,
			CommandID:        binding.CommandID,
			ThreadID:         binding.ThreadID,
			TurnID:           binding.TurnID,
			Status:           binding.Status,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].InstanceID == values[j].InstanceID {
			return values[i].QueueItemID < values[j].QueueItemID
		}
		return values[i].InstanceID < values[j].InstanceID
	})
	return values
}

func (s *Service) ActiveRemoteTurns() []RemoteTurnStatus {
	values := make([]RemoteTurnStatus, 0, len(s.activeRemote))
	for _, binding := range s.activeRemote {
		if binding == nil {
			continue
		}
		values = append(values, RemoteTurnStatus{
			InstanceID:       binding.InstanceID,
			SurfaceSessionID: binding.SurfaceSessionID,
			QueueItemID:      binding.QueueItemID,
			SourceMessageID:  binding.SourceMessageID,
			CommandID:        binding.CommandID,
			ThreadID:         binding.ThreadID,
			TurnID:           binding.TurnID,
			Status:           binding.Status,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].InstanceID == values[j].InstanceID {
			return values[i].TurnID < values[j].TurnID
		}
		return values[i].InstanceID < values[j].InstanceID
	})
	return values
}

func (s *Service) ApplyInstanceConnected(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = true

	var events []control.UIEvent
	for _, surface := range s.root.Surfaces {
		pending := surface.PendingHeadless
		if pending == nil || pending.InstanceID != instanceID {
			continue
		}
		events = append(events, s.attachHeadlessInstance(surface, inst, pending)...)
	}
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.dispatchNext(surface)...)
	}
	return events
}

func (s *Service) ApplyInstanceDisconnected(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	inst.Online = false
	inst.ActiveTurnID = ""

	for _, surface := range s.root.Surfaces {
		if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != instanceID {
			continue
		}
		surface.PendingHeadless = nil
		if surface.SelectionPrompt != nil && surface.SelectionPrompt.Kind == "new_instance_thread" {
			surface.SelectionPrompt = nil
		}
	}

	surfaces := s.findAttachedSurfaces(instanceID)
	if len(surfaces) == 0 {
		delete(s.pendingRemote, instanceID)
		delete(s.activeRemote, instanceID)
		return nil
	}

	var events []control.UIEvent
	for _, surface := range surfaces {
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.ActiveTurnOrigin = ""
		surface.DispatchMode = state.DispatchModeNormal
		delete(s.handoffUntil, surface.SurfaceSessionID)
		clearSurfaceRequests(surface)

		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				events = append(events, s.failSurfaceActiveQueueItem(surface, item, &control.Notice{
					Code: "attached_instance_offline",
					Text: fmt.Sprintf("当前接管实例已离线：%s", inst.DisplayName),
				}, false)...)
				continue
			}
			surface.ActiveQueueItemID = ""
		}

		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "attached_instance_offline",
				Text: fmt.Sprintf("当前接管实例已离线：%s", inst.DisplayName),
			},
		})
	}
	delete(s.pendingRemote, instanceID)
	delete(s.activeRemote, instanceID)
	return events
}

func (s *Service) observeConfig(inst *state.InstanceRecord, threadID, cwd, scope, model, effort string) {
	if inst == nil {
		return
	}
	switch scope {
	case "cwd_default":
		if cwd == "" {
			return
		}
		if inst.CWDDefaults == nil {
			inst.CWDDefaults = map[string]state.ModelConfigRecord{}
		}
		current := inst.CWDDefaults[cwd]
		if model != "" {
			current.Model = model
		}
		if effort != "" {
			current.ReasoningEffort = effort
		}
		inst.CWDDefaults[cwd] = current
	default:
		if threadID == "" {
			return
		}
		thread := s.ensureThread(inst, threadID)
		if cwd != "" {
			thread.CWD = cwd
		}
		if model != "" {
			thread.ExplicitModel = model
		}
		if effort != "" {
			thread.ExplicitReasoningEffort = effort
		}
	}
}

func (s *Service) discardDrafts(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	for _, image := range surface.StagedImages {
		if image.State != state.ImageStaged {
			continue
		}
		image.State = state.ImageDiscarded
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: image.ImageID,
			Status:      string(image.State),
			QueueOff:    true,
			ThumbsDown:  true,
		}, []string{image.SourceMessageID})...)
	}
	for _, queueID := range append([]string{}, surface.QueuedQueueItemIDs...) {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued {
			continue
		}
		item.Status = state.QueueItemDiscarded
		s.markImagesForMessages(surface, queueItemSourceMessageIDs(item), state.ImageDiscarded)
		events = append(events, s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsDown:  true,
		}, queueItemSourceMessageIDs(item))...)
	}
	surface.QueuedQueueItemIDs = nil
	surface.StagedImages = map[string]*state.StagedImageRecord{}
	return events
}

func (s *Service) maybePromoteWorkspaceRoot(inst *state.InstanceRecord, cwd string) {
	if cwd == "" {
		return
	}
	switch {
	case inst.WorkspaceRoot == "":
		inst.WorkspaceRoot = cwd
	case strings.HasPrefix(inst.WorkspaceRoot, cwd+string(os.PathSeparator)):
		inst.WorkspaceRoot = cwd
	}
	inst.WorkspaceKey = inst.WorkspaceRoot
	inst.ShortName = filepath.Base(inst.WorkspaceKey)
	if inst.DisplayName == "" {
		inst.DisplayName = inst.ShortName
	}
}

func (s *Service) retargetManagedHeadlessInstance(inst *state.InstanceRecord, cwd string) {
	if inst == nil || strings.TrimSpace(cwd) == "" || !isHeadlessInstance(inst) {
		return
	}
	inst.WorkspaceRoot = cwd
	inst.WorkspaceKey = cwd
	inst.ShortName = filepath.Base(cwd)
	inst.DisplayName = inst.ShortName
}

func (s *Service) threadFocusEvents(instanceID, threadID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.maybeRequestThreadRefresh(surface, inst, threadID)...)
	}
	return events
}

func (s *Service) bindSurfaceToThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	if surface == nil || inst == nil || threadID == "" {
		return nil
	}
	thread := s.ensureThread(inst, threadID)
	if !threadVisible(thread) {
		return nil
	}
	surface.SelectedThreadID = threadID
	surface.RouteMode = state.RouteModePinned
	return s.threadSelectionEvents(
		surface,
		threadID,
		string(surface.RouteMode),
		displayThreadTitle(inst, thread, threadID),
		threadPreview(thread),
	)
}

func (s *Service) threadSelectionEvents(surface *state.SurfaceConsoleRecord, threadID, routeMode, title, preview string) []control.UIEvent {
	if surface.LastSelection != nil &&
		surface.LastSelection.ThreadID == threadID &&
		surface.LastSelection.RouteMode == routeMode {
		surface.LastSelection.Title = title
		surface.LastSelection.Preview = preview
		return nil
	}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  threadID,
		RouteMode: routeMode,
		Title:     title,
		Preview:   preview,
	}
	return []control.UIEvent{threadSelectionEvent(surface, threadID, routeMode, title, preview)}
}

func notice(surface *state.SurfaceConsoleRecord, code, text string) []control.UIEvent {
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &control.Notice{Code: code, Text: text},
	}}
}

func selectionExpiredText(prompt *state.SelectionPromptRecord) string {
	if prompt != nil && prompt.Kind == "new_instance_thread" {
		return "这个按钮对应的选择已过期，请重新发送 /use 查看可恢复会话，或执行 /killinstance 取消。"
	}
	return "这个按钮对应的选择已过期，请重新发送 /list、/use 或 /useall。"
}

func (s *Service) HandleProblem(instanceID string, problem agentproto.ErrorInfo) []control.UIEvent {
	return s.handleProblem(instanceID, problem)
}

func (s *Service) handleProblem(instanceID string, problem agentproto.ErrorInfo) []control.UIEvent {
	problem = problem.Normalize()
	surfaces := s.problemTargets(instanceID, problem)
	if len(surfaces) == 0 {
		return nil
	}
	notice := NoticeForProblem(problem)
	events := make([]control.UIEvent, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		noticeCopy := notice
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &noticeCopy,
		})
	}
	return events
}

func (s *Service) problemTargets(instanceID string, problem agentproto.ErrorInfo) []*state.SurfaceConsoleRecord {
	if surface := s.root.Surfaces[problem.SurfaceSessionID]; surface != nil {
		return []*state.SurfaceConsoleRecord{surface}
	}
	if problem.CommandID != "" {
		for _, binding := range s.pendingRemote {
			if binding != nil && binding.CommandID == problem.CommandID {
				if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
					return []*state.SurfaceConsoleRecord{surface}
				}
			}
		}
		for _, binding := range s.activeRemote {
			if binding != nil && binding.CommandID == problem.CommandID {
				if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
					return []*state.SurfaceConsoleRecord{surface}
				}
			}
		}
	}
	if surface := s.turnSurface(instanceID, problem.ThreadID, problem.TurnID); surface != nil {
		return []*state.SurfaceConsoleRecord{surface}
	}
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	return s.findAttachedSurfaces(instanceID)
}

func commandAckProblem(surfaceID string, ack agentproto.CommandAck) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Code:             "command_rejected",
		Layer:            "wrapper",
		Stage:            "command_ack",
		Message:          "本地 Codex 拒绝了这条消息。",
		Details:          strings.TrimSpace(ack.Error),
		SurfaceSessionID: surfaceID,
		CommandID:        ack.CommandID,
	}
	if ack.Problem == nil {
		return defaults.Normalize()
	}
	return ack.Problem.WithDefaults(defaults)
}

func problemFromEvent(event agentproto.Event) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Message:   event.ErrorMessage,
		ThreadID:  event.ThreadID,
		TurnID:    event.TurnID,
		ItemID:    event.ItemID,
		RequestID: event.RequestID,
	}
	if event.Problem == nil {
		return defaults.Normalize()
	}
	return event.Problem.WithDefaults(defaults)
}

func isHeadlessInstance(inst *state.InstanceRecord) bool {
	return inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func lookupStringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeRequestType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "", normalized == "approval", normalized == "confirm", normalized == "confirmation":
		return strings.ToLower(strings.TrimSpace(firstNonEmpty(value, "approval")))
	case strings.HasPrefix(normalized, "approval"):
		return "approval"
	case strings.HasPrefix(normalized, "confirm"):
		return "approval"
	default:
		return normalized
	}
}

func normalizeRequestOptionID(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	switch normalized {
	case "accept", "allow", "approve", "yes":
		return "accept"
	case "acceptforsession", "allowforsession", "allowthissession", "session":
		return "acceptForSession"
	case "decline", "deny", "reject", "no":
		return "decline"
	case "capturefeedback", "feedback", "tellcodexwhattodo", "tellcodexwhattododifferently":
		return "captureFeedback"
	default:
		return strings.TrimSpace(value)
	}
}

func requestOptionIDFromApproved(approved bool) string {
	if approved {
		return "accept"
	}
	return "decline"
}

func requestHasOption(request *state.RequestPromptRecord, optionID string) bool {
	if request == nil {
		return false
	}
	if len(request.Options) == 0 {
		switch optionID {
		case "accept", "decline":
			return true
		default:
			return false
		}
	}
	for _, option := range request.Options {
		if normalizeRequestOptionID(option.OptionID) == optionID {
			return true
		}
	}
	return false
}

func decisionForRequestOption(optionID string) string {
	switch normalizeRequestOptionID(optionID) {
	case "accept":
		return "accept"
	case "acceptForSession":
		return "acceptForSession"
	case "decline":
		return "decline"
	default:
		return ""
	}
}

func activePendingRequest(surface *state.SurfaceConsoleRecord) *state.RequestPromptRecord {
	if surface == nil || len(surface.PendingRequests) == 0 {
		return nil
	}
	for requestID, request := range surface.PendingRequests {
		if request == nil {
			delete(surface.PendingRequests, requestID)
			continue
		}
		return request
	}
	return nil
}

func requestCaptureExpired(now time.Time, capture *state.RequestCaptureRecord) bool {
	if capture == nil || capture.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(capture.ExpiresAt)
}

func requestPromptOptionsToControl(options []state.RequestPromptOptionRecord) []control.RequestPromptOption {
	if len(options) == 0 {
		return nil
	}
	out := make([]control.RequestPromptOption, 0, len(options))
	for _, option := range options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			continue
		}
		out = append(out, control.RequestPromptOption{
			OptionID: strings.TrimSpace(option.OptionID),
			Label:    label,
			Style:    strings.TrimSpace(option.Style),
		})
	}
	return out
}

func buildApprovalRequestOptions(metadata map[string]any) []state.RequestPromptOptionRecord {
	var options []state.RequestPromptOptionRecord
	seen := map[string]bool{}
	add := func(optionID, label, style string) {
		optionID = normalizeRequestOptionID(optionID)
		if optionID == "" || seen[optionID] {
			return
		}
		switch optionID {
		case "accept", "acceptForSession", "decline", "captureFeedback":
		default:
			return
		}
		if label == "" {
			switch optionID {
			case "accept":
				label = "允许一次"
			case "acceptForSession":
				label = "本会话允许"
			case "decline":
				label = "拒绝"
			case "captureFeedback":
				label = "告诉 Codex 怎么改"
			default:
				return
			}
		}
		if style == "" {
			switch optionID {
			case "accept":
				style = "primary"
			default:
				style = "default"
			}
		}
		options = append(options, state.RequestPromptOptionRecord{
			OptionID: optionID,
			Label:    label,
			Style:    style,
		})
		seen[optionID] = true
	}

	for _, option := range metadataRequestOptions(metadata) {
		add(option.OptionID, option.Label, option.Style)
	}
	if len(options) == 0 {
		add("accept", firstNonEmpty(metadataString(metadata, "acceptLabel"), "允许一次"), "primary")
		if approvalRequestSupportsSession(metadata) {
			add("acceptForSession", "本会话允许", "default")
		}
		add("decline", firstNonEmpty(metadataString(metadata, "declineLabel"), "拒绝"), "default")
	}
	add("captureFeedback", "告诉 Codex 怎么改", "default")
	return options
}

func approvalRequestSupportsSession(metadata map[string]any) bool {
	if len(metadataRequestOptions(metadata)) != 0 {
		for _, option := range metadataRequestOptions(metadata) {
			if normalizeRequestOptionID(option.OptionID) == "acceptForSession" {
				return true
			}
		}
		return false
	}
	switch strings.ToLower(strings.TrimSpace(metadataString(metadata, "requestKind"))) {
	case "approval_command", "approval_file_change", "approval_network":
		return true
	default:
		return false
	}
}

func metadataRequestOptions(metadata map[string]any) []state.RequestPromptOptionRecord {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["options"]
	if !ok {
		return nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []map[string]any:
		values = make([]any, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
	default:
		return nil
	}
	options := make([]state.RequestPromptOptionRecord, 0, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		optionID := firstNonEmpty(
			lookupStringFromAny(record["id"]),
			lookupStringFromAny(record["optionId"]),
			lookupStringFromAny(record["decision"]),
			lookupStringFromAny(record["value"]),
			lookupStringFromAny(record["action"]),
		)
		optionID = normalizeRequestOptionID(optionID)
		if optionID == "" {
			continue
		}
		label := firstNonEmpty(
			lookupStringFromAny(record["label"]),
			lookupStringFromAny(record["title"]),
			lookupStringFromAny(record["text"]),
			lookupStringFromAny(record["name"]),
		)
		style := firstNonEmpty(
			lookupStringFromAny(record["style"]),
			lookupStringFromAny(record["appearance"]),
			lookupStringFromAny(record["variant"]),
		)
		options = append(options, state.RequestPromptOptionRecord{
			OptionID: optionID,
			Label:    label,
			Style:    style,
		})
	}
	return options
}

func threadSelectionEvent(surface *state.SurfaceConsoleRecord, threadID, routeMode, title, preview string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventThreadSelectionChange,
		SurfaceSessionID: surface.SurfaceSessionID,
		ThreadSelection: &control.ThreadSelectionChanged{
			ThreadID:  threadID,
			RouteMode: routeMode,
			Title:     title,
			Preview:   preview,
		},
	}
}

func (s *Service) touchThread(thread *state.ThreadRecord) {
	if thread == nil {
		return
	}
	thread.LastUsedAt = s.now()
}

func (s *Service) pendingInputEvents(surface *state.SurfaceConsoleRecord, pending control.PendingInputState, sourceMessageIDs []string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	messageIDs := uniqueStrings(sourceMessageIDs)
	if len(messageIDs) == 0 && pending.SourceMessageID != "" {
		messageIDs = []string{pending.SourceMessageID}
	}
	if len(messageIDs) == 0 {
		return nil
	}
	events := make([]control.UIEvent, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		pendingCopy := pending
		pendingCopy.SourceMessageID = messageID
		events = append(events, control.UIEvent{
			Kind:             control.UIEventPendingInput,
			SurfaceSessionID: surface.SurfaceSessionID,
			PendingInput:     &pendingCopy,
		})
	}
	return events
}

func appendPendingInputTyping(events []control.UIEvent, primaryMessageID string, typingOn bool) []control.UIEvent {
	if primaryMessageID == "" {
		return events
	}
	for i := range events {
		pending := events[i].PendingInput
		if pending == nil || pending.SourceMessageID != primaryMessageID {
			continue
		}
		pending.TypingOn = typingOn
		pending.TypingOff = !typingOn
		return events
	}
	return events
}

func queueItemSourceMessageIDs(item *state.QueueItemRecord) []string {
	if item == nil {
		return nil
	}
	return uniqueStrings(append([]string{item.SourceMessageID}, item.SourceMessageIDs...))
}

func queueItemHasSourceMessage(item *state.QueueItemRecord, messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || item == nil {
		return false
	}
	for _, candidate := range queueItemSourceMessageIDs(item) {
		if candidate == messageID {
			return true
		}
	}
	return false
}

func (s *Service) markImagesForMessages(surface *state.SurfaceConsoleRecord, sourceMessageIDs []string, next state.ImageState) {
	if surface == nil || len(surface.StagedImages) == 0 {
		return
	}
	targets := map[string]struct{}{}
	for _, messageID := range uniqueStrings(sourceMessageIDs) {
		if messageID == "" {
			continue
		}
		targets[messageID] = struct{}{}
	}
	if len(targets) == 0 {
		return
	}
	for _, image := range surface.StagedImages {
		if image == nil {
			continue
		}
		if _, ok := targets[image.SourceMessageID]; ok {
			image.State = next
		}
	}
}

func countPendingDrafts(surface *state.SurfaceConsoleRecord) int {
	if surface == nil {
		return 0
	}
	total := 0
	for _, image := range surface.StagedImages {
		if image != nil && image.State == state.ImageStaged {
			total++
		}
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		if item := surface.QueueItems[queueID]; item != nil && item.Status == state.QueueItemQueued {
			total++
		}
	}
	return total
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isDigits(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}

func threadTitle(inst *state.InstanceRecord, thread *state.ThreadRecord, fallback string) string {
	if inst == nil {
		inst = &state.InstanceRecord{}
	}
	short := inst.ShortName
	if short == "" {
		short = filepath.Base(inst.WorkspaceKey)
	}
	if short == "" {
		short = inst.DisplayName
	}
	if thread == nil {
		if fallback == "" {
			return short
		}
		return fmt.Sprintf("%s · %s", short, shortenThreadID(fallback))
	}
	if thread.Name != "" {
		return fmt.Sprintf("%s · %s", short, thread.Name)
	}
	if summary := previewSnippet(thread.Preview); summary != "" {
		return fmt.Sprintf("%s · %s", short, summary)
	}
	if thread.CWD != "" {
		base := filepath.Base(thread.CWD)
		switch {
		case base == "", base == ".", base == string(filepath.Separator), base == short:
			return fmt.Sprintf("%s · %s", short, shortenThreadID(fallback))
		default:
			return fmt.Sprintf("%s · %s · %s", short, base, shortenThreadID(fallback))
		}
	}
	if fallback == "" {
		return short
	}
	return fmt.Sprintf("%s · %s", short, shortenThreadID(fallback))
}

func displayThreadTitle(inst *state.InstanceRecord, thread *state.ThreadRecord, fallback string) string {
	title := threadTitle(inst, thread, fallback)
	if inst == nil || fallback == "" {
		return title
	}
	shortID := shortenThreadID(fallback)
	if strings.Contains(title, shortID) {
		return title
	}
	if duplicateThreadTitle(inst, title) {
		return fmt.Sprintf("%s · %s", title, shortID)
	}
	return title
}

func duplicateThreadTitle(inst *state.InstanceRecord, title string) bool {
	if inst == nil || title == "" {
		return false
	}
	count := 0
	for threadID, thread := range inst.Threads {
		if !threadVisible(thread) {
			continue
		}
		if threadTitle(inst, thread, threadID) != title {
			continue
		}
		count++
		if count > 1 {
			return true
		}
	}
	return false
}

func threadPreview(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	return previewSnippet(thread.Preview)
}

func (s *Service) maybeRequestThreadRefresh(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	if surface == nil || inst == nil || surface.AttachedInstanceID != inst.InstanceID {
		return nil
	}
	if s.threadRefreshes[inst.InstanceID] || !threadNeedsRefresh(inst.Threads[threadID]) {
		return nil
	}
	s.threadRefreshes[inst.InstanceID] = true
	return []control.UIEvent{{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandThreadsRefresh,
			Origin: agentproto.Origin{
				Surface: surface.SurfaceSessionID,
				UserID:  surface.ActorUserID,
				ChatID:  surface.ChatID,
			},
		},
	}}
}

func threadNeedsRefresh(thread *state.ThreadRecord) bool {
	if thread == nil || !threadVisible(thread) {
		return false
	}
	return !thread.Loaded || (strings.TrimSpace(thread.Name) == "" && strings.TrimSpace(thread.Preview) == "")
}

func threadSelectionSubtitle(thread *state.ThreadRecord, threadID string) string {
	if thread != nil && thread.CWD != "" {
		return thread.CWD
	}
	if short := shortenThreadID(threadID); short != "" {
		return "会话 ID " + short
	}
	return ""
}

func headlessPendingNoticeCode(pending *state.HeadlessLaunchRecord) string {
	if pending != nil && pending.Status == state.HeadlessLaunchSelecting {
		return "headless_selection_waiting"
	}
	return "headless_starting"
}

func headlessPendingNoticeText(pending *state.HeadlessLaunchRecord) string {
	if pending != nil && pending.Status == state.HeadlessLaunchSelecting {
		return "请先选择一个要恢复的会话，或执行 /killinstance 取消。"
	}
	return "headless 实例仍在创建中，请等待完成或执行 /killinstance 取消。"
}

func selectionPromptExpired(now time.Time, prompt *state.SelectionPromptRecord) bool {
	if prompt == nil || prompt.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(prompt.ExpiresAt)
}

func isInternalHelperEvent(event agentproto.Event) bool {
	return event.TrafficClass == agentproto.TrafficClassInternalHelper || event.Initiator.Kind == agentproto.InitiatorInternalHelper
}

func threadVisible(thread *state.ThreadRecord) bool {
	return thread != nil && !thread.Archived && thread.TrafficClass != agentproto.TrafficClassInternalHelper
}

func visibleThreads(inst *state.InstanceRecord) []*state.ThreadRecord {
	if inst == nil {
		return nil
	}
	threads := make([]*state.ThreadRecord, 0, len(inst.Threads))
	for _, thread := range inst.Threads {
		if threadVisible(thread) {
			threads = append(threads, thread)
		}
	}
	sortVisibleThreads(threads)
	return threads
}

func sortVisibleThreads(threads []*state.ThreadRecord) {
	sort.SliceStable(threads, func(i, j int) bool {
		left := threads[i]
		right := threads[j]
		switch {
		case left == nil:
			return false
		case right == nil:
			return true
		case !left.LastUsedAt.Equal(right.LastUsedAt):
			return left.LastUsedAt.After(right.LastUsedAt)
		case left.ListOrder == 0 && right.ListOrder != 0:
			return false
		case left.ListOrder != 0 && right.ListOrder == 0:
			return true
		case left.ListOrder != right.ListOrder:
			return left.ListOrder < right.ListOrder
		default:
			return left.ThreadID < right.ThreadID
		}
	})
}

func shortenThreadID(threadID string) string {
	parts := strings.Split(threadID, "-")
	if len(parts) >= 2 {
		head := strings.TrimSpace(parts[1])
		tail := strings.TrimSpace(parts[len(parts)-1])
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		switch {
		case head == "":
		case tail == "":
			return head
		case head == tail:
			return head
		default:
			return head + "…" + tail
		}
	}
	if len(threadID) <= 10 {
		return threadID
	}
	return threadID[len(threadID)-8:]
}

func previewSnippet(text string) string {
	text = previewOfText(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40]) + "..."
	}
	return text
}

func isClearCommand(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "clear", "reset":
		return true
	default:
		return false
	}
}

func looksLikeReasoningEffort(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func formatOverrideNotice(summary control.PromptRouteSummary, prefix string) string {
	lines := []string{prefix}
	lines = append(lines, fmt.Sprintf("当前生效模型：%s", displayConfigValue(summary.EffectiveModel, summary.EffectiveModelSource)))
	lines = append(lines, fmt.Sprintf("当前推理强度：%s", displayConfigValue(summary.EffectiveReasoningEffort, summary.EffectiveReasoningEffortSource)))
	lines = append(lines, fmt.Sprintf("当前执行权限：%s", agentproto.DisplayAccessModeShort(summary.EffectiveAccessMode)))
	if summary.ThreadTitle != "" {
		lines = append(lines, fmt.Sprintf("当前输入目标：%s", summary.ThreadTitle))
	} else if summary.CreateThread {
		lines = append(lines, "当前输入目标：新建会话")
	}
	lines = append(lines, "说明：仅对之后从飞书发出的消息生效，不会同步 VS Code。")
	return strings.Join(lines, "\n")
}

func displayConfigValue(value, source string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
}

func configSourceLabel(value string) string {
	switch value {
	case "thread":
		return "会话配置"
	case "cwd_default":
		return "工作目录默认配置"
	case "surface_override":
		return "飞书临时覆盖"
	case "surface_default":
		return "飞书默认"
	default:
		return "未知"
	}
}

func previewOfText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return text
}

func turnRenderKey(instanceID, threadID, turnID string) string {
	return instanceID + "\x00" + threadID + "\x00" + turnID
}

func threadStateForInstance(inst *state.InstanceRecord) string {
	if !inst.Online {
		return "offline"
	}
	if inst.ActiveTurnID != "" {
		return "running"
	}
	return "idle"
}

func itemBufferKey(instanceID, threadID, turnID, itemID string) string {
	return strings.Join([]string{instanceID, threadID, turnID, itemID}, "::")
}

func (s *Service) ensureItemBuffer(instanceID, threadID, turnID, itemID, itemKind string) *itemBuffer {
	key := itemBufferKey(instanceID, threadID, turnID, itemID)
	if existing := s.itemBuffers[key]; existing != nil {
		if existing.ItemKind == "" {
			existing.ItemKind = itemKind
		}
		return existing
	}
	buf := &itemBuffer{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
		ItemID:     itemID,
		ItemKind:   itemKind,
	}
	s.itemBuffers[key] = buf
	return buf
}

func deleteMatchingItemBuffers(buffers map[string]*itemBuffer, instanceID, threadID, turnID string) {
	for key, buf := range buffers {
		if buf == nil {
			continue
		}
		if buf.InstanceID != instanceID {
			continue
		}
		if threadID != "" && buf.ThreadID != threadID {
			continue
		}
		if turnID != "" && buf.TurnID != turnID {
			continue
		}
		delete(buffers, key)
	}
}

func tracksTextItem(itemKind string) bool {
	switch itemKind {
	case "agent_message", "plan", "reasoning", "reasoning_summary", "reasoning_content", "command_execution_output", "file_change_output":
		return true
	default:
		return false
	}
}

func rendersTextItem(itemKind string) bool {
	switch itemKind {
	case "agent_message", "plan":
		return true
	default:
		return false
	}
}
