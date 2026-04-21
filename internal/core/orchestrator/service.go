package orchestrator

import (
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
	LocalPauseMaxWait  time.Duration
	DetachAbandonWait  time.Duration
	GitAvailable       bool
}

type Service struct {
	now                  func() time.Time
	config               Config
	root                 *state.Root
	renderer             *renderer.Planner
	nextQueueItemID      int
	nextImageID          int
	nextFileID           int
	nextPromptID         int
	nextRequestCommandID int
	nextLocalRequestID   int
	nextHeadlessID       int
	handoffUntil         map[string]time.Time
	pausedUntil          map[string]time.Time
	abandoningUntil      map[string]time.Time
	itemBuffers          map[string]*itemBuffer
	threadRefreshes      map[string]bool
	instanceClaims       map[string]*instanceClaimRecord
	workspaceClaims      map[string]*workspaceClaimRecord
	threadClaims         map[string]*threadClaimRecord
	surfaceUIRuntime     map[string]*surfaceUIRuntimeRecord
	turns                *serviceTurnRuntime
	pickers              *servicePickerRuntime
	catalog              *serviceCatalogRuntime
	progress             *serviceProgressRuntime
}

type itemBuffer struct {
	InstanceID string
	ThreadID   string
	TurnID     string
	ItemID     string
	ItemKind   string
	textChunks []string
	textValue  string
}

type turnPlanSnapshotRecord struct {
	SurfaceSessionID string
	InstanceID       string
	ThreadID         string
	TurnID           string
	Snapshot         *agentproto.TurnPlanSnapshot
}

type remoteTurnBinding struct {
	InstanceID            string
	SurfaceSessionID      string
	QueueItemID           string
	SourceMessageID       string
	SourceMessagePreview  string
	ReplyToMessageID      string
	ReplyToMessagePreview string
	CommandID             string
	ThreadID              string
	ThreadCWD             string
	TurnID                string
	Status                string
	StartedAt             time.Time
	StartTotalUsage       agentproto.TokenUsageBreakdown
	HasStartTotalUsage    bool
	LastUsage             agentproto.TokenUsageBreakdown
	HasLastUsage          bool
	ModelReroute          *agentproto.TurnModelReroute
}

type compactTurnStatus string

const (
	compactTurnStatusDispatching compactTurnStatus = "dispatching"
	compactTurnStatusRunning     compactTurnStatus = "running"
)

type compactTurnBinding struct {
	InstanceID       string
	SurfaceSessionID string
	FlowID           string
	ThreadID         string
	CommandID        string
	TurnID           string
	Status           compactTurnStatus
	CompletionSeen   bool
}

type pendingSteerBinding struct {
	InstanceID         string
	SurfaceSessionID   string
	QueueItemID        string
	QueueItemIDs       []string
	QueueIndices       map[string]int
	SourceMessageID    string
	OwnerCardMessageID string
	CommandID          string
	ThreadID           string
	TurnID             string
	QueueIndex         int
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

type workspaceClaimRecord struct {
	WorkspaceKey     string
	SurfaceSessionID string
}

type threadClaimRecord struct {
	ThreadID         string
	InstanceID       string
	SurfaceSessionID string
}

type PersistedThreadCatalog interface {
	RecentThreads(limit int) ([]state.ThreadRecord, error)
	RecentWorkspaces(limit int) (map[string]time.Time, error)
	ThreadByID(threadID string) (*state.ThreadRecord, error)
}

type PathPickerConsumer interface {
	PathPickerConfirmed(*Service, *state.SurfaceConsoleRecord, control.PathPickerResult) []control.UIEvent
	PathPickerCancelled(*Service, *state.SurfaceConsoleRecord, control.PathPickerResult) []control.UIEvent
}

type PathPickerEntryFilter interface {
	PathPickerFilterEntry(*Service, *state.SurfaceConsoleRecord, *activePathPickerRecord, control.FeishuPathPickerEntry, string) (control.FeishuPathPickerEntry, bool)
}

type PathPickerConfirmLifecycleOwner interface {
	PathPickerOwnsConfirmLifecycle() bool
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
	svc := &Service{
		now:              now,
		config:           cfg,
		root:             state.NewRoot(),
		renderer:         planner,
		handoffUntil:     map[string]time.Time{},
		pausedUntil:      map[string]time.Time{},
		abandoningUntil:  map[string]time.Time{},
		itemBuffers:      map[string]*itemBuffer{},
		threadRefreshes:  map[string]bool{},
		instanceClaims:   map[string]*instanceClaimRecord{},
		workspaceClaims:  map[string]*workspaceClaimRecord{},
		threadClaims:     map[string]*threadClaimRecord{},
		surfaceUIRuntime: map[string]*surfaceUIRuntimeRecord{},
	}
	svc.turns = newServiceTurnRuntime(svc)
	svc.pickers = newServicePickerRuntime(svc)
	svc.catalog = newServiceCatalogRuntime(svc)
	svc.progress = newServiceProgressRuntime(svc)
	svc.RegisterPathPickerConsumer(targetPickerWorkspaceCreatePathPickerConsumerKind, targetPickerWorkspaceCreatePathPickerConsumer{})
	svc.RegisterPathPickerConsumer(targetPickerAddWorkspacePathPickerConsumerKind, targetPickerAddWorkspacePathPickerConsumer{})
	svc.RegisterPathPickerConsumer(sendFilePathPickerConsumerKind, sendFilePathPickerConsumer{})
	svc.RegisterPathPickerEntryFilter(targetPickerBusyWorkspacePathPickerEntryFilterKind, targetPickerBusyWorkspacePathPickerEntryFilter{})
	return svc
}

func (s *Service) normalizeSurfaceProductMode(surface *state.SurfaceConsoleRecord) state.ProductMode {
	if surface == nil {
		return state.ProductModeNormal
	}
	surface.ProductMode = state.NormalizeProductMode(surface.ProductMode)
	surface.Verbosity = state.NormalizeSurfaceVerbosity(surface.Verbosity)
	surface.PlanMode = state.NormalizePlanModeSetting(surface.PlanMode)
	s.normalizeLegacyNormalFollowRoute(surface)
	s.normalizeLegacyVSCodePreparedNewThread(surface)
	return surface.ProductMode
}

func (s *Service) normalizeLegacyNormalFollowRoute(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.ProductMode != state.ProductModeNormal || surface.RouteMode != state.RouteModeFollowLocal {
		return
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	threadID := surface.SelectedThreadID
	if inst != nil && threadID != "" {
		if owner := s.threadClaimSurface(threadID); owner == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
			if !s.surfaceOwnsThread(surface, threadID) {
				s.claimKnownThread(surface, inst, threadID)
			}
			if s.surfaceOwnsThread(surface, threadID) {
				thread := s.ensureThread(inst, threadID)
				surface.RouteMode = state.RouteModePinned
				surface.LastSelection = &state.SelectionAnnouncementRecord{
					ThreadID:  threadID,
					RouteMode: string(state.RouteModePinned),
					Title:     displayThreadTitle(inst, thread, threadID),
					Preview:   threadPreview(thread),
				}
				return
			}
		}
	}
	s.releaseSurfaceThreadClaim(surface)
	surface.RouteMode = state.RouteModeUnbound
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(state.RouteModeUnbound),
		Title:     "未绑定会话",
		Preview:   "",
	}
}

func (s *Service) normalizeLegacyVSCodePreparedNewThread(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.ProductMode != state.ProductModeVSCode || surface.RouteMode != state.RouteModeNewThreadReady {
		return
	}
	if s.preparedNewThreadHasPendingCreate(surface) {
		return
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	s.clearPreparedNewThread(surface)
	s.releaseSurfaceThreadClaim(surface)
	surface.RouteMode = state.RouteModeFollowLocal

	if inst != nil {
		targetThreadID := strings.TrimSpace(inst.ObservedFocusedThreadID)
		if targetThreadID != "" && threadVisible(inst.Threads[targetThreadID]) {
			if owner := s.threadClaimSurface(targetThreadID); owner == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
				if !s.surfaceOwnsThread(surface, targetThreadID) {
					s.claimKnownThread(surface, inst, targetThreadID)
				}
				if s.surfaceOwnsThread(surface, targetThreadID) {
					thread := s.ensureThread(inst, targetThreadID)
					surface.SelectedThreadID = targetThreadID
					surface.LastSelection = &state.SelectionAnnouncementRecord{
						ThreadID:  targetThreadID,
						RouteMode: string(state.RouteModeFollowLocal),
						Title:     displayThreadTitle(inst, thread, targetThreadID),
						Preview:   threadPreview(thread),
					}
					return
				}
			}
		}
	}

	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(state.RouteModeFollowLocal),
		Title:     "跟随当前 VS Code（等待中）",
		Preview:   "",
	}
}

func (s *Service) UpsertInstance(inst *state.InstanceRecord) {
	if inst.Threads == nil {
		inst.Threads = map[string]*state.ThreadRecord{}
	}
	if inst.CWDDefaults == nil {
		inst.CWDDefaults = map[string]state.ModelConfigRecord{}
	}
	inst.WorkspaceRoot = state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
	inst.WorkspaceKey = state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot)
	if inst.ShortName == "" {
		inst.ShortName = state.WorkspaceShortName(inst.WorkspaceKey)
	}
	if inst.DisplayName == "" {
		inst.DisplayName = inst.ShortName
	}
	s.backfillLegacyWorkspaceDefaults(inst)
	s.root.Instances[inst.InstanceID] = inst
}

func (s *Service) SetPersistedThreadCatalog(catalog PersistedThreadCatalog) {
	if s == nil || s.catalog == nil {
		return
	}
	s.catalog.setPersistedThreadCatalog(catalog)
}

func (s *Service) ApplySurfaceAction(action control.Action) []control.UIEvent {
	surface := s.ensureSurface(action)
	s.pruneExpiredPathPicker(surface)
	if surface.Abandoning {
		switch action.Kind {
		case control.ActionStatus:
			return s.filterEventsForSurfaceVisibility([]control.UIEvent{{Kind: control.UIEventSnapshot, SurfaceSessionID: surface.SurfaceSessionID, Snapshot: s.buildSnapshot(surface)}})
		case control.ActionAutoContinueCommand:
			return s.filterEventsForSurfaceVisibility(s.handleAutoContinueCommand(surface, action))
		case control.ActionDetach:
			return s.filterEventsForSurfaceVisibility(notice(surface, "detach_pending", "当前仍在等待已发出的 turn 收尾，请稍后再试。"))
		default:
			return s.filterEventsForSurfaceVisibility(notice(surface, "detach_pending", "当前会话正在等待已发出的 turn 收尾，暂时不能执行新的操作。"))
		}
	}
	if blocked := s.pendingHeadlessActionBlocked(surface, action); blocked != nil {
		return s.filterEventsForSurfaceVisibility(blocked)
	}
	if blocked := s.blockActionForActivePathPicker(surface, action); blocked != nil {
		return s.filterEventsForSurfaceVisibility(blocked)
	}
	if blocked := s.blockActionForActiveTargetPicker(surface, action); blocked != nil {
		return s.filterEventsForSurfaceVisibility(blocked)
	}
	s.noteAutoContinueAction(surface, action)
	if intent, ok := control.FeishuUIIntentFromAction(action); ok {
		return s.filterEventsForSurfaceVisibility(s.applyFeishuUIIntent(surface, *intent))
	}
	var events []control.UIEvent
	switch action.Kind {
	case control.ActionListInstances:
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			events = s.openTargetPicker(surface, control.TargetPickerRequestSourceList, "", "", false)
			break
		}
		events = s.presentInstanceSelection(surface)
	case control.ActionNewThread:
		events = s.prepareNewThread(surface)
	case control.ActionCompact:
		events = s.handleCompactCommand(surface, action)
	case control.ActionSendFile:
		events = s.openSendFilePicker(surface)
	case control.ActionSteerAll:
		events = s.handleSteerAllCommand(surface, action)
	case control.ActionAttachInstance:
		events = s.attachInstance(surface, action.InstanceID)
	case control.ActionAttachWorkspace:
		events = s.attachWorkspace(surface, action.WorkspaceKey)
	case control.ActionTargetPickerConfirm:
		events = s.handleTargetPickerConfirm(surface, action.PickerID, action.ActorUserID, action.WorkspaceKey, action.TargetPickerValue, action.RequestAnswers)
	case control.ActionShowCommandHelp:
		events = []control.UIEvent{s.commandViewEvent(surface, s.buildCommandHelpView(surface))}
	case control.ActionShowHistory:
		events = s.openThreadHistory(surface, action.MessageID, action.IsCardAction())
	case control.ActionDebugCommand:
		events = []control.UIEvent{{
			Kind:             control.UIEventDaemonCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  action.MessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandDebug,
				GatewayID:        surface.GatewayID,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  action.MessageID,
				Text:             action.Text,
			},
		}}
	case control.ActionCronCommand:
		events = []control.UIEvent{{
			Kind:             control.UIEventDaemonCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  action.MessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandCron,
				GatewayID:        surface.GatewayID,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  action.MessageID,
				Text:             action.Text,
			},
		}}
	case control.ActionUpgradeCommand:
		events = []control.UIEvent{{
			Kind:             control.UIEventDaemonCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  action.MessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandUpgrade,
				GatewayID:        surface.GatewayID,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  action.MessageID,
				FromCardAction:   action.IsCardAction(),
				Text:             action.Text,
			},
		}}
	case control.ActionVSCodeMigrateCommand:
		events = []control.UIEvent{{
			Kind:             control.UIEventDaemonCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  action.MessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandVSCodeMigrateCommand,
				GatewayID:        surface.GatewayID,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  action.MessageID,
				FromCardAction:   action.IsCardAction(),
				Text:             action.Text,
			},
		}}
	case control.ActionUpgradeOwnerFlow:
		ownerFlow := action.OwnerFlow
		if ownerFlow == nil && (strings.TrimSpace(action.PickerID) != "" || strings.TrimSpace(action.OptionID) != "") {
			ownerFlow = &control.ActionOwnerCardFlow{FlowID: action.PickerID, OptionID: action.OptionID}
		}
		if ownerFlow == nil {
			return notice(surface, "owner_flow_invalid", "当前升级确认动作缺少有效的 owner-flow 上下文。")
		}
		events = []control.UIEvent{{
			Kind:             control.UIEventDaemonCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  action.MessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandUpgradeOwnerFlow,
				GatewayID:        surface.GatewayID,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  action.MessageID,
				FromCardAction:   action.IsCardAction(),
				PickerID:         ownerFlow.FlowID,
				OptionID:         ownerFlow.OptionID,
			},
		}}
	case control.ActionModelCommand:
		events = s.handleModelCommand(surface, action)
	case control.ActionReasoningCommand:
		events = s.handleReasoningCommand(surface, action)
	case control.ActionAccessCommand:
		events = s.handleAccessCommand(surface, action)
	case control.ActionPlanCommand:
		events = s.handlePlanCommand(surface, action)
	case control.ActionPlanProposalDecision:
		events = s.handlePlanProposalDecision(surface, action)
	case control.ActionVerboseCommand:
		events = s.handleVerboseCommand(surface, action)
	case control.ActionAutoContinueCommand:
		events = s.handleAutoContinueCommand(surface, action)
	case control.ActionModeCommand:
		events = s.handleModeCommand(surface, action)
	case control.ActionRespondRequest:
		events = s.respondRequest(surface, action)
	case control.ActionUseThread:
		events = s.useThread(surface, action.ThreadID, action.AllowCrossWorkspace)
	case control.ActionConfirmKickThread:
		events = s.confirmKickThread(surface, action.ThreadID)
	case control.ActionCancelKickThread:
		events = notice(surface, "kick_cancelled", "已取消强踢。")
	case control.ActionFollowLocal:
		events = s.followLocal(surface)
	case control.ActionVSCodeMigrate:
		ownerFlow := action.OwnerFlow
		if ownerFlow == nil && (strings.TrimSpace(action.PickerID) != "" || strings.TrimSpace(action.OptionID) != "") {
			ownerFlow = &control.ActionOwnerCardFlow{FlowID: action.PickerID, OptionID: action.OptionID}
		}
		if ownerFlow == nil {
			return notice(surface, "owner_flow_invalid", "当前 VS Code 迁移动作缺少有效的 owner-flow 上下文。")
		}
		events = []control.UIEvent{{
			Kind:             control.UIEventDaemonCommand,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			SourceMessageID:  action.MessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandVSCodeMigrate,
				GatewayID:        surface.GatewayID,
				SurfaceSessionID: surface.SurfaceSessionID,
				SourceMessageID:  action.MessageID,
				FromCardAction:   action.IsCardAction(),
				PickerID:         ownerFlow.FlowID,
				OptionID:         ownerFlow.OptionID,
			},
		}}
	case control.ActionTextMessage:
		events = s.handleText(surface, action)
	case control.ActionImageMessage:
		events = s.stageImage(surface, action)
	case control.ActionFileMessage:
		events = s.stageFile(surface, action)
	case control.ActionReactionCreated:
		events = s.handleReactionCreated(surface, action)
	case control.ActionMessageRecalled:
		events = s.handleMessageRecalled(surface, action.TargetMessageID)
	case control.ActionStop:
		events = s.stopSurface(surface)
	case control.ActionStatus:
		events = []control.UIEvent{{Kind: control.UIEventSnapshot, SurfaceSessionID: surface.SurfaceSessionID, Snapshot: s.buildSnapshot(surface)}}
	case control.ActionDetach:
		events = s.detach(surface)
	default:
		return nil
	}
	return s.filterEventsForSurfaceVisibility(events)
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
		return s.filterEventsForSurfaceVisibility(append(preface, s.threadFocusEvents(instanceID, event.ThreadID)...))
	case agentproto.EventConfigObserved:
		s.observeConfig(inst, event.ThreadID, event.CWD, event.ConfigScope, event.Model, event.ReasoningEffort, event.AccessMode, event.PlanMode)
		return s.filterEventsForSurfaceVisibility(preface)
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
			if strings.TrimSpace(thread.LastAssistantMessage) == "" {
				thread.LastAssistantMessage = previewOfText(event.Preview)
			}
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
		if event.PlanMode != "" {
			thread.ObservedPlanMode = state.NormalizePlanModeSetting(state.PlanModeSetting(event.PlanMode))
		}
		if event.RuntimeStatus != nil {
			applyThreadRuntimeStatus(thread, event.RuntimeStatus)
		} else {
			thread.Loaded = true
			if event.Status != "" {
				thread.State = event.Status
			}
		}
		s.touchThread(thread)
		return s.filterEventsForSurfaceVisibility(append(preface, s.threadFocusEvents(instanceID, event.ThreadID)...))
	case agentproto.EventThreadRuntimeStatusUpdated:
		thread := s.ensureThread(inst, event.ThreadID)
		if event.RuntimeStatus != nil {
			applyThreadRuntimeStatus(thread, event.RuntimeStatus)
		} else {
			thread.Loaded = event.Loaded
			if event.Status != "" {
				thread.State = event.Status
			}
		}
		if threadRuntimeActive(thread) {
			s.touchThread(thread)
		}
		return s.filterEventsForSurfaceVisibility(preface)
	case agentproto.EventThreadsSnapshot:
		delete(s.threadRefreshes, instanceID)
		nextThreads := map[string]*state.ThreadRecord{}
		for threadID, thread := range inst.Threads {
			if thread == nil {
				continue
			}
			copied := cloneThreadRecord(thread)
			markThreadNotLoaded(copied)
			nextThreads[threadID] = copied
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
				if strings.TrimSpace(current.LastAssistantMessage) == "" {
					current.LastAssistantMessage = previewOfText(thread.Preview)
				}
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
			if thread.PlanMode != "" {
				current.ObservedPlanMode = state.NormalizePlanModeSetting(state.PlanModeSetting(thread.PlanMode))
			}
			current.Loaded = thread.Loaded
			current.Archived = thread.Archived
			if thread.State != "" {
				current.State = thread.State
			}
			if thread.RuntimeStatus != nil {
				applyThreadRuntimeStatus(current, thread.RuntimeStatus)
			}
			current.ListOrder = thread.ListOrder
			nextThreads[thread.ThreadID] = current
		}
		inst.Threads = nextThreads
		events := append(preface, s.reconcileInstanceSurfaceThreads(instanceID)...)
		return s.filterEventsForSurfaceVisibility(append(events, s.threadFocusEvents(instanceID, "")...))
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
		return s.filterEventsForSurfaceVisibility(append(events, s.reevaluateFollowSurfaces(instanceID)...))
	case agentproto.EventThreadTokenUsageUpdated:
		return s.filterEventsForSurfaceVisibility(append(preface, s.progress.applyThreadTokenUsageUpdate(instanceID, event)...))
	case agentproto.EventTurnModelRerouted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		return s.filterEventsForSurfaceVisibility(append(preface, s.applyTurnModelReroute(instanceID, event)...))
	case agentproto.EventTurnDiffUpdated:
		s.progress.recordTurnDiffSnapshot(instanceID, event)
		return s.filterEventsForSurfaceVisibility(preface)
	case agentproto.EventTurnPlanUpdated:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		return s.filterEventsForSurfaceVisibility(append(preface, s.applyTurnPlanUpdate(instanceID, event)...))
	case agentproto.EventTurnStarted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		preface = append(preface, s.maybeSealPlanProposalForTurnStart(instanceID, event.ThreadID, event.TurnID)...)
		trackActiveTurn := s.shouldTrackInstanceActiveTurn(instanceID, event)
		if trackActiveTurn {
			inst.ActiveTurnID = event.TurnID
			inst.ActiveThreadID = event.ThreadID
		}
		if event.ThreadID != "" {
			s.touchThread(s.ensureThread(inst, event.ThreadID))
		}
		if trackActiveTurn {
			if surface := s.surfaceForInitiator(instanceID, event); surface != nil {
				surface.ActiveTurnOrigin = event.Initiator.Kind
			}
		}
		compactEvents := s.promoteCompactTurn(instanceID, event)
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
			events = append(events, compactEvents...)
			return s.filterEventsForSurfaceVisibility(append(events, s.reevaluateFollowSurfaces(instanceID)...))
		}
		events := append(preface, compactEvents...)
		events = append(events, s.markRemoteTurnRunning(instanceID, event.Initiator, event.ThreadID, event.TurnID)...)
		return s.filterEventsForSurfaceVisibility(events)
	case agentproto.EventTurnCompleted:
		event.Initiator = s.normalizeTurnInitiator(instanceID, event)
		clearTrackedTurn := shouldClearTrackedInstanceActiveTurn(inst, event.ThreadID, event.TurnID)
		if clearTrackedTurn {
			inst.ActiveTurnID = ""
		}
		s.clearRequestsForTurn(instanceID, event.ThreadID, event.TurnID)
		var thread *state.ThreadRecord
		if event.ThreadID != "" {
			if clearTrackedTurn || event.Initiator.Kind == agentproto.InitiatorLocalUI {
				inst.ActiveThreadID = event.ThreadID
			}
			thread = s.ensureThread(inst, event.ThreadID)
			s.touchThread(thread)
		}
		surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
		if clearTrackedTurn && surface != nil {
			surface.ActiveTurnOrigin = ""
		}
		deleteMatchingItemBuffers(s.itemBuffers, instanceID, event.ThreadID, event.TurnID)
		summary := s.progress.takeTurnFileChangeSummary(instanceID, event.ThreadID, event.TurnID)
		turnDiff := s.progress.takeTurnDiffSnapshot(instanceID, event.ThreadID, event.TurnID)
		finalText := pendingTurnTextValue(s.progress.pendingTurnText, instanceID, event.ThreadID, event.TurnID)
		finalizeTurnOutput := shouldFinalizeTurnOutput(event)
		finalRenderSummary := (*control.FileChangeSummary)(nil)
		finalRenderTurnDiff := (*control.TurnDiffSnapshot)(nil)
		finalRenderTurnSummary := (*control.FinalTurnSummary)(nil)
		if finalizeTurnOutput {
			finalRenderSummary = summary
			finalRenderTurnDiff = turnDiff
			finalRenderTurnSummary = finalTurnSummaryForBinding(s.now().UTC(), s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID), thread)
		}
		events := s.flushPendingTurnTextWithSummary(
			instanceID,
			event.ThreadID,
			event.TurnID,
			finalizeTurnOutput,
			finalRenderSummary,
			finalRenderTurnDiff,
			finalRenderTurnSummary,
		)
		events = append(events, s.finalizeExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID, event.Status, finalText)...)
		deleteMatchingTurnPlanSnapshots(s.progress.turnPlanSnapshots, instanceID, event.ThreadID, event.TurnID)
		deleteMatchingMCPToolCallProgress(s.progress.mcpToolCallProgress, instanceID, event.ThreadID, event.TurnID)
		compactEvents := s.completeCompactTurn(instanceID, event)
		if event.Initiator.Kind == agentproto.InitiatorLocalUI {
			events = append(events, s.enterHandoff(instanceID)...)
			events = append(events, s.maybePresentCompletedPlanProposal(instanceID, event.ThreadID, event.TurnID)...)
			events = append(events, compactEvents...)
			if surface != nil {
				events = append(events, s.finishSurfaceAfterWork(surface)...)
			}
			return s.filterEventsForSurfaceVisibility(events)
		}
		events = append(events, s.completeRemoteTurn(instanceID, event.ThreadID, event.TurnID, event.Status, event.ErrorMessage, event.Problem, finalText, summary)...)
		events = append(events, s.maybePresentCompletedPlanProposal(instanceID, event.ThreadID, event.TurnID)...)
		events = append(events, compactEvents...)
		return s.filterEventsForSurfaceVisibility(events)
	case agentproto.EventItemStarted:
		s.trackItemStart(instanceID, event)
		return s.filterEventsForSurfaceVisibility(append(preface, s.handleProcessProgressItemStarted(instanceID, event)...))
	case agentproto.EventItemDelta:
		s.trackItemDelta(instanceID, event)
		return s.filterEventsForSurfaceVisibility(append(preface, s.handleProcessProgressItemDelta(instanceID, event)...))
	case agentproto.EventItemCompleted:
		events := append(preface, s.handleProcessProgressItemCompleted(instanceID, event)...)
		events = append(events, s.completeItem(instanceID, event)...)
		return s.filterEventsForSurfaceVisibility(events)
	case agentproto.EventRequestStarted:
		return s.filterEventsForSurfaceVisibility(append(preface, s.presentRequestPrompt(instanceID, event)...))
	case agentproto.EventRequestResolved:
		return s.filterEventsForSurfaceVisibility(append(preface, s.resolveRequestPrompt(instanceID, event)...))
	case agentproto.EventSystemError:
		problem := problemFromEvent(event)
		events := append(preface, s.handleCompactProblem(instanceID, problem)...)
		events = append(events, s.handleProblem(instanceID, problem)...)
		return s.filterEventsForSurfaceVisibility(events)
	default:
		return s.filterEventsForSurfaceVisibility(preface)
	}
}

func shouldFinalizeTurnOutput(event agentproto.Event) bool {
	if event.Status != "completed" {
		return false
	}
	if strings.TrimSpace(event.ErrorMessage) != "" {
		return false
	}
	return event.Problem == nil
}

// Tick is the orchestrator's deadline driver.
// Keep it limited to in-memory expiry/backoff transitions that must still fire
// when no new ingress event arrives.
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
				Text: s.detachTimeoutText(surface),
			},
		})
	}
	for _, surface := range s.root.Surfaces {
		if pending := surface.PendingHeadless; pending != nil && !pending.ExpiresAt.IsZero() && !now.Before(pending.ExpiresAt) {
			events = append(events, s.expirePendingHeadless(surface, pending)...)
		}
		if requestCaptureExpired(now, surface.ActiveRequestCapture) {
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
		events = append(events, s.maybeDispatchPendingAutoContinue(surface, now)...)
		events = append(events, s.tickExecCommandProgressAnimations(surface, now)...)
	}
	return s.filterEventsForSurfaceVisibility(events)
}
