package orchestrator

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type HeadlessRestoreAttempt struct {
	ThreadID    string
	ThreadTitle string
	ThreadCWD   string
}

type SurfaceResumeAttempt struct {
	InstanceID   string
	ThreadID     string
	WorkspaceKey string
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

type attachWorkspaceMode string

const (
	attachWorkspaceModeDefault               attachWorkspaceMode = "default"
	attachWorkspaceModeSurfaceResume         attachWorkspaceMode = "surface_resume"
	attachWorkspaceModeTargetPickerNewThread attachWorkspaceMode = "target_picker_new_thread"
)

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
		Verbosity:        state.SurfaceVerbosityNormal,
		RouteMode:        state.RouteModeUnbound,
		DispatchMode:     state.DispatchModeNormal,
		LastInboundAt:    s.now(),
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
	}
	s.root.Surfaces[action.SurfaceSessionID] = surface
	return surface
}

func (s *Service) pendingHeadlessActionBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	if s.targetPickerHasBlockingProcessing(surface) {
		return nil
	}
	switch action.Kind {
	case control.ActionStatus,
		control.ActionAutoContinueCommand,
		control.ActionModeCommand,
		control.ActionDebugCommand,
		control.ActionUpgradeCommand,
		control.ActionDetach,
		control.ActionKillInstance,
		control.ActionRemovedCommand,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return nil
	default:
		return notice(surface, headlessPendingNoticeCode(surface.PendingHeadless), headlessPendingNoticeText(surface.PendingHeadless))
	}
}

func (s *Service) expirePendingHeadless(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || pending == nil {
		return nil
	}
	surface.PendingHeadless = nil
	events := []control.UIEvent{}
	if surface.AttachedInstanceID == pending.InstanceID {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
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

func (s *Service) handleRemovedCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	command := control.LegacyActionCommand(action.Text)
	switch control.LegacyActionKey(action.Text) {
	case "newinstance":
		return notice(surface, "command_removed_newinstance", "`/newinstance` 已移除。请改用 `/use` 或 `/useall` 选择要恢复的会话；在默认 normal 模式下，系统会自动复用在线工作区，必要时在后台恢复。")
	case "resume_headless_thread":
		return notice(surface, "selection_expired", "这个旧恢复卡片（来自已移除的 `/newinstance` 流程）已失效，请改用 `/use` 或 `/useall` 选择要恢复的会话；在默认 normal 模式下，系统会自动复用在线工作区，必要时在后台恢复。")
	default:
		if command == "" {
			return notice(surface, "command_removed", "这个旧命令已移除。请发送 `/help` 查看当前可用命令。")
		}
		return notice(surface, "command_removed", fmt.Sprintf("旧命令 `%s` 已移除。请发送 `/help` 查看当前可用命令。", command))
	}
}
