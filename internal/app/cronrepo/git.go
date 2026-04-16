package cronrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
)

type ErrorCode string

const (
	ErrorGitMissing           ErrorCode = "cron_repo_git_missing"
	ErrorInvalidURL           ErrorCode = "cron_repo_invalid_url"
	ErrorRefNotFound          ErrorCode = "cron_repo_ref_not_found"
	ErrorAuthFailed           ErrorCode = "cron_repo_auth_failed"
	ErrorCacheInitFailed      ErrorCode = "cron_repo_cache_init_failed"
	ErrorFetchFailed          ErrorCode = "cron_repo_fetch_failed"
	ErrorWorktreeCreateFailed ErrorCode = "cron_repo_worktree_create_failed"
	ErrorCleanupFailed        ErrorCode = "cron_repo_cleanup_failed"
)

type Error struct {
	Code        ErrorCode
	Message     string
	SourceInput string
	RepoURL     string
	Ref         string
	Path        string
	Stderr      string
	Err         error
}

func (e *Error) Error() string {
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

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ensureGitBinary() error {
	if _, err := exec.LookPath("git"); err != nil {
		return &Error{Code: ErrorGitMissing, Message: "git executable not found", Err: err}
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	gitworkspace.PrepareCommand(cmd)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w%s", err, formatGitStderr(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitRun(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)
	return err
}

func formatGitStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

func classifyGitError(code ErrorCode, message string, spec SourceSpec, pathValue string, err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// keep caller-provided code
	case strings.Contains(text, "couldn't find remote ref"),
		(strings.Contains(text, "remote branch") && strings.Contains(text, "not found")),
		strings.Contains(text, "needed a single revision"),
		strings.Contains(text, "unknown revision"):
		code = ErrorRefNotFound
		message = "git ref not found"
	case strings.Contains(text, "authentication failed"),
		strings.Contains(text, "permission denied"),
		strings.Contains(text, "repository not found"),
		strings.Contains(text, "could not read username"):
		code = ErrorAuthFailed
		message = "git authentication failed"
	case strings.Contains(text, "does not appear to be a git repository"),
		strings.Contains(text, "not a git repository"),
		strings.Contains(text, "invalid repository name"),
		strings.Contains(text, "unable to access"):
		code = ErrorInvalidURL
		message = "git repository url is invalid"
	}
	return &Error{
		Code:        code,
		Message:     message,
		SourceInput: spec.RawInput,
		RepoURL:     spec.RepoURL,
		Ref:         spec.Ref,
		Path:        pathValue,
		Stderr:      extractGitStderr(err),
		Err:         err,
	}
}

func extractGitStderr(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if index := strings.Index(text, ": "); index >= 0 && index+2 < len(text) {
		return strings.TrimSpace(text[index+2:])
	}
	return text
}

func ensureDir(pathValue string) error {
	if err := os.MkdirAll(pathValue, 0o755); err != nil {
		return err
	}
	return nil
}

func pathExists(pathValue string) bool {
	_, err := os.Stat(pathValue)
	return err == nil
}

func cleanAbs(pathValue string) string {
	if strings.TrimSpace(pathValue) == "" {
		return ""
	}
	if abs, err := filepath.Abs(pathValue); err == nil {
		pathValue = abs
	}
	return filepath.Clean(pathValue)
}
