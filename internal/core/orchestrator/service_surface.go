package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type HeadlessRestoreAttempt struct {
	ThreadID    string
	ThreadTitle string
	ThreadCWD   string
}

type SurfaceResumeAttempt struct {
	InstanceID       string
	ThreadID         string
	WorkspaceKey     string
	Backend          agentproto.Backend
	PrepareNewThread bool
}

type HeadlessRestoreStatus string

const (
	HeadlessRestoreStatusSkipped  HeadlessRestoreStatus = "skipped"
	HeadlessRestoreStatusWaiting  HeadlessRestoreStatus = "waiting"
	HeadlessRestoreStatusAttached HeadlessRestoreStatus = "attached"
	HeadlessRestoreStatusStarting HeadlessRestoreStatus = "starting"
	HeadlessRestoreStatusFailed   HeadlessRestoreStatus = "failed"
)

type HeadlessRestoreResult struct {
	Status      HeadlessRestoreStatus
	FailureCode string
}

type SurfaceResumeStatus string

const (
	SurfaceResumeStatusSkipped           SurfaceResumeStatus = "skipped"
	SurfaceResumeStatusWaiting           SurfaceResumeStatus = "waiting"
	SurfaceResumeStatusStarting          SurfaceResumeStatus = "starting"
	SurfaceResumeStatusInstanceAttached  SurfaceResumeStatus = "instance_attached"
	SurfaceResumeStatusThreadAttached    SurfaceResumeStatus = "thread_attached"
	SurfaceResumeStatusWorkspaceAttached SurfaceResumeStatus = "workspace_attached"
	SurfaceResumeStatusFailed            SurfaceResumeStatus = "failed"
)

type SurfaceResumeResult struct {
	Status      SurfaceResumeStatus
	FailureCode string
}

type attachSurfaceToKnownThreadMode string

const (
	attachSurfaceToKnownThreadDefault         attachSurfaceToKnownThreadMode = "default"
	attachSurfaceToKnownThreadHeadlessRestore attachSurfaceToKnownThreadMode = "headless_restore"
	attachSurfaceToKnownThreadSurfaceResume   attachSurfaceToKnownThreadMode = "surface_resume"
)

type startHeadlessMode string

const (
	startHeadlessModeDefault         startHeadlessMode = "default"
	startHeadlessModeHeadlessRestore startHeadlessMode = "headless_restore"
)

type attachWorkspaceOptions struct {
	ResumeNotice     bool
	PrepareNewThread bool
}

type attachInstanceMode string

const (
	attachInstanceModeDefault       attachInstanceMode = "default"
	attachInstanceModeSurfaceResume attachInstanceMode = "surface_resume"
)

type threadSelectionDisplayMode string

const (
	threadSelectionDisplayRecent      threadSelectionDisplayMode = "recent"
	threadSelectionDisplayAll         threadSelectionDisplayMode = "all"
	threadSelectionDisplayAllExpanded threadSelectionDisplayMode = "all_expanded"
	threadSelectionDisplayScopedAll   threadSelectionDisplayMode = "scoped_all"
)

func (s *Service) ensureSurface(action control.Action) *state.SurfaceConsoleRecord {
	surface := s.root.Surfaces[action.SurfaceSessionID]
	if surface != nil {
		if action.GatewayID != "" {
			surface.GatewayID = action.GatewayID
		}
		if action.ChatID != "" {
			surface.ChatID = action.ChatID
		}
		if action.ActorUserID != "" {
			surface.ActorUserID = action.ActorUserID
		}
		if surface.PendingRequests == nil {
			surface.PendingRequests = map[string]*state.RequestPromptRecord{}
		}
		if surface.SurfaceMessages == nil {
			surface.SurfaceMessages = map[string]*state.SurfaceMessageRecord{}
		}
		s.normalizeSurfaceProductMode(surface)
		s.surfaceCurrentWorkspaceKey(surface)
		surface.LastInboundAt = s.now()
		return surface
	}

	surface = &state.SurfaceConsoleRecord{
		SurfaceSessionID: action.SurfaceSessionID,
		Platform:         "feishu",
		GatewayID:        action.GatewayID,
		ChatID:           action.ChatID,
		ActorUserID:      action.ActorUserID,
		ProductMode:      state.ProductModeNormal,
		Backend:          agentproto.BackendCodex,
		Verbosity:        state.SurfaceVerbosityNormal,
		RouteMode:        state.RouteModeUnbound,
		DispatchMode:     state.DispatchModeNormal,
		LastInboundAt:    s.now(),
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		StagedFiles:      map[string]*state.StagedFileRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
		SurfaceMessages:  map[string]*state.SurfaceMessageRecord{},
	}
	s.root.Surfaces[action.SurfaceSessionID] = surface
	return surface
}

func (s *Service) pendingHeadlessActionBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	if s.targetPickerHasBlockingProcessing(surface) {
		return nil
	}
	switch action.Kind {
	case control.ActionStatus,
		control.ActionAutoWhipCommand,
		control.ActionAutoContinueCommand,
		control.ActionPlanCommand,
		control.ActionModeCommand,
		control.ActionDebugCommand,
		control.ActionUpgradeCommand,
		control.ActionVSCodeMigrateCommand,
		control.ActionVSCodeMigrate,
		control.ActionDetach,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return nil
	default:
		return notice(surface, headlessPendingNoticeCode(surface.PendingHeadless), headlessPendingNoticeText(surface.PendingHeadless))
	}
}

func (s *Service) expirePendingHeadless(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) []eventcontract.Event {
	if surface == nil || pending == nil {
		return nil
	}
	surface.PendingHeadless = nil
	events := []eventcontract.Event{}
	if surface.AttachedInstanceID == pending.InstanceID {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindDaemonCommand,
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
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           pendingHeadlessTimeoutNotice(pending),
	})
	return s.maybeFinalizePendingTargetPicker(surface, events, pendingHeadlessTimeoutNotice(pending).Text)
}

func pendingHeadlessTimeoutNotice(pending *state.HeadlessLaunchRecord) *control.Notice {
	if pending != nil && pending.AutoRestore {
		return &control.Notice{
			Code:  "headless_restore_start_timeout",
			Title: "恢复失败",
			Text:  "之前的会话恢复超时，请稍后重试或尝试其他会话。",
		}
	}
	return &control.Notice{
		Code:  "headless_start_timeout",
		Title: "恢复超时",
		Text:  "后台恢复启动超时，已自动取消，请重新发送 /use 或 /useall 选择要恢复的会话。",
	}
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
