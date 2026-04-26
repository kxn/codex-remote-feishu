package gitmeta

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

const defaultInspectTimeout = 2 * time.Second

// WorkspaceStatus is a UI-neutral summary of local git status output.
type WorkspaceStatus struct {
	Dirty          bool
	Files          []string
	ModifiedCount  int
	UntrackedCount int
}

// WorkspaceInfo is a shared read model for git-backed workspaces.
type WorkspaceInfo struct {
	ProbePath      string
	RepoRoot       string
	GitDir         string
	Branch         string
	Detached       bool
	LinkedWorktree bool
	Status         WorkspaceStatus
}

// InRepo reports whether the probed path resolved to a git-backed workspace.
func (info WorkspaceInfo) InRepo() bool {
	return strings.TrimSpace(info.RepoRoot) != "" && strings.TrimSpace(info.GitDir) != ""
}

type InspectOptions struct {
	IncludeStatus bool
	Timeout       time.Duration
}

// LocateWorkspace resolves the nearest git workspace for the given path
// without invoking the git binary.
func LocateWorkspace(path string) (WorkspaceInfo, error) {
	return locateWorkspace(path)
}

// InspectWorkspace resolves git workspace metadata and optionally reads status.
func InspectWorkspace(path string, opts InspectOptions) (WorkspaceInfo, error) {
	info, err := locateWorkspace(path)
	if err != nil || !info.InRepo() {
		return info, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultInspectTimeout
	}
	commandDir := strings.TrimSpace(info.ProbePath)
	if commandDir == "" {
		commandDir = info.RepoRoot
	}
	branch, detached, err := inspectBranch(commandDir, timeout)
	if err != nil {
		return info, err
	}
	info.Branch = branch
	info.Detached = detached
	if !opts.IncludeStatus {
		return info, nil
	}
	output, err := runGit(commandDir, timeout, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return info, err
	}
	info.Status = ParseStatusSummary(output)
	return info, nil
}

// ParseStatusPaths extracts normalized file paths from git porcelain output.
func ParseStatusPaths(output string) []string {
	return ParseStatusSummary(output).Files
}

// ParseStatusSummary parses git porcelain output into a neutral status summary.
func ParseStatusSummary(output string) WorkspaceStatus {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	files := make([]string, 0, len(lines))
	modifiedSeen := map[string]bool{}
	untrackedSeen := map[string]bool{}
	modifiedCount := 0
	untrackedCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		path := strings.TrimSpace(line[3:])
		if idx := strings.LastIndex(path, " -> "); idx >= 0 {
			path = strings.TrimSpace(path[idx+4:])
		}
		path = normalizeStatusPath(parseStatusPath(path))
		if path == "" {
			continue
		}
		if status == "??" {
			if !untrackedSeen[path] {
				untrackedSeen[path] = true
				untrackedCount++
			}
		} else if !modifiedSeen[path] {
			modifiedSeen[path] = true
			modifiedCount++
		}
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	return WorkspaceStatus{
		Dirty:          len(files) > 0,
		Files:          files,
		ModifiedCount:  modifiedCount,
		UntrackedCount: untrackedCount,
	}
}

func locateWorkspace(path string) (WorkspaceInfo, error) {
	probePath := normalizeProbePath(path)
	info := WorkspaceInfo{ProbePath: probePath}
	if probePath == "" {
		return info, nil
	}
	current := probePath
	if stat, err := os.Stat(current); err == nil && !stat.IsDir() {
		current = filepath.Dir(current)
		info.ProbePath = current
	}
	for current != "" {
		gitPath := filepath.Join(current, ".git")
		stat, err := os.Stat(gitPath)
		if err == nil {
			if stat.IsDir() {
				info.RepoRoot = current
				info.GitDir = gitPath
				return info, nil
			}
			parsed, parseErr := ParseGitDirFile(gitPath)
			if parseErr != nil {
				return info, parseErr
			}
			if strings.TrimSpace(parsed) != "" {
				info.RepoRoot = current
				info.GitDir = ResolveGitDirPath(current, parsed)
				info.LinkedWorktree = true
				return info, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return info, nil
}

func inspectBranch(cwd string, timeout time.Duration) (string, bool, error) {
	output, symbolicErr := runGit(cwd, timeout, "symbolic-ref", "--short", "HEAD")
	if branch := strings.TrimSpace(output); symbolicErr == nil && branch != "" {
		return branch, false, nil
	}
	output, headErr := runGit(cwd, timeout, "rev-parse", "--short", "HEAD")
	if branch := strings.TrimSpace(output); headErr == nil && branch != "" {
		return branch, true, nil
	}
	if headErr != nil {
		return "", false, headErr
	}
	return "", false, symbolicErr
}

func runGit(cwd string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	execlaunch.Prepare(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func normalizeProbePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func parseStatusPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && strings.HasPrefix(path, "\"") && strings.HasSuffix(path, "\"") {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return unquoted
		}
	}
	return path
}

func normalizeStatusPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.Trim(path, "/")
	return path
}
