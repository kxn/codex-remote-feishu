package gitmeta

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type WorktreeCreateErrorCode string

const (
	WorktreeCreateErrorGitMissing           WorktreeCreateErrorCode = "git_worktree_git_missing"
	WorktreeCreateErrorInvalidBaseWorkspace WorktreeCreateErrorCode = "git_worktree_invalid_base_workspace"
	WorktreeCreateErrorBaseWorkspaceNotGit  WorktreeCreateErrorCode = "git_worktree_base_not_git"
	WorktreeCreateErrorInvalidBranchName    WorktreeCreateErrorCode = "git_worktree_invalid_branch_name"
	WorktreeCreateErrorBranchExists         WorktreeCreateErrorCode = "git_worktree_branch_exists"
	WorktreeCreateErrorInvalidDirectoryName WorktreeCreateErrorCode = "git_worktree_invalid_directory_name"
	WorktreeCreateErrorDestinationExists    WorktreeCreateErrorCode = "git_worktree_destination_exists"
	WorktreeCreateErrorCreateFailed         WorktreeCreateErrorCode = "git_worktree_create_failed"
)

type WorktreeCreateRequest struct {
	BaseWorkspacePath string
	BranchName        string
	DirectoryName     string
}

type WorktreePreviewResult struct {
	BaseWorkspacePath string
	BranchName        string
	DirectoryName     string
	ParentDir         string
	DestinationPath   string
}

type WorktreeCreateError struct {
	Code              WorktreeCreateErrorCode
	Message           string
	BaseWorkspacePath string
	BranchName        string
	DirectoryName     string
	DestinationPath   string
	Stderr            string
	Err               error
}

func (e *WorktreeCreateError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *WorktreeCreateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func PreviewWorktree(req WorktreeCreateRequest) (WorktreePreviewResult, error) {
	baseWorkspacePath := normalizeProbePath(req.BaseWorkspacePath)
	branchName := strings.TrimSpace(req.BranchName)
	directoryName := strings.TrimSpace(req.DirectoryName)
	if baseWorkspacePath == "" {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:    WorktreeCreateErrorInvalidBaseWorkspace,
			Message: "base workspace path is required",
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorGitMissing,
			Message:           "git executable not found",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     directoryName,
			Err:               err,
		}
	}
	info, err := InspectWorkspace(baseWorkspacePath, InspectOptions{})
	if err != nil {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorCreateFailed,
			Message:           "failed to inspect base workspace",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     directoryName,
			Err:               err,
		}
	}
	baseWorkspacePath = normalizeProbePath(info.RepoRoot)
	if !info.InRepo() || baseWorkspacePath == "" {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorBaseWorkspaceNotGit,
			Message:           "base workspace is not a git repository",
			BaseWorkspacePath: normalizeProbePath(req.BaseWorkspacePath),
			BranchName:        branchName,
			DirectoryName:     directoryName,
		}
	}
	if err := ValidateBranchName(branchName); err != nil {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorInvalidBranchName,
			Message:           err.Error(),
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     directoryName,
			Err:               err,
		}
	}
	branchExists, err := branchExists(baseWorkspacePath, branchName)
	if err != nil {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorCreateFailed,
			Message:           "failed to inspect branch state",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     directoryName,
			Err:               err,
		}
	}
	if branchExists {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorBranchExists,
			Message:           "branch already exists",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     directoryName,
		}
	}
	resolvedDirName, err := resolveWorktreeDirectoryName(branchName, directoryName)
	if err != nil {
		var worktreeErr *WorktreeCreateError
		if errors.As(err, &worktreeErr) {
			worktreeErr.BaseWorkspacePath = baseWorkspacePath
			worktreeErr.BranchName = branchName
			return WorktreePreviewResult{}, worktreeErr
		}
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorInvalidDirectoryName,
			Message:           err.Error(),
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     directoryName,
			Err:               err,
		}
	}
	parentDir := filepath.Dir(baseWorkspacePath)
	if _, err := os.ReadDir(parentDir); err != nil {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorCreateFailed,
			Message:           "failed to inspect destination parent directory",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     resolvedDirName,
			Err:               err,
		}
	}
	destinationPath := filepath.Join(parentDir, resolvedDirName)
	if _, statErr := os.Stat(destinationPath); statErr == nil {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorDestinationExists,
			Message:           "destination already exists",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     resolvedDirName,
			DestinationPath:   destinationPath,
		}
	} else if !os.IsNotExist(statErr) {
		return WorktreePreviewResult{}, &WorktreeCreateError{
			Code:              WorktreeCreateErrorCreateFailed,
			Message:           "failed to inspect destination",
			BaseWorkspacePath: baseWorkspacePath,
			BranchName:        branchName,
			DirectoryName:     resolvedDirName,
			DestinationPath:   destinationPath,
			Err:               statErr,
		}
	}
	return WorktreePreviewResult{
		BaseWorkspacePath: baseWorkspacePath,
		BranchName:        branchName,
		DirectoryName:     resolvedDirName,
		ParentDir:         parentDir,
		DestinationPath:   destinationPath,
	}, nil
}

func ValidateBranchName(branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultInspectTimeout)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "git", "check-ref-format", "--branch", branchName)
	prepareGitCommand(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		if text := strings.TrimSpace(string(output)); text != "" {
			return errors.New(text)
		}
		return fmt.Errorf("branch name is invalid")
	}
	return nil
}

func branchExists(cwd, branchName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultInspectTimeout)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+strings.TrimSpace(branchName))
	cmd.Dir = strings.TrimSpace(cwd)
	prepareGitCommand(cmd)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func resolveWorktreeDirectoryName(branchName, directoryName string) (string, error) {
	if strings.TrimSpace(directoryName) != "" {
		if err := workspaceimport.ValidateDirectoryName(directoryName); err != nil {
			return "", &WorktreeCreateError{
				Code:          WorktreeCreateErrorInvalidDirectoryName,
				Message:       err.Error(),
				BranchName:    strings.TrimSpace(branchName),
				DirectoryName: strings.TrimSpace(directoryName),
				Err:           err,
			}
		}
		return strings.TrimSpace(directoryName), nil
	}
	inferred := workspaceimport.SanitizeDirectoryName(branchName)
	if strings.TrimSpace(inferred) == "" {
		return "", &WorktreeCreateError{
			Code:       WorktreeCreateErrorInvalidDirectoryName,
			Message:    "failed to infer directory name from branch name",
			BranchName: strings.TrimSpace(branchName),
		}
	}
	if err := workspaceimport.ValidateDirectoryName(inferred); err != nil {
		return "", &WorktreeCreateError{
			Code:          WorktreeCreateErrorInvalidDirectoryName,
			Message:       err.Error(),
			BranchName:    strings.TrimSpace(branchName),
			DirectoryName: inferred,
			Err:           err,
		}
	}
	return inferred, nil
}

func prepareGitCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	execlaunch.Prepare(cmd)
	baseEnv := cmd.Env
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	cmd.Env = append(baseEnv,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
}
