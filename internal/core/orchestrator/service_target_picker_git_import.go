package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) respondLocalRequest(surface *state.SurfaceConsoleRecord, _ *state.RequestPromptRecord, _ control.Action) []control.UIEvent {
	return notice(surface, "request_unsupported", "这张本地交互卡片已经失效，请重新发送最新命令。")
}

func (s *Service) CompleteTargetPickerGitImport(surfaceSessionID, pickerID, workspaceKey string) []control.UIEvent {
	surface := s.root.Surfaces[surfaceSessionID]
	if surface == nil {
		return nil
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "git_import_clone_failed", "仓库已拉取完成，但解析本地工作区目录失败。")
	}
	record := s.activeTargetPicker(surface)
	if record == nil || strings.TrimSpace(record.PickerID) != strings.TrimSpace(pickerID) {
		return notice(surface, "git_import_flow_stale", fmt.Sprintf("仓库已拉取到 `%s`，但原始选择流程已经失效。目录会保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey))
	}
	events := s.enterTargetPickerNewThread(surface, workspaceKey)
	if targetPickerNewThreadSucceeded(surface, workspaceKey) {
		s.clearSurfaceTargetPicker(surface)
		return events
	}
	return append(events, notice(surface, "git_import_workspace_attach_failed", fmt.Sprintf("仓库已拉取到 `%s`，但接入工作区失败。目录已保留，你可以稍后通过“添加工作区 / 本地目录”继续接入。", workspaceKey))...)
}
