package gitworkspace

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/workspaceimport"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type ImportErrorCode = workspaceimport.ImportErrorCode

const (
	ImportErrorGitMissing           = workspaceimport.ImportErrorGitMissing
	ImportErrorInvalidURL           = workspaceimport.ImportErrorInvalidURL
	ImportErrorInvalidDirectoryName = workspaceimport.ImportErrorInvalidDirectoryName
	ImportErrorDestinationExists    = workspaceimport.ImportErrorDestinationExists
	ImportErrorCloneFailed          = workspaceimport.ImportErrorCloneFailed
	ImportErrorRefNotFound          = workspaceimport.ImportErrorRefNotFound
	ImportErrorAuthFailed           = workspaceimport.ImportErrorAuthFailed
)

type ImportRequest = workspaceimport.ImportRequest

type ImportResult struct {
	WorkspacePath string
	DirectoryName string
}

type PreviewResult = workspaceimport.PreviewResult

type ImportError = workspaceimport.ImportError

func Preview(req ImportRequest) (PreviewResult, error) {
	return workspaceimport.Preview(req)
}

func Import(ctx context.Context, req ImportRequest) (ImportResult, error) {
	repoURL := strings.TrimSpace(req.RepoURL)
	if repoURL == "" {
		return ImportResult{}, &ImportError{Code: ImportErrorInvalidURL, Message: "git repo url is required"}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return ImportResult{}, &ImportError{Code: ImportErrorGitMissing, Message: "git executable not found", Err: err}
	}
	preview, err := workspaceimport.Preview(req)
	if err != nil {
		return ImportResult{}, err
	}
	parentDir := preview.ParentDir
	directoryName := preview.DirectoryName
	destinationPath := preview.DestinationPath

	args := []string{"clone"}
	if refName := strings.TrimSpace(req.RefName); refName != "" {
		args = append(args, "--branch", refName, "--single-branch")
	}
	args = append(args, "--", repoURL, destinationPath)

	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = parentDir
	PrepareCommand(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ImportResult{}, classifyImportFailure(req, parentDir, destinationPath, stderr.String(), err)
	}
	return ImportResult{
		WorkspacePath: destinationPath,
		DirectoryName: directoryName,
	}, nil
}

func classifyImportFailure(req ImportRequest, parentDir, destinationPath, stderr string, err error) error {
	lower := strings.ToLower(stderr)
	code := ImportErrorCloneFailed
	message := "git clone failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code = ImportErrorCloneFailed
		message = "git clone timed out"
	case strings.Contains(lower, "couldn't find remote ref"),
		(strings.Contains(lower, "remote branch") && strings.Contains(lower, "not found")):
		code = ImportErrorRefNotFound
		message = "git ref not found"
	case strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "repository not found"),
		strings.Contains(lower, "could not read username"):
		code = ImportErrorAuthFailed
		message = "git authentication failed"
	case strings.Contains(lower, "does not appear to be a git repository"),
		strings.Contains(lower, "not a git repository"),
		strings.Contains(lower, "invalid repository name"):
		code = ImportErrorInvalidURL
		message = "git repository url is invalid"
	}
	return &ImportError{
		Code:            code,
		Message:         message,
		RepoURL:         strings.TrimSpace(req.RepoURL),
		RefName:         strings.TrimSpace(req.RefName),
		ParentDir:       parentDir,
		DestinationPath: destinationPath,
		Stderr:          strings.TrimSpace(stderr),
		Err:             err,
	}
}
