package orchestrator

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const targetPickerWorkspaceCreatePathPickerConsumerKind = "target_picker_workspace_create"

type targetPickerWorkspaceCreatePathPickerConsumer struct{}

func (targetPickerWorkspaceCreatePathPickerConsumer) PathPickerConfirmed(s *Service, surface *state.SurfaceConsoleRecord, result control.PathPickerResult) []eventcontract.Event {
	if s == nil || surface == nil {
		return nil
	}
	workspaceKey, err := state.ResolveWorkspaceRootOnHost(result.SelectedPath)
	if err != nil {
		return notice(surface, "workspace_create_invalid", fmt.Sprintf("目录路径无效：%v", err))
	}
	if workspaceKey == "" {
		return notice(surface, "workspace_create_invalid", "目录路径无效，请重新选择。")
	}
	events := s.enterTargetPickerNewThread(surface, workspaceKey)
	if targetPickerNewThreadReady(surface, workspaceKey) {
		s.clearTargetPickerRuntime(surface)
	}
	return events
}

func (targetPickerWorkspaceCreatePathPickerConsumer) PathPickerCancelled(_ *Service, surface *state.SurfaceConsoleRecord, _ control.PathPickerResult) []eventcontract.Event {
	return notice(surface, "workspace_create_cancelled", "已取消添加工作区。当前工作目标保持不变。")
}

func (s *Service) openTargetPickerWorkspaceCreatePicker(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.openWorkspaceCreatePicker(surface, targetPickerWorkspaceCreatePathPickerConsumerKind, "接入并准备新会话", "未确认前不会切换当前工作目标。")
}

func (s *Service) openWorkspaceCreatePicker(surface *state.SurfaceConsoleRecord, consumerKind, confirmLabel, hint string) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return notice(surface, "workspace_create_normal_only", "当前处于 vscode 模式，不能从目录直接添加工作区。请先 `/mode normal`。")
	}
	rootPath, initialPath := workspacePickerPaths(s.surfaceCurrentWorkspaceKey(surface))
	return s.openPathPicker(surface, surface.ActorUserID, control.PathPickerRequest{
		Mode:         control.PathPickerModeDirectory,
		Title:        "选择要接入的目录",
		RootPath:     rootPath,
		InitialPath:  initialPath,
		Hint:         strings.TrimSpace(hint),
		ConfirmLabel: strings.TrimSpace(firstNonEmpty(confirmLabel, "接入为工作区")),
		CancelLabel:  "取消",
		ConsumerKind: strings.TrimSpace(consumerKind),
	})
}

func workspacePickerPaths(initialPath string) (string, string) {
	return workspacePickerPathsForGOOS(runtime.GOOS, initialPath, windowsWorkspaceCreateFallbackPath())
}

func workspacePickerPathsForGOOS(goos, initialPath, windowsFallbackPath string) (string, string) {
	initialPath = strings.TrimSpace(initialPath)
	rootPath := workspaceCreatePickerRootForGOOSWithFallback(goos, initialPath, windowsFallbackPath)
	if initialPath == "" {
		initialPath = rootPath
	}
	return rootPath, initialPath
}

func workspaceCreatePickerRoot(initialPath string) string {
	return workspaceCreatePickerRootForGOOS(runtime.GOOS, initialPath)
}

func workspaceCreatePickerRootForGOOS(goos, initialPath string) string {
	return workspaceCreatePickerRootForGOOSWithFallback(goos, initialPath, windowsWorkspaceCreateFallbackPath())
}

func workspaceCreatePickerRootForGOOSWithFallback(goos, initialPath, windowsFallbackPath string) string {
	initialPath = strings.TrimSpace(initialPath)
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "windows":
		for _, candidate := range []string{initialPath, windowsFallbackPath} {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if volume := windowsVolumeRoot(candidate); volume != "" {
				return volume
			}
		}
	}
	return "/"
}

func windowsWorkspaceCreateFallbackPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(home)
}

func windowsVolumeRoot(path string) string {
	path = strings.TrimSpace(path)
	if !isWindowsVolumePath(path) {
		return ""
	}
	return path[:2] + "/"
}

func (s *Service) startFreshWorkspaceHeadless(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return s.startFreshWorkspaceHeadlessWithOptions(surface, workspaceKey, false)
}

func (s *Service) startFreshWorkspaceHeadlessWithOptions(surface *state.SurfaceConsoleRecord, workspaceKey string, prepareNewThread bool) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_create_invalid", "目录路径无效，请重新选择。")
	}
	if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
		return blocked
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-workspace-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	events := []eventcontract.Event{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.PendingHeadless = nil
		surface.ActiveQueueItemID = ""
		surface.DispatchMode = state.DispatchModeNormal
		surface.Abandoning = false
		delete(s.pausedUntil, surface.SurfaceSessionID)
		delete(s.abandoningUntil, surface.SurfaceSessionID)
	}
	if !s.claimWorkspace(surface, workspaceKey) {
		return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")...)
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:       instanceID,
		ThreadCWD:        workspaceKey,
		ClaudeProfileID:  s.surfaceClaudeProfileID(surface),
		RequestedAt:      s.now(),
		ExpiresAt:        s.now().Add(s.config.HeadlessLaunchWait),
		Status:           state.HeadlessLaunchStarting,
		Purpose:          state.HeadlessLaunchPurposeFreshWorkspace,
		PrepareNewThread: prepareNewThread,
	}
	s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	noticeTitle := "正在接入工作区"
	noticeText := fmt.Sprintf("正在把 `%s` 接入为可用工作区，完成后你就可以直接发送文本开启新会话。", workspaceKey)
	if prepareNewThread {
		noticeText = fmt.Sprintf("正在把 `%s` 接入为可用工作区，完成后会直接进入新会话待命。", workspaceKey)
	}
	events = append(events,
		eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "workspace_create_starting",
				Title: noticeTitle,
				Text:  noticeText,
			},
		},
		eventcontract.Event{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: func() *control.DaemonCommand {
				command := &control.DaemonCommand{
					Kind:             control.DaemonCommandStartHeadless,
					SurfaceSessionID: surface.SurfaceSessionID,
					InstanceID:       instanceID,
					ThreadCWD:        workspaceKey,
				}
				s.applyCurrentClaudeProfileToHeadlessCommand(surface, command)
				return command
			}(),
		},
	)
	return events
}
