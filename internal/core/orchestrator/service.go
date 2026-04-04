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
	TurnHandoffWait time.Duration
}

type Service struct {
	now             func() time.Time
	config          Config
	root            *state.Root
	renderer        *renderer.Planner
	nextQueueItemID int
	nextImageID     int
	nextPromptID    int
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

func NewService(now func() time.Time, cfg Config, planner *renderer.Planner) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TurnHandoffWait <= 0 {
		cfg.TurnHandoffWait = 800 * time.Millisecond
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
	case control.ActionAttachInstance:
		return s.attachInstance(surface, action.InstanceID)
	case control.ActionModelCommand:
		return s.handleModelCommand(surface, action)
	case control.ActionReasoningCommand:
		return s.handleReasoningCommand(surface, action)
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
		return s.cancelPending(surface, action.TargetMessageID)
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
		return append(preface, s.threadFocusEvents(instanceID, "")...)
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
	case agentproto.EventRequestStarted, agentproto.EventRequestResolved:
		return preface
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

func (s *Service) attachInstance(surface *state.SurfaceConsoleRecord, instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return notice(surface, "instance_not_found", "实例不存在。")
	}

	events := s.discardDrafts(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.AttachedInstanceID = instanceID
	surface.SelectionPrompt = nil
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
		surface.PromptOverride = state.ModelConfigRecord{}
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
		if surface.PromptOverride.Model == "" {
			surface.PromptOverride = state.ModelConfigRecord{}
		}
		return notice(surface, "surface_override_reasoning_cleared", "已清除飞书临时推理强度覆盖。")
	}
	if len(parts) != 2 || !looksLikeReasoningEffort(parts[1]) {
		return notice(surface, "surface_override_usage", "用法：`/reasoning` 查看当前配置；`/reasoning <推理强度>`；`/reasoning clear`。")
	}
	surface.PromptOverride.ReasoningEffort = strings.ToLower(parts[1])
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时推理强度覆盖。"))
}

func (s *Service) handleText(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	text := strings.TrimSpace(action.Text)
	if text == "" {
		return nil
	}

	if selectionPromptExpired(s.now(), surface.SelectionPrompt) {
		surface.SelectionPrompt = nil
		if isDigits(text) {
			return notice(surface, "selection_expired", "之前的序号选择已过期，请重新发送 /list、/use 或 /useall。")
		}
	}

	if surface.SelectionPrompt != nil && isDigits(text) {
		return s.resolveSelection(surface, text)
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}

	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	inputs := s.consumeStagedInputs(surface)
	inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: text})

	s.nextQueueItemID++
	itemID := fmt.Sprintf("queue-%d", s.nextQueueItemID)
	item := &state.QueueItemRecord{
		ID:                 itemID,
		SurfaceSessionID:   surface.SurfaceSessionID,
		SourceMessageID:    action.MessageID,
		Inputs:             inputs,
		FrozenThreadID:     threadID,
		FrozenCWD:          cwd,
		FrozenOverride:     surface.PromptOverride,
		RouteModeAtEnqueue: routeMode,
		Status:             state.QueueItemQueued,
	}
	surface.QueueItems[item.ID] = item
	surface.QueuedQueueItemIDs = append(surface.QueuedQueueItemIDs, item.ID)

	events := []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     item.ID,
			SourceMessageID: item.SourceMessageID,
			Status:          string(item.Status),
			QueuePosition:   len(surface.QueuedQueueItemIDs),
		},
	}}

	if createThread {
		_ = createThread
	}
	events = append(events, s.dispatchNext(surface)...)
	return events
}

func (s *Service) stageImage(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
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
		},
	}}
}

func (s *Service) cancelPending(surface *state.SurfaceConsoleRecord, targetMessageID string) []control.UIEvent {
	for _, image := range surface.StagedImages {
		if image.SourceMessageID == targetMessageID && image.State == state.ImageStaged {
			image.State = state.ImageCancelled
			return []control.UIEvent{{
				Kind:             control.UIEventPendingInput,
				SurfaceSessionID: surface.SurfaceSessionID,
				PendingInput: &control.PendingInputState{
					QueueItemID:     image.ImageID,
					SourceMessageID: image.SourceMessageID,
					Status:          string(image.State),
					ThumbsDown:      true,
				},
			}}
		}
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item != nil && item.SourceMessageID == targetMessageID && item.Status == state.QueueItemQueued {
			item.Status = state.QueueItemDiscarded
			surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
			return []control.UIEvent{{
				Kind:             control.UIEventPendingInput,
				SurfaceSessionID: surface.SurfaceSessionID,
				PendingInput: &control.PendingInputState{
					QueueItemID:     item.ID,
					SourceMessageID: item.SourceMessageID,
					Status:          string(item.Status),
					ThumbsDown:      true,
				},
			}}
		}
	}
	return nil
}

func (s *Service) stopSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	inst := s.root.Instances[surface.AttachedInstanceID]
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
	}

	events = append(events, s.discardDrafts(surface)...)
	surface.QueuedQueueItemIDs = nil
	surface.SelectionPrompt = nil
	s.clearRemoteOwnership(surface)
	return events
}

func (s *Service) detach(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	events := s.discardDrafts(surface)
	s.clearRemoteOwnership(surface)
	surface.AttachedInstanceID = ""
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.DispatchMode = state.DispatchModeNormal
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.SelectionPrompt = nil
	surface.ActiveQueueItemID = ""
	surface.LastSelection = nil
	return append(events, notice(surface, "detached", "已断开当前实例接管。")...)
}

func (s *Service) resolveSelection(surface *state.SurfaceConsoleRecord, text string) []control.UIEvent {
	index, _ := strconv.Atoi(text)
	if surface.SelectionPrompt == nil {
		return nil
	}
	if selectionPromptExpired(s.now(), surface.SelectionPrompt) {
		surface.SelectionPrompt = nil
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
		}
	}
	return notice(surface, "selection_invalid", "无效的序号。")
}

func (s *Service) consumeStagedInputs(surface *state.SurfaceConsoleRecord) []agentproto.Input {
	keys := make([]string, 0, len(surface.StagedImages))
	for imageID := range surface.StagedImages {
		keys = append(keys, imageID)
	}
	sort.Strings(keys)

	var inputs []agentproto.Input
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
	}
	return inputs
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
		},
	}

	return []control.UIEvent{
		{
			Kind:             control.UIEventPendingInput,
			SurfaceSessionID: surface.SurfaceSessionID,
			PendingInput: &control.PendingInputState{
				QueueItemID:     item.ID,
				SourceMessageID: item.SourceMessageID,
				Status:          string(item.Status),
				TypingOn:        true,
			},
		},
		{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command:          command,
		},
	}
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

func (s *Service) clearTurnArtifacts(instanceID, threadID, turnID string) {
	deleteMatchingItemBuffers(s.itemBuffers, instanceID, threadID, turnID)
	if turnID == "" {
		return
	}
	delete(s.pendingTurnText, turnRenderKey(instanceID, threadID, turnID))
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
	if override.Model == "" && override.ReasoningEffort == "" {
		override = surface.PromptOverride
	}
	threadTitle := ""
	if threadID != "" {
		threadTitle = displayThreadTitle(inst, inst.Threads[threadID], threadID)
	}
	baseModel, baseEffort := resolveBasePromptConfig(inst, threadID, cwd)
	effectiveModel := baseModel
	effectiveEffort := baseEffort
	if override.Model != "" {
		effectiveModel = configValue{Value: override.Model, Source: "surface_override"}
	}
	if override.ReasoningEffort != "" {
		effectiveEffort = configValue{Value: override.ReasoningEffort, Source: "surface_override"}
	}
	return control.PromptRouteSummary{
		RouteMode:                      string(routeMode),
		ThreadID:                       threadID,
		ThreadTitle:                    threadTitle,
		CWD:                            cwd,
		CreateThread:                   createThread,
		BaseModel:                      baseModel.Value,
		BaseReasoningEffort:            baseEffort.Value,
		BaseModelSource:                baseModel.Source,
		BaseReasoningEffortSource:      baseEffort.Source,
		OverrideModel:                  override.Model,
		OverrideReasoningEffort:        override.ReasoningEffort,
		EffectiveModel:                 effectiveModel.Value,
		EffectiveReasoningEffort:       effectiveEffort.Value,
		EffectiveModelSource:           effectiveModel.Source,
		EffectiveReasoningEffortSource: effectiveEffort.Source,
	}
}

type configValue struct {
	Value  string
	Source string
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

func (s *Service) failSurfaceActiveQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, noticeCode, noticeText string, tryDispatchNext bool) []control.UIEvent {
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
	if noticeText != "" {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: noticeCode,
				Text: noticeText,
			},
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
	noticeText := ""
	if err != nil {
		noticeText = fmt.Sprintf("消息未成功发送到本地 Codex：%v", err)
	}
	return s.failSurfaceActiveQueueItem(surface, item, "dispatch_failed", noticeText, true)
}

func (s *Service) HandleCommandRejected(instanceID, commandID, errorMessage string) []control.UIEvent {
	if commandID == "" {
		return nil
	}
	binding := s.pendingRemote[instanceID]
	if binding == nil || binding.CommandID != commandID {
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
	text := "本地 Codex 拒绝了这条消息。"
	if strings.TrimSpace(errorMessage) != "" {
		text = fmt.Sprintf("本地 Codex 拒绝了这条消息：%s", strings.TrimSpace(errorMessage))
	}
	return s.failSurfaceActiveQueueItem(surface, item, "command_rejected", text, true)
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

		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil && (item.Status == state.QueueItemDispatching || item.Status == state.QueueItemRunning) {
				events = append(events, s.failSurfaceActiveQueueItem(surface, item, "attached_instance_offline", fmt.Sprintf("当前接管实例已离线：%s", inst.DisplayName), false)...)
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
		events = append(events, control.UIEvent{
			Kind:             control.UIEventPendingInput,
			SurfaceSessionID: surface.SurfaceSessionID,
			PendingInput: &control.PendingInputState{
				QueueItemID:     image.ImageID,
				SourceMessageID: image.SourceMessageID,
				Status:          string(image.State),
				ThumbsDown:      true,
			},
		})
	}
	for _, queueID := range append([]string{}, surface.QueuedQueueItemIDs...) {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued {
			continue
		}
		item.Status = state.QueueItemDiscarded
		events = append(events, control.UIEvent{
			Kind:             control.UIEventPendingInput,
			SurfaceSessionID: surface.SurfaceSessionID,
			PendingInput: &control.PendingInputState{
				QueueItemID:     item.ID,
				SourceMessageID: item.SourceMessageID,
				Status:          string(item.Status),
				ThumbsDown:      true,
			},
		})
	}
	surface.QueuedQueueItemIDs = nil
	surface.QueueItems = map[string]*state.QueueItemRecord{}
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

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
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
	lines = append(lines, fmt.Sprintf("当前生效模型：%s", displayConfigValue(summary.EffectiveModel)))
	lines = append(lines, fmt.Sprintf("当前推理强度：%s", displayConfigValue(summary.EffectiveReasoningEffort)))
	if summary.ThreadTitle != "" {
		lines = append(lines, fmt.Sprintf("当前输入目标：%s", summary.ThreadTitle))
	} else if summary.CreateThread {
		lines = append(lines, "当前输入目标：新建会话")
	}
	lines = append(lines, "说明：仅对之后从飞书发出的消息生效，不会同步 VS Code。")
	return strings.Join(lines, "\n")
}

func displayConfigValue(value string) string {
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
