package gitworkspace

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type WorktreeErrorCode = gitmeta.WorktreeCreateErrorCode

const (
	WorktreeErrorGitMissing           = gitmeta.WorktreeCreateErrorGitMissing
	WorktreeErrorInvalidBaseWorkspace = gitmeta.WorktreeCreateErrorInvalidBaseWorkspace
	WorktreeErrorBaseWorkspaceNotGit  = gitmeta.WorktreeCreateErrorBaseWorkspaceNotGit
	WorktreeErrorInvalidBranchName    = gitmeta.WorktreeCreateErrorInvalidBranchName
	WorktreeErrorBranchExists         = gitmeta.WorktreeCreateErrorBranchExists
	WorktreeErrorInvalidDirectoryName = gitmeta.WorktreeCreateErrorInvalidDirectoryName
	WorktreeErrorDestinationExists    = gitmeta.WorktreeCreateErrorDestinationExists
	WorktreeErrorCreateFailed         = gitmeta.WorktreeCreateErrorCreateFailed
)

type WorktreeRequest = gitmeta.WorktreeCreateRequest

type WorktreeResult struct {
	WorkspacePath string
	BranchName    string
	DirectoryName string
}

type WorktreePreviewResult = gitmeta.WorktreePreviewResult

type WorktreeError = gitmeta.WorktreeCreateError

func PreviewWorktree(req WorktreeRequest) (WorktreePreviewResult, error) {
	return gitmeta.PreviewWorktree(req)
}

func CreateWorktree(ctx context.Context, req WorktreeRequest) (WorktreeResult, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return WorktreeResult{}, &WorktreeError{
			Code:              WorktreeErrorGitMissing,
			Message:           "git executable not found",
			BaseWorkspacePath: strings.TrimSpace(req.BaseWorkspacePath),
			BranchName:        strings.TrimSpace(req.BranchName),
			DirectoryName:     strings.TrimSpace(req.DirectoryName),
			Err:               err,
		}
	}
	preview, err := gitmeta.PreviewWorktree(req)
	if err != nil {
		return WorktreeResult{}, err
	}
	args := []string{"worktree", "add", "-b", preview.BranchName, preview.DestinationPath, "HEAD"}
	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = preview.BaseWorkspacePath
	PrepareCommand(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return WorktreeResult{}, classifyWorktreeFailure(preview, stderr.String(), err)
	}
	return WorktreeResult{
		WorkspacePath: preview.DestinationPath,
		BranchName:    preview.BranchName,
		DirectoryName: preview.DirectoryName,
	}, nil
}

func classifyWorktreeFailure(preview gitmeta.WorktreePreviewResult, stderr string, err error) error {
	lower := strings.ToLower(stderr)
	code := WorktreeErrorCreateFailed
	message := "git worktree create failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		message = "git worktree create timed out"
	case strings.Contains(lower, "already exists") && strings.Contains(lower, "branch"):
		code = WorktreeErrorBranchExists
		message = "git branch already exists"
	case strings.Contains(lower, "already exists") && strings.Contains(lower, "destination"):
		code = WorktreeErrorDestinationExists
		message = "git worktree destination already exists"
	case strings.Contains(lower, "invalid branch"), strings.Contains(lower, "not a valid branch name"):
		code = WorktreeErrorInvalidBranchName
		message = "git branch name is invalid"
	}
	return &WorktreeError{
		Code:              code,
		Message:           message,
		BaseWorkspacePath: preview.BaseWorkspacePath,
		BranchName:        preview.BranchName,
		DirectoryName:     preview.DirectoryName,
		DestinationPath:   preview.DestinationPath,
		Stderr:            strings.TrimSpace(stderr),
		Err:               err,
	}
}
