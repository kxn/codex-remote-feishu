package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
)

const gitWorkspaceWorktreeCommandTimeout = 10 * time.Minute

var runGitWorkspaceWorktreeCreate = gitworkspace.CreateWorktree

func (a *App) handleGitWorkspaceWorktreeCreateCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	request := gitworkspace.WorktreeRequest{
		BaseWorkspacePath: strings.TrimSpace(command.WorkspaceKey),
		BranchName:        strings.TrimSpace(command.BranchName),
		DirectoryName:     strings.TrimSpace(command.DirectoryName),
	}
	if request.BaseWorkspacePath == "" {
		return gitWorkspaceWorktreeNotice(command.SurfaceSessionID, "worktree_create_invalid_base_workspace", "基准工作区无效，请重新开始创建流程。")
	}
	if request.BranchName == "" {
		return gitWorkspaceWorktreeNotice(command.SurfaceSessionID, "worktree_create_invalid_branch_name", "新分支名无效，请重新开始创建流程。")
	}

	createCtx, cancel := context.WithTimeout(context.Background(), gitWorkspaceWorktreeCommandTimeout)
	runtimeKey := a.beginGitWorkspaceWorktreeRuntimeLocked(command.SurfaceSessionID, command.PickerID, cancel)
	defer cancel()

	a.mu.Unlock()
	result, err := runGitWorkspaceWorktreeCreate(createCtx, request)
	a.mu.Lock()
	if a.finishGitWorkspaceWorktreeRuntimeLocked(runtimeKey) {
		return nil
	}
	if err != nil {
		var createErr *gitmeta.WorktreeCreateError
		if errors.As(err, &createErr) {
			log.Printf(
				"git worktree create failed: surface=%s picker=%s base=%s branch=%s dir=%s dest=%s code=%s err=%v stderr=%s",
				command.SurfaceSessionID,
				command.PickerID,
				createErr.BaseWorkspacePath,
				createErr.BranchName,
				createErr.DirectoryName,
				createErr.DestinationPath,
				createErr.Code,
				createErr.Err,
				createErr.Stderr,
			)
			if events := a.service.FailTargetPickerWorktreeCreate(command.SurfaceSessionID, command.PickerID, createErr); len(events) != 0 {
				return events
			}
			return gitWorkspaceWorktreeNotice(command.SurfaceSessionID, string(createErr.Code), gitmeta.WorktreeCreateErrorText(createErr))
		}
		log.Printf("git worktree create failed: surface=%s picker=%s base=%s branch=%s err=%v", command.SurfaceSessionID, command.PickerID, request.BaseWorkspacePath, request.BranchName, err)
		return gitWorkspaceWorktreeNotice(command.SurfaceSessionID, "worktree_create_failed", "worktree 创建失败，请稍后重试。")
	}

	events := a.service.CompleteTargetPickerWorktreeCreate(command.SurfaceSessionID, command.PickerID, result.WorkspacePath)
	if len(events) != 0 {
		return events
	}
	return gitWorkspaceWorktreeNotice(command.SurfaceSessionID, "worktree_create_completed", fmt.Sprintf("worktree 已创建到 `%s`。", result.WorkspacePath))
}

func (a *App) handleGitWorkspaceWorktreeCancelCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	a.cancelGitWorkspaceWorktreeRuntimeLocked(command.SurfaceSessionID, command.PickerID)
	return nil
}

func gitWorkspaceWorktreeNotice(surfaceID, code, text string) []eventcontract.Event {
	title := "Worktree 创建失败"
	switch code {
	case "worktree_create_starting":
		title = "正在创建 Worktree 工作区"
	case "worktree_create_completed":
		title = "Worktree 工作区已创建"
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
