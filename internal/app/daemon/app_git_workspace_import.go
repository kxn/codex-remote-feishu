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
)

const gitWorkspaceImportCommandTimeout = 10 * time.Minute

var runGitWorkspaceImport = gitworkspace.Import

func (a *App) handleGitWorkspaceImportCommandLocked(command control.DaemonCommand) []control.UIEvent {
	request := gitworkspace.ImportRequest{
		RepoURL:       strings.TrimSpace(command.RepoURL),
		RefName:       strings.TrimSpace(command.RefName),
		ParentDir:     strings.TrimSpace(command.LocalPath),
		DirectoryName: strings.TrimSpace(command.DirectoryName),
	}
	if request.RepoURL == "" {
		return gitWorkspaceImportNotice(command.SurfaceSessionID, "git_import_invalid_url", "Git 仓库地址无效，请重新开始导入流程。")
	}
	if request.ParentDir == "" {
		return gitWorkspaceImportNotice(command.SurfaceSessionID, "git_import_clone_failed", "目标父目录无效，请重新选择本地落地位置。")
	}

	importCtx, cancel := context.WithTimeout(context.Background(), gitWorkspaceImportCommandTimeout)
	runtimeKey := a.beginGitWorkspaceImportRuntimeLocked(command.SurfaceSessionID, command.PickerID, cancel)
	defer cancel()

	a.mu.Unlock()
	result, err := runGitWorkspaceImport(importCtx, request)
	a.mu.Lock()
	if a.finishGitWorkspaceImportRuntimeLocked(runtimeKey) {
		return nil
	}
	if err != nil {
		var importErr *gitworkspace.ImportError
		if errors.As(err, &importErr) {
			log.Printf(
				"git import failed: surface=%s picker=%s repo=%s parent=%s dest=%s code=%s err=%v stderr=%s",
				command.SurfaceSessionID,
				command.PickerID,
				importErr.RepoURL,
				importErr.ParentDir,
				importErr.DestinationPath,
				importErr.Code,
				importErr.Err,
				importErr.Stderr,
			)
			if events := a.service.FailTargetPickerGitImport(command.SurfaceSessionID, command.PickerID, importErr); len(events) != 0 {
				return events
			}
			return gitWorkspaceImportNotice(command.SurfaceSessionID, string(importErr.Code), gitWorkspaceImportErrorText(importErr))
		}
		log.Printf("git import failed: surface=%s picker=%s repo=%s parent=%s err=%v", command.SurfaceSessionID, command.PickerID, request.RepoURL, request.ParentDir, err)
		return gitWorkspaceImportNotice(command.SurfaceSessionID, "git_import_clone_failed", "Git 仓库导入失败，请稍后重试。")
	}

	events := a.service.CompleteTargetPickerGitImport(command.SurfaceSessionID, command.PickerID, result.WorkspacePath)
	if len(events) != 0 {
		return events
	}
	return gitWorkspaceImportNotice(command.SurfaceSessionID, "git_import_completed", fmt.Sprintf("仓库已拉取到 `%s`。", result.WorkspacePath))
}

func (a *App) handleGitWorkspaceImportCancelCommandLocked(command control.DaemonCommand) []control.UIEvent {
	a.cancelGitWorkspaceImportRuntimeLocked(command.SurfaceSessionID, command.PickerID)
	return nil
}

func gitWorkspaceImportNotice(surfaceID, code, text string) []control.UIEvent {
	title := "Git 仓库导入失败"
	switch code {
	case "git_import_starting":
		title = "正在导入 Git 工作区"
	case "git_import_completed":
		title = "Git 工作区已导入"
	}
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: title,
			Text:  text,
		},
	}}
}

func gitWorkspaceImportErrorText(err *gitworkspace.ImportError) string {
	if err == nil {
		return "Git 仓库导入失败，请稍后重试。"
	}
	switch err.Code {
	case gitworkspace.ImportErrorGitMissing:
		return "当前机器未检测到 `git`，暂时不能直接从 Git URL 导入。"
	case gitworkspace.ImportErrorInvalidURL:
		return "Git 仓库地址无效，请检查地址格式后重试。"
	case gitworkspace.ImportErrorInvalidDirectoryName:
		return "目标目录名无效，请改成不含路径分隔符的普通目录名。"
	case gitworkspace.ImportErrorDestinationExists:
		return "目标目录已经存在，请换一个父目录或目录名后重试。"
	case gitworkspace.ImportErrorRefNotFound:
		return "指定的分支或标签不存在，请检查后重试。"
	case gitworkspace.ImportErrorAuthFailed:
		return "无法访问这个仓库，请确认当前机器上的 Git 凭据或仓库权限后重试。"
	default:
		return "Git 仓库导入失败，请稍后重试。"
	}
}
