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
	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
)

const gitWorkspaceImportCommandTimeout = 10 * time.Minute

var runGitWorkspaceImport = gitworkspace.Import

func (a *App) handleGitWorkspaceImportCommandLocked(command control.DaemonCommand) []eventcontract.Event {
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
		var importErr *workspaceimport.ImportError
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
			return gitWorkspaceImportNotice(command.SurfaceSessionID, string(importErr.Code), workspaceimport.ErrorText(importErr))
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

func (a *App) handleGitWorkspaceImportCancelCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	a.cancelGitWorkspaceImportRuntimeLocked(command.SurfaceSessionID, command.PickerID)
	return nil
}

func gitWorkspaceImportNotice(surfaceID, code, text string) []eventcontract.Event {
	title := "Git 仓库导入失败"
	switch code {
	case "git_import_starting":
		title = "正在导入 Git 工作区"
	case "git_import_completed":
		title = "Git 工作区已导入"
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
