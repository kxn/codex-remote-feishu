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
	if !state.IsWindowsVolumePath(path) {
		return ""
	}
	return path[:2] + "/"
}

func (s *Service) startFreshWorkspaceHeadless(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return s.startFreshWorkspaceHeadlessWithOptions(surface, workspaceKey, false)
}

func (s *Service) startFreshWorkspaceHeadlessWithOptions(surface *state.SurfaceConsoleRecord, workspaceKey string, prepareNewThread bool) []eventcontract.Event {
	return s.startFreshWorkspaceHeadlessWithOverlayCleanup(surface, workspaceKey, prepareNewThread, surfaceOverlayRouteCleanupOptions{})
}

func (s *Service) startFreshWorkspaceHeadlessWithOverlayCleanup(surface *state.SurfaceConsoleRecord, workspaceKey string, prepareNewThread bool, cleanup surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	noticeText := fmt.Sprintf("正在把 `%s` 接入为可用工作区，完成后你就可以直接发送文本开启新会话。", normalizeWorkspaceClaimKey(workspaceKey))
	if prepareNewThread {
		noticeText = fmt.Sprintf("正在把 `%s` 接入为可用工作区，完成后会直接进入新会话待命。", normalizeWorkspaceClaimKey(workspaceKey))
	}
	return s.startWorkspaceHeadlessLaunchWithOverlayCleanup(surface, workspaceKey, workspaceHeadlessLaunchOptions{
		Purpose:                  state.HeadlessLaunchPurposeFreshWorkspace,
		PrepareNewThread:         prepareNewThread,
		ValidateRoomWorkspaceSet: true,
		NoticeCode:               "workspace_create_starting",
		NoticeTitle:              "正在接入工作区",
		NoticeText:               noticeText,
		OverlayCleanup:           cleanup,
	})
}

func (s *Service) startWorkspaceRouteRestartHeadlessWithOverlayCleanup(surface *state.SurfaceConsoleRecord, attempt SurfaceResumeAttempt, cleanup surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	workspaceKey := normalizeWorkspaceClaimKey(attempt.WorkspaceKey)
	if workspaceKey == "" {
		workspaceKey = state.ResolveHeadlessResumeWorkspaceKey("", attempt.ThreadCWD)
	}
	noticeText := fmt.Sprintf("正在按当前工作区 `%s` 重新准备运行环境。", workspaceKey)
	if attempt.PrepareNewThread {
		noticeText = fmt.Sprintf("正在按当前工作区 `%s` 重新准备运行环境，完成后会直接进入新会话待命。", workspaceKey)
	}
	return s.startWorkspaceHeadlessLaunchWithOverlayCleanup(surface, workspaceKey, workspaceHeadlessLaunchOptions{
		Purpose:                  state.HeadlessLaunchPurposeWorkspaceRouteRestart,
		PrepareNewThread:         attempt.PrepareNewThread,
		ValidateRoomWorkspaceSet: false,
		NoticeCode:               "workspace_route_restart_starting",
		NoticeTitle:              "正在重新准备当前工作区",
		NoticeText:               noticeText,
		OverlayCleanup:           cleanup,
	})
}

type workspaceHeadlessLaunchOptions struct {
	Purpose                  state.HeadlessLaunchPurpose
	PrepareNewThread         bool
	ValidateRoomWorkspaceSet bool
	NoticeCode               string
	NoticeTitle              string
	NoticeText               string
	OverlayCleanup           surfaceOverlayRouteCleanupOptions
}

func (s *Service) startWorkspaceHeadlessLaunchWithOverlayCleanup(surface *state.SurfaceConsoleRecord, workspaceKey string, options workspaceHeadlessLaunchOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_create_invalid", "目录路径无效，请重新选择。")
	}
	if blocked := s.blockFreshThreadAttach(surface, options.OverlayCleanup); blocked != nil {
		return blocked
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	if options.ValidateRoomWorkspaceSet {
		if blocked := s.prepareFeishuRoomWorkspaceChange(surface, workspaceKey); blocked != nil {
			return blocked
		}
	}
	if options.Purpose == "" {
		options.Purpose = state.HeadlessLaunchPurposeFreshWorkspace
	}
	if options.NoticeCode == "" {
		options.NoticeCode = "workspace_create_starting"
	}
	if options.NoticeTitle == "" {
		options.NoticeTitle = "正在接入工作区"
	}
	if strings.TrimSpace(options.NoticeText) == "" && options.PrepareNewThread {
		options.NoticeText = fmt.Sprintf("正在把 `%s` 接入为可用工作区，完成后会直接进入新会话待命。", workspaceKey)
	}
	if strings.TrimSpace(options.NoticeText) == "" {
		options.NoticeText = fmt.Sprintf("正在把 `%s` 接入为可用工作区，完成后你就可以直接发送文本开启新会话。", workspaceKey)
	}

	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-workspace-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	events := s.prepareSurfaceForExecutionReattachWithOverlayCleanup(surface, options.OverlayCleanup)
	if !s.claimWorkspace(surface, workspaceKey) {
		return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")...)
	}
	if blocked := s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface); len(blocked) != 0 {
		return append(events, blocked...)
	}
	launchContract := s.headlessLaunchContract(surface)
	s.adoptSurfacePendingHeadlessLaunch(surface, &state.HeadlessLaunchRecord{
		InstanceID:                instanceID,
		WorkspaceKey:              workspaceKey,
		ThreadCWD:                 workspaceKey,
		Backend:                   launchContract.Backend,
		CodexProfileID:            launchContract.CodexProfileID,
		CodexAdmissionRef:         state.NormalizeCodexAdmissionRef(launchContract.CodexAdmissionRef),
		CodexConnectionContract:   state.CloneCodexConnectionContract(launchContract.CodexConnectionContract),
		CodexThreadPolicy:         state.CloneCodexThreadPolicy(launchContract.CodexThreadPolicy),
		ClaudeProfileID:           launchContract.ClaudeProfileID,
		ClaudeReasoningEffort:     launchContract.ClaudeReasoningEffort,
		OpenCodeProfileID:         launchContract.OpenCodeProfileID,
		OpenCodeAdmissionRef:      state.NormalizeOpenCodeAdmissionRef(launchContract.OpenCodeAdmissionRef),
		OpenCodeRuntimeAccessMode: launchContract.OpenCodeRuntimeAccessMode,
		RequestedAt:               s.now(),
		ExpiresAt:                 s.now().Add(s.config.HeadlessLaunchWait),
		Status:                    state.HeadlessLaunchStarting,
		Purpose:                   options.Purpose,
		PrepareNewThread:          options.PrepareNewThread,
	})
	if options.ValidateRoomWorkspaceSet {
		s.syncFeishuRoomWorkspaceBinding(surface, workspaceKey)
	}
	events = append(events,
		eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  options.NoticeCode,
				Title: options.NoticeTitle,
				Text:  options.NoticeText,
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
					WorkspaceKey:     workspaceKey,
					ThreadCWD:        workspaceKey,
				}
				s.applyHeadlessLaunchContract(command, launchContract)
				return command
			}(),
		},
	)
	return events
}
