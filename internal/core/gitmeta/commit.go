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
		commit := parseCommitSummaryLine(line)
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
	if len(prefix) < 4 {
		return CommitResolveResult{Status: CommitResolveAmbiguous}, nil
	}
	matches, err := resolveCommitPrefixObjectMatches(commandDir, prefix)
	if err != nil {
		if hasHead, headErr := repoHasCommittedHEAD(commandDir); headErr == nil && !hasHead {
			return CommitResolveResult{Status: CommitResolveNotFound}, nil
		}
		return CommitResolveResult{}, err
	}
	switch len(matches) {
	case 0:
		return CommitResolveResult{Status: CommitResolveNotFound}, nil
	case 1:
		commit, err := loadCommitSummary(commandDir, matches[0])
		if err != nil {
			return CommitResolveResult{}, err
		}
		return CommitResolveResult{
			Status: CommitResolveFound,
			Commit: commit,
		}, nil
	default:
		return CommitResolveResult{Status: CommitResolveAmbiguous}, nil
	}
}

func resolveCommitPrefixObjectMatches(commandDir, prefix string) ([]string, error) {
	output, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "rev-parse", "--disambiguate="+prefix)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	matches := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, line := range lines {
		oid := strings.TrimSpace(strings.ToLower(line))
		if oid == "" || seen[oid] {
			continue
		}
		seen[oid] = true
		typ, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "cat-file", "-t", oid)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(typ) != "commit" {
			continue
		}
		matches = append(matches, oid)
		if len(matches) > 1 {
			return matches, nil
		}
	}
	return matches, nil
}

func loadCommitSummary(commandDir, sha string) (CommitSummary, error) {
	output, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "show", "-s", "--no-show-signature", "--format=%H%x1f%h%x1f%s", sha)
	if err != nil {
		return CommitSummary{}, err
	}
	return parseCommitSummaryLine(output), nil
}

func parseCommitSummaryLine(line string) CommitSummary {
	parts := strings.SplitN(line, "\x1f", 3)
	if len(parts) < 3 {
		return CommitSummary{}
	}
	return CommitSummary{
		SHA:      parts[0],
		ShortSHA: parts[1],
		Subject:  parts[2],
	}.Normalized()
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
	output, err := runGitCommandRawOutput(cwd, timeout, args...)
	return strings.TrimSpace(output), err
}

func runGitCommandRawOutput(cwd string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	execlaunch.Prepare(cmd)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
