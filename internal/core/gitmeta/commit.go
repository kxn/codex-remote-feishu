package gitmeta

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

const defaultCommitInspectTimeout = 2 * time.Second

type CommitSummary struct {
	SHA      string
	ShortSHA string
	Subject  string
}

func (c CommitSummary) Normalized() CommitSummary {
	c.SHA = strings.TrimSpace(strings.ToLower(c.SHA))
	c.ShortSHA = strings.TrimSpace(strings.ToLower(c.ShortSHA))
	c.Subject = strings.TrimSpace(c.Subject)
	if c.ShortSHA == "" && len(c.SHA) >= 7 {
		c.ShortSHA = c.SHA[:7]
	}
	return c
}

type CommitResolveStatus string

const (
	CommitResolveFound     CommitResolveStatus = "found"
	CommitResolveNotFound  CommitResolveStatus = "not_found"
	CommitResolveAmbiguous CommitResolveStatus = "ambiguous"
)

type CommitResolveResult struct {
	Status CommitResolveStatus
	Commit CommitSummary
}

func ListRecentCommits(path string, limit int) ([]CommitSummary, error) {
	commandDir, err := workspaceGitCommandDir(path)
	if err != nil || commandDir == "" {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	output, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "log", "--no-show-signature", "--format=%H%x1f%h%x1f%s", "-n", strconv.Itoa(limit))
	if err != nil {
		if hasHead, headErr := repoHasCommittedHEAD(commandDir); headErr == nil && !hasHead {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	commits := make([]CommitSummary, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) < 3 {
			continue
		}
		commit := CommitSummary{
			SHA:      parts[0],
			ShortSHA: parts[1],
			Subject:  parts[2],
		}.Normalized()
		if commit.SHA == "" {
			continue
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func ResolveCommitPrefix(path, prefix string) (CommitResolveResult, error) {
	commandDir, err := workspaceGitCommandDir(path)
	if err != nil || commandDir == "" {
		return CommitResolveResult{}, err
	}
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return CommitResolveResult{Status: CommitResolveNotFound}, nil
	}
	output, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "log", "--all", "--no-show-signature", "--format=%H%x1f%h%x1f%s")
	if err != nil {
		if hasHead, headErr := repoHasCommittedHEAD(commandDir); headErr == nil && !hasHead {
			return CommitResolveResult{Status: CommitResolveNotFound}, nil
		}
		return CommitResolveResult{}, err
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	matches := make([]CommitSummary, 0, 4)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) < 3 {
			continue
		}
		commit := CommitSummary{
			SHA:      parts[0],
			ShortSHA: parts[1],
			Subject:  parts[2],
		}.Normalized()
		if !strings.HasPrefix(commit.SHA, prefix) {
			continue
		}
		matches = append(matches, commit)
	}
	switch len(matches) {
	case 0:
		return CommitResolveResult{Status: CommitResolveNotFound}, nil
	case 1:
		return CommitResolveResult{
			Status: CommitResolveFound,
			Commit: matches[0],
		}, nil
	default:
		return CommitResolveResult{Status: CommitResolveAmbiguous}, nil
	}
}

func MatchRecentCommitPrefix(commits []CommitSummary, prefix string) (CommitSummary, bool) {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return CommitSummary{}, false
	}
	var match CommitSummary
	count := 0
	for _, current := range commits {
		current = current.Normalized()
		if current.SHA == "" {
			continue
		}
		if !strings.HasPrefix(current.SHA, prefix) {
			continue
		}
		match = current
		count++
		if count > 1 {
			return CommitSummary{}, false
		}
	}
	return match, count == 1
}

func workspaceGitCommandDir(path string) (string, error) {
	info, err := LocateWorkspace(path)
	if err != nil || !info.InRepo() {
		return "", err
	}
	commandDir := strings.TrimSpace(info.ProbePath)
	if commandDir == "" {
		commandDir = strings.TrimSpace(info.RepoRoot)
	}
	return commandDir, nil
}

func repoHasCommittedHEAD(commandDir string) (bool, error) {
	_, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func runGitCommandOutput(cwd string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	execlaunch.Prepare(cmd)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
